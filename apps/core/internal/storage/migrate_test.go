package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

func openMigratedDatabase(t *testing.T) *sql.DB {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "aggregation-hub.db")
	database, err := storage.Open(databasePath)
	if err != nil {
		t.Fatalf("打开临时 SQLite 数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("关闭临时 SQLite 数据库失败: %v", err)
		}
	})

	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("执行初始迁移失败: %v", err)
	}
	return database
}

func mustReadMigration(t *testing.T, name string) []byte {
	t.Helper()
	value, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatalf("读取迁移文件 %s 失败: %v", name, err)
	}
	return value
}

func TestMigrateCreatesInitialSchemaAndEnforcesForeignKeys(t *testing.T) {
	database := openMigratedDatabase(t)

	var migrationCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&migrationCount); err != nil {
		t.Fatalf("查询 schema_migrations 失败: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("初始迁移记录数量=%d，期望 1", migrationCount)
	}

	var foreignKeys int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("查询 foreign_keys pragma 失败: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d，期望 1", foreignKeys)
	}

	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("重复执行相同迁移应幂等: %v", err)
	}
}

func TestMigrateCreatesAllV1Tables(t *testing.T) {
	database := openMigratedDatabase(t)

	requiredTables := []string{
		"schema_migrations",
		"app_settings",
		"local_access_keys",
		"providers",
		"provider_headers",
		"provider_models",
		"model_prices",
		"oauth_accounts",
		"provider_health_checks",
		"requests",
		"usage_daily",
		"audit_events",
		"runtime_settings_revision",
	}
	for _, tableName := range requiredTables {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tableName).Scan(&count); err != nil {
			t.Fatalf("查询表 %s 失败: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("缺少 V1 表 %s", tableName)
		}
	}

	var foreignKeyCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_list('provider_models')").Scan(&foreignKeyCount); err != nil {
		t.Fatalf("查询 provider_models 外键失败: %v", err)
	}
	if foreignKeyCount != 1 {
		t.Fatalf("provider_models 外键数量=%d，期望 1", foreignKeyCount)
	}
}
func TestMigrateRejectsChecksumDrift(t *testing.T) {
	database := openMigratedDatabase(t)

	changedMigrations := fstest.MapFS{
		"0001_initial.sql": &fstest.MapFile{Data: []byte("-- migration content changed\n")},
	}
	err := storage.Migrate(context.Background(), database, changedMigrations)
	if !errors.Is(err, storage.ErrMigrationChecksumMismatch) {
		t.Fatalf("迁移校验和变化错误=%v，期望 ErrMigrationChecksumMismatch", err)
	}
}

func TestMigrateRollsBackFailedMigrationWithoutResettingDatabase(t *testing.T) {
	database := openMigratedDatabase(t)

	brokenMigrations := fstest.MapFS{
		"0001_initial.sql":                     &fstest.MapFile{Data: mustReadMigration(t, "0001_initial.sql")},
		"0002_model_limit_overrides.sql":       &fstest.MapFile{Data: mustReadMigration(t, "0002_model_limit_overrides.sql")},
		"0003_usage_token_reporting.sql":       &fstest.MapFile{Data: mustReadMigration(t, "0003_usage_token_reporting.sql")},
		"0004_observability_query_indexes.sql": &fstest.MapFile{Data: mustReadMigration(t, "0004_observability_query_indexes.sql")},
		"0004_broken.sql":                      &fstest.MapFile{Data: []byte("CREATE TABLE broken (id INTEGER;\n")},
	}

	var beforeMigrationCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&beforeMigrationCount); err != nil {
		t.Fatalf("读取迁移前记录失败: %v", err)
	}

	err := storage.Migrate(context.Background(), database, brokenMigrations)
	if err == nil {
		t.Fatal("非法迁移不应被接受")
	}

	var migrationCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("查询迁移记录失败: %v", err)
	}
	if migrationCount != beforeMigrationCount {
		t.Fatalf("失败迁移不得写入记录，实际记录数=%d，期望=%d", migrationCount, beforeMigrationCount)
	}

	var providerTableCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='providers'").Scan(&providerTableCount); err != nil {
		t.Fatalf("查询原有表失败: %v", err)
	}
	if providerTableCount != 1 {
		t.Fatalf("迁移失败后不得 reset 原数据库，providers 表数量=%d", providerTableCount)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := storage.Open(" "); !errors.Is(err, storage.ErrInvalidDatabasePath) {
		t.Fatalf("空数据库路径错误=%v，期望 ErrInvalidDatabasePath", err)
	}
}

func TestMigrateAcceptsVerifiedLegacyInitialChecksum(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "aggregation-hub.db")
	database, err := storage.Open(databasePath)
	if err != nil {
		t.Fatalf("打开临时 SQLite 数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("关闭临时 SQLite 数据库失败: %v", err)
		}
	})

	initialOnly := fstest.MapFS{
		"0001_initial.sql": &fstest.MapFile{Data: mustReadMigration(t, "0001_initial.sql")},
	}
	if err := storage.Migrate(context.Background(), database, initialOnly); err != nil {
		t.Fatalf("执行旧初始迁移失败: %v", err)
	}
	if _, err := database.Exec(`UPDATE schema_migrations SET checksum = ? WHERE version = 1`, "9409c95a142c6a8b64e4babc6d13adadf58418426530652fc4c047505a3bfdf5"); err != nil {
		t.Fatalf("写入旧校验和夹具失败: %v", err)
	}

	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("已验证的旧初始库应可继续前向迁移: %v", err)
	}

	var migrationCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("读取迁移记录失败: %v", err)
	}
	if migrationCount != 5 {
		t.Fatalf("迁移记录数=%d，期望 5", migrationCount)
	}

	var storedChecksum string
	if err := database.QueryRow("SELECT checksum FROM schema_migrations WHERE version = 1").Scan(&storedChecksum); err != nil {
		t.Fatalf("读取旧迁移校验和失败: %v", err)
	}
	if storedChecksum != "9409c95a142c6a8b64e4babc6d13adadf58418426530652fc4c047505a3bfdf5" {
		t.Fatalf("旧迁移校验和不应被静默改写，实际值=%s", storedChecksum)
	}
}

func TestMigrateAcceptsVerifiedLegacyChecksumAfterLaterMigrations(t *testing.T) {
	database := openMigratedDatabase(t)
	if _, err := database.Exec(`UPDATE schema_migrations SET checksum = ? WHERE version = 1`, "9409c95a142c6a8b64e4babc6d13adadf58418426530652fc4c047505a3bfdf5"); err != nil {
		t.Fatalf("写入旧校验和夹具失败: %v", err)
	}

	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("已完成后续迁移的旧初始库应可继续启动: %v", err)
	}
}

func TestMigrateRejectsVerifiedLegacyChecksumWhenSchemaIsNotLegacyBaseline(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "aggregation-hub.db")
	database, err := storage.Open(databasePath)
	if err != nil {
		t.Fatalf("打开临时 SQLite 数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("关闭临时 SQLite 数据库失败: %v", err)
		}
	})

	initialOnly := fstest.MapFS{
		"0001_initial.sql": &fstest.MapFile{Data: mustReadMigration(t, "0001_initial.sql")},
	}
	if err := storage.Migrate(context.Background(), database, initialOnly); err != nil {
		t.Fatalf("执行旧初始迁移失败: %v", err)
	}
	if _, err := database.Exec(`ALTER TABLE provider_models ADD COLUMN context_window_override_tokens INTEGER`); err != nil {
		t.Fatalf("构造非旧基线数据库失败: %v", err)
	}
	if _, err := database.Exec(`UPDATE schema_migrations SET checksum = ? WHERE version = 1`, "9409c95a142c6a8b64e4babc6d13adadf58418426530652fc4c047505a3bfdf5"); err != nil {
		t.Fatalf("写入旧校验和夹具失败: %v", err)
	}

	err = storage.Migrate(context.Background(), database, migrations.FS)
	if !errors.Is(err, storage.ErrMigrationChecksumMismatch) {
		t.Fatalf("非旧基线的校验和不匹配错误=%v，期望 ErrMigrationChecksumMismatch", err)
	}
}
