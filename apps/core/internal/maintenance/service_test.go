package maintenance_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"aggregationhub.local/core/internal/maintenance"
	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

func TestServiceAppliesDefaultsAndRequiresCurrentVersion(t *testing.T) {
	dataDir, database := openDatabase(t)
	settings, err := storage.NewSettingsRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	retention, err := storage.NewRetentionRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := storage.NewBackupManager(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service, err := maintenance.NewService(settings, retention, backups)
	if err != nil {
		t.Fatal(err)
	}

	initial, err := service.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if initial.ListenPort != maintenance.DefaultListenPort || initial.RequestTimeoutMS != maintenance.DefaultRequestTimeoutMS || initial.RequestRetentionDays != maintenance.DefaultRequestRetentionDays || initial.Version != 0 {
		t.Fatalf("默认设置=%+v", initial)
	}
	updated, err := service.UpdateSettings(context.Background(), maintenance.UpdateRuntimeSettingsInput{ListenPort: 19443, RequestTimeoutMS: 120000, RequestRetentionDays: 90, Version: initial.Version})
	if err != nil {
		t.Fatalf("更新设置失败: %v", err)
	}
	if !updated.RestartRequired || updated.Settings.Version != 1 {
		t.Fatalf("更新结果=%+v", updated)
	}
	if _, err := service.UpdateSettings(context.Background(), maintenance.UpdateRuntimeSettingsInput{ListenPort: 20443, RequestTimeoutMS: 120000, RequestRetentionDays: 90, Version: 0}); !errors.Is(err, storage.ErrStaleSettings) {
		t.Fatalf("过期设置版本错误=%v", err)
	}
	if _, err := service.UpdateSettings(context.Background(), maintenance.UpdateRuntimeSettingsInput{ListenPort: 80, RequestTimeoutMS: 120000, RequestRetentionDays: 90, Version: 1}); !errors.Is(err, maintenance.ErrInvalidRuntimeSettings) {
		t.Fatalf("非法端口错误=%v", err)
	}
}

func openDatabase(t *testing.T) (string, *sql.DB) {
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
	t.Cleanup(func() { _ = database.Close() })
	return dataDir, database
}
