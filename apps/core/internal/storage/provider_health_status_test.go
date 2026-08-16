package storage_test

import (
	"context"
	"testing"
	"time"

	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/storage"
)

func TestProviderRepositorySetHealthStatusPreservesDisabledProvider(t *testing.T) {
	database := openMigratedDatabase(t)
	now := time.Now().UTC()
	_, err := database.Exec(`INSERT INTO providers(id,slug,name,adapter_type,auth_type,base_url,lifecycle_status,enabled,timeout_ms,adapter_config_json,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "provider", "provider", "Provider", "openai-compatible", "none", "https://provider.example", "enabled", 1, 1000, "{}", 1, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO providers(id,slug,name,adapter_type,auth_type,base_url,lifecycle_status,enabled,timeout_ms,adapter_config_json,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "disabled", "disabled", "Disabled", "openai-compatible", "none", "https://provider.example", "disabled", 0, 1000, "{}", 1, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	repository, _ := storage.NewProviderRepository(database)
	if err := repository.SetHealthStatus(context.Background(), "provider", provider.ProviderStatusDegraded, now); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := database.QueryRow(`SELECT lifecycle_status FROM providers WHERE id=?`, "provider").Scan(&status); err != nil || status != "degraded" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	if err := repository.SetHealthStatus(context.Background(), "disabled", provider.ProviderStatusDegraded, now); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT lifecycle_status FROM providers WHERE id=?`, "disabled").Scan(&status); err != nil || status != "disabled" {
		t.Fatalf("disabled status=%s err=%v", status, err)
	}
}
