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
			return fmt.Errorf("%w: version=%d", ErrMigrationChecksumMismatch, current.version)
		}
	}

	return nil
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
