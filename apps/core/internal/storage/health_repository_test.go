package storage_test

import (
	"context"
	"testing"
	"time"

	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/storage"
)

func TestHealthRepositoryStoresSafeRecordsAndPrunesAfterSevenDays(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewHealthRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = database.Exec(`INSERT INTO providers(id,slug,name,adapter_type,auth_type,base_url,credential_ref,lifecycle_status,enabled,timeout_ms,adapter_config_json,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "provider", "provider", "Provider", "openai-compatible", "none", "https://provider.example", nil, "enabled", 1, 1000, "{}", 1, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	latency := int64(12)
	if err := repository.Record(context.Background(), provider.HealthCheck{ID: "health-new", ProviderID: "provider", CheckType: provider.HealthCheckConnection, Status: provider.HealthCheckFailed, LatencyMS: &latency, ErrorCode: "timeout", CheckedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO provider_health_checks(id,provider_id,check_type,status,checked_at) VALUES(?,?,?,?,?)`, "health-old", "provider", "connection", "failed", now.Add(-8*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := repository.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	recent, err := repository.Recent(context.Background(), "provider", 10)
	if err != nil || len(recent) != 1 || recent[0].ErrorCode != "timeout" || recent[0].LatencyMS == nil || *recent[0].LatencyMS != 12 {
		t.Fatalf("records=%+v err=%v", recent, err)
	}
}
func TestHealthRepositoryRejectsUnsafeErrorCode(t *testing.T) {
	repository, _ := storage.NewHealthRepository(openMigratedDatabase(t))
	err := repository.Record(context.Background(), provider.HealthCheck{ID: "health", ProviderID: "provider", CheckType: provider.HealthCheckConnection, Status: provider.HealthCheckFailed, ErrorCode: "raw upstream body", CheckedAt: time.Now()})
	if err == nil {
		t.Fatal("不安全错误代码未拒绝")
	}
}
