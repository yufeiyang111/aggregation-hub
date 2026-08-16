package storage_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

func TestBackupCreatesVerifiedSnapshotAndKeepsLatestFive(t *testing.T) {
	dataDir, database := openBackupDatabase(t)
	manager, err := storage.NewBackupManager(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO app_settings(key,value_json,updated_at) VALUES(?,?,?)`, "ui.theme", `"light"`, time.Now().UTC().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	var first storage.BackupRecord
	for index := 0; index < 6; index++ {
		backup, err := manager.Create(context.Background())
		if err != nil {
			t.Fatalf("创建第 %d 份备份失败: %v", index+1, err)
		}
		if index == 0 {
			first = backup
		}
	}
	backups, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 5 {
		t.Fatalf("保留备份数量=%d，期望 5", len(backups))
	}
	for _, backup := range backups {
		if backup.ID == first.ID {
			t.Fatalf("最早备份 %s 未被清理", first.ID)
		}
	}
	latestPath := filepath.Join(dataDir, "backups", backups[0].ID+".db")
	snapshot, err := storage.Open(latestPath)
	if err != nil {
		t.Fatalf("打开备份快照失败: %v", err)
	}
	defer snapshot.Close()
	var stored string
	if err := snapshot.QueryRow(`SELECT value_json FROM app_settings WHERE key='ui.theme'`).Scan(&stored); err != nil {
		t.Fatalf("读取备份快照内容失败: %v", err)
	}
	if stored != `"light"` {
		t.Fatalf("备份快照内容=%s", stored)
	}
	var auditCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type='database_backup_created'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 6 {
		t.Fatalf("备份审计数量=%d，期望 6", auditCount)
	}
}

func TestBackupRestoreIsScheduledSafelyAndAppliedBeforeNextOpen(t *testing.T) {
	dataDir, database := openBackupDatabase(t)
	manager, err := storage.NewBackupManager(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := database.Exec(`INSERT INTO app_settings(key,value_json,updated_at) VALUES(?,?,?)`, "ui.theme", `"before"`, now); err != nil {
		t.Fatal(err)
	}
	source, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE app_settings SET value_json=?,updated_at=? WHERE key='ui.theme'`, `"after"`, now+1); err != nil {
		t.Fatal(err)
	}
	schedule, err := manager.ScheduleRestore(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("计划恢复失败: %v", err)
	}
	if !schedule.RestartRequired || schedule.SafetyBackup.ID == "" {
		t.Fatalf("恢复计划=%+v", schedule)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "restore-pending.db")); err != nil {
		t.Fatalf("缺少待恢复快照: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	applied, err := storage.ApplyPendingRestore(dataDir)
	if err != nil || !applied {
		t.Fatalf("应用待恢复快照失败: applied=%v err=%v", applied, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "aggregation-hub.pre-restore.db")); err != nil {
		t.Fatalf("恢复前数据库未保留: %v", err)
	}
	restored, err := storage.Open(filepath.Join(dataDir, "aggregation-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := storage.Migrate(context.Background(), restored, migrations.FS); err != nil {
		t.Fatalf("恢复后迁移校验失败: %v", err)
	}
	var value string
	if err := restored.QueryRow(`SELECT value_json FROM app_settings WHERE key='ui.theme'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != `"before"` {
		t.Fatalf("恢复后的设置=%s，期望 before", value)
	}
	if _, err := manager.ScheduleRestore(context.Background(), "../outside"); err == nil {
		t.Fatal("非法备份标识不应被接受")
	}
}

func TestBackupRestoreStagesSelectedOldestSnapshotBeforeSafetyBackupPruning(t *testing.T) {
	dataDir, database := openBackupDatabase(t)
	manager, err := storage.NewBackupManager(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := database.Exec(`INSERT INTO app_settings(key,value_json,updated_at) VALUES(?,?,?)`, "ui.theme", `"selected"`, now); err != nil {
		t.Fatal(err)
	}
	selected, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dataDir, "backups", selected.ID+".db"), time.Now().Add(-24*time.Hour), time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		if _, err := manager.Create(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`UPDATE app_settings SET value_json=?,updated_at=? WHERE key='ui.theme'`, `"current"`, now+1); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ScheduleRestore(context.Background(), selected.ID); err != nil {
		t.Fatalf("所选最旧备份应在安全备份淘汰前被暂存: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	applied, err := storage.ApplyPendingRestore(dataDir)
	if err != nil || !applied {
		t.Fatalf("应用所选最旧快照失败: applied=%v err=%v", applied, err)
	}
	restored, err := storage.Open(filepath.Join(dataDir, "aggregation-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var restoredValue string
	if err := restored.QueryRow(`SELECT value_json FROM app_settings WHERE key='ui.theme'`).Scan(&restoredValue); err != nil {
		t.Fatal(err)
	}
	if restoredValue != `"selected"` {
		t.Fatalf("恢复值=%s，期望 selected", restoredValue)
	}
}

func TestBackupRejectsCancelledAndInvalidSnapshot(t *testing.T) {
	dataDir, database := openBackupDatabase(t)
	manager, err := storage.NewBackupManager(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Create(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消备份错误=%v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "restore-pending.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ApplyPendingRestore(dataDir); err == nil {
		t.Fatal("损坏待恢复快照不应覆盖原数据库")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "aggregation-hub.db")); err != nil {
		t.Fatalf("原数据库不应丢失: %v", err)
	}
}

func openBackupDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := storage.Open(filepath.Join(dataDir, "aggregation-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return dataDir, database
}

func TestRuntimeSettingsRepositoryUsesVersionedAtomicUpdates(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewSettingsRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := repository.ReadRuntime(context.Background(), []string{"gateway.listen_port"})
	if err != nil || initial.Version != 0 || len(initial.Values) != 0 {
		t.Fatalf("初始运行时设置=%+v err=%v", initial, err)
	}
	updated, err := repository.UpdateRuntime(context.Background(), map[string]json.RawMessage{"gateway.listen_port": json.RawMessage(`19443`)}, 0, time.Now().UTC())
	if err != nil || updated.Version != 1 {
		t.Fatalf("写入运行时设置=%+v err=%v", updated, err)
	}
	if _, err := repository.UpdateRuntime(context.Background(), map[string]json.RawMessage{"gateway.listen_port": json.RawMessage(`20443`)}, 0, time.Now().UTC()); !errors.Is(err, storage.ErrStaleSettings) {
		t.Fatalf("过期版本错误=%v", err)
	}
}
