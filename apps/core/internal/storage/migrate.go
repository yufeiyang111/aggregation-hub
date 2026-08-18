package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMigrationChecksumMismatch = errors.New("migration_checksum_mismatch")
	ErrInvalidMigration          = errors.New("迁移文件无效")
)

const legacyInitialMigrationChecksum = "9409c95a142c6a8b64e4babc6d13adadf58418426530652fc4c047505a3bfdf5"

var migrationFileName = regexp.MustCompile(`^(\d{4,})_([a-z0-9][a-z0-9_-]*)\.sql$`)

type migration struct {
	version  int64
	name     string
	checksum string
	sql      string
}

// Migrate 按版本顺序执行嵌入的前向迁移。已执行迁移的内容变化会被显式拒绝，绝不 reset 数据库。
func Migrate(ctx context.Context, database *sql.DB, migrationFS fs.FS) error {
	if ctx == nil {
		return errors.New("迁移上下文不能为空")
	}
	if database == nil {
		return errors.New("数据库不能为空")
	}
	if migrationFS == nil {
		return errors.New("迁移文件系统不能为空")
	}

	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("初始化迁移元数据失败: %w", err)
	}

	migrations, err := readMigrations(migrationFS)
	if err != nil {
		return err
	}

	for _, current := range migrations {
		var storedChecksum string
		err := database.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version = ?`, current.version).Scan(&storedChecksum)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := applyMigration(ctx, database, current); err != nil {
				return err
			}
		case err != nil:
			return fmt.Errorf("读取迁移元数据失败: %w", err)
		case storedChecksum != current.checksum:
			compatible, err := isVerifiedLegacyInitialMigration(ctx, database, current, storedChecksum, migrations)
			if err != nil {
				return err
			}
			if !compatible {
				return fmt.Errorf("%w: version=%d", ErrMigrationChecksumMismatch, current.version)
			}
		}
	}

	return nil
}

// isVerifiedLegacyInitialMigration 仅兼容已知预发布初始库，绝不接受任意校验和漂移。
func isVerifiedLegacyInitialMigration(ctx context.Context, database *sql.DB, current migration, storedChecksum string, migrations []migration) (bool, error) {
	if current.version != 1 || current.name != "0001_initial.sql" || storedChecksum != legacyInitialMigrationChecksum {
		return false, nil
	}

	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return false, fmt.Errorf("校验旧初始库完整性失败: %w", err)
	}
	if integrity != "ok" {
		return false, nil
	}

	appliedMigrations, err := readAppliedMigrations(ctx, database)
	if err != nil {
		return false, err
	}
	for version, applied := range appliedMigrations {
		if version == current.version {
			continue
		}
		var expected *migration
		for index := range migrations {
			if migrations[index].version == version {
				expected = &migrations[index]
				break
			}
		}
		if expected == nil || applied.name != expected.name || applied.checksum != expected.checksum {
			return false, nil
		}
	}

	providerModelColumns, err := readVerifiedLegacyColumns(ctx, database, "provider_models")
	if err != nil {
		return false, err
	}
	usageColumns, err := readVerifiedLegacyColumns(ctx, database, "usage_daily")
	if err != nil {
		return false, err
	}

	if !hasColumns(providerModelColumns, []string{
		"id", "provider_id", "upstream_model_id", "public_model_id", "context_window_tokens", "max_output_tokens", "capability_override_json",
	}) {
		return false, nil
	}
	if !hasColumns(usageColumns, []string{
		"date_utc", "provider_slug_snapshot", "public_model_snapshot", "input_tokens", "output_tokens", "cached_input_tokens", "reasoning_tokens",
	}) {
		return false, nil
	}

	// 只有旧初始迁移时才要求不存在后续迁移产生的结构；后续迁移已经存在时，
	// 允许它们按当前版本继续通过下面的普通迁移校验。
	if len(appliedMigrations) == 1 {
		if hasAnyColumn(providerModelColumns, []string{"context_window_override_tokens", "max_output_override_tokens"}) || hasAnyColumn(usageColumns, []string{
			"input_token_reported_count", "output_token_reported_count", "cached_input_token_reported_count", "reasoning_token_reported_count", "cache_eligible_input_tokens", "cache_eligible_cached_input_tokens",
		}) {
			return false, nil
		}

		var runtimeSettingsTableCount int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'runtime_settings_revision'").Scan(&runtimeSettingsTableCount); err != nil {
			return false, fmt.Errorf("读取旧初始库表结构失败: %w", err)
		}
		return runtimeSettingsTableCount == 0, nil
	}
	return true, nil
}

type appliedMigration struct {
	name     string
	checksum string
}

func readAppliedMigrations(ctx context.Context, database *sql.DB) (map[int64]appliedMigration, error) {
	rows, err := database.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("读取旧初始库迁移记录失败: %w", err)
	}
	defer rows.Close()

	result := make(map[int64]appliedMigration)
	for rows.Next() {
		var version int64
		var applied appliedMigration
		if err := rows.Scan(&version, &applied.name, &applied.checksum); err != nil {
			return nil, fmt.Errorf("读取旧初始库迁移记录值失败: %w", err)
		}
		result[version] = applied
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历旧初始库迁移记录失败: %w", err)
	}
	return result, nil
}

func readVerifiedLegacyColumns(ctx context.Context, database *sql.DB, table string) (map[string]struct{}, error) {
	query := ""
	switch table {
	case "provider_models":
		query = "PRAGMA table_info(provider_models)"
	case "usage_daily":
		query = "PRAGMA table_info(usage_daily)"
	default:
		return nil, errors.New("旧初始库表标识无效")
	}

	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("读取旧初始库列失败: %w", err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var columnIndex int
		var columnName string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&columnIndex, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("读取旧初始库列值失败: %w", err)
		}
		columns[columnName] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历旧初始库列失败: %w", err)
	}
	return columns, nil
}

func hasColumns(columns map[string]struct{}, required []string) bool {
	for _, name := range required {
		if _, exists := columns[name]; !exists {
			return false
		}
	}
	return true
}

func hasAnyColumn(columns map[string]struct{}, names []string) bool {
	for _, name := range names {
		if _, exists := columns[name]; exists {
			return true
		}
	}
	return false
}

func readMigrations(migrationFS fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, ".")
	if err != nil {
		return nil, fmt.Errorf("读取迁移目录失败: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	versions := make(map[int64]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		matches := migrationFileName.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("%w: 文件名=%s", ErrInvalidMigration, entry.Name())
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("%w: 版本=%s", ErrInvalidMigration, matches[1])
		}
		if _, exists := versions[version]; exists {
			return nil, fmt.Errorf("%w: 重复版本=%d", ErrInvalidMigration, version)
		}
		versions[version] = struct{}{}

		contents, err := fs.ReadFile(migrationFS, path.Clean(entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("读取迁移文件失败: %w", err)
		}
		if len(strings.TrimSpace(string(contents))) == 0 {
			return nil, fmt.Errorf("%w: 空文件=%s", ErrInvalidMigration, entry.Name())
		}
		checksum := sha256.Sum256(contents)
		migrations = append(migrations, migration{
			version:  version,
			name:     entry.Name(),
			checksum: fmt.Sprintf("%x", checksum),
			sql:      string(contents),
		})
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("%w: 未找到 SQL 文件", ErrInvalidMigration)
	}

	sort.Slice(migrations, func(left, right int) bool {
		return migrations[left].version < migrations[right].version
	})
	return migrations, nil
}

func applyMigration(ctx context.Context, database *sql.DB, migration migration) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始迁移事务失败: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	if _, err := transaction.ExecContext(ctx, migration.sql); err != nil {
		return fmt.Errorf("执行迁移 %s 失败: %w", migration.name, err)
	}
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		migration.version,
		migration.name,
		migration.checksum,
		time.Now().UTC().UnixMilli(),
	); err != nil {
		return fmt.Errorf("记录迁移 %s 失败: %w", migration.name, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交迁移 %s 失败: %w", migration.name, err)
	}
	return nil
}
