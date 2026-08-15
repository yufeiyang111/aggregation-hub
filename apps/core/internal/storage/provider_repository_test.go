package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/storage"
)

func testProvider(id string, slug string, reference *credential.Ref) provider.Provider {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	return provider.Provider{ID: id, Slug: slug, Name: "测试 Provider", AdapterType: "openai-compatible", AuthType: provider.AuthTypeNone, BaseURL: "https://example.test/v1", CredentialRef: reference, LifecycleStatus: provider.ProviderStatusDraft, Timeout: 30 * time.Second, AdapterConfigJSON: json.RawMessage(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
}

func testAudit(id string, entityID string) provider.AuditEvent {
	return provider.AuditEvent{ID: id, EventType: "provider_created", EntityType: "provider", EntityID: entityID, DetailJSON: json.RawMessage(`{"provider_slug":"test"}`), CreatedAt: time.UnixMilli(1_700_000_000_000).UTC()}
}

func TestProviderRepositoryCreatesUpdatesListsAndSoftDeletes(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	first := testProvider("01H00000000000000000000001", "alpha", nil)
	if err := repository.Create(context.Background(), first, testAudit("01H00000000000000000000002", first.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	loaded, err := repository.FindByID(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("读取 Provider 失败: %v", err)
	}
	if loaded.Timeout != 30*time.Second || loaded.Version != 1 || loaded.CredentialRef != nil {
		t.Fatalf("Provider round-trip 错误: %+v", loaded)
	}

	loaded.Name = "更新后的 Provider"
	loaded.UpdatedAt = loaded.UpdatedAt.Add(time.Minute)
	updated, err := repository.Update(context.Background(), loaded, 1, testAudit("01H00000000000000000000003", first.ID))
	if err != nil {
		t.Fatalf("更新 Provider 失败: %v", err)
	}
	if updated.Version != 2 || updated.Name != "更新后的 Provider" {
		t.Fatalf("更新结果错误: %+v", updated)
	}
	if _, err := repository.Update(context.Background(), loaded, 1, testAudit("01H00000000000000000000004", first.ID)); !errors.Is(err, provider.ErrStaleResource) {
		t.Fatalf("旧版本更新错误=%v，期望 ErrStaleResource", err)
	}

	second := testProvider("01H00000000000000000000005", "bravo", nil)
	if err := repository.Create(context.Background(), second, testAudit("01H00000000000000000000006", second.ID)); err != nil {
		t.Fatalf("创建第二个 Provider 失败: %v", err)
	}
	page, err := repository.List(context.Background(), provider.ProviderPageQuery{PageSize: 1})
	if err != nil {
		t.Fatalf("分页列出 Provider 失败: %v", err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("分页结果错误: %+v", page)
	}
	page, err = repository.List(context.Background(), provider.ProviderPageQuery{Cursor: page.NextCursor, PageSize: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != second.ID {
		t.Fatalf("下一页结果错误: %+v, %v", page, err)
	}
	if _, err := repository.List(context.Background(), provider.ProviderPageQuery{PageSize: 201}); !errors.Is(err, provider.ErrUnsupportedPagination) {
		t.Fatalf("越界分页错误=%v", err)
	}

	if err := repository.SoftDelete(context.Background(), first.ID, updated.Version, testAudit("01H00000000000000000000007", first.ID)); err != nil {
		t.Fatalf("软删除 Provider 失败: %v", err)
	}
	if _, err := repository.FindByID(context.Background(), first.ID); !errors.Is(err, provider.ErrProviderNotFound) {
		t.Fatalf("软删除后读取错误=%v", err)
	}
	var deletedModels int
	if err := database.QueryRow(`SELECT COUNT(*) FROM provider_models WHERE provider_id=? AND lifecycle_status='deleted'`, first.ID).Scan(&deletedModels); err != nil {
		t.Fatalf("查询已删除模型失败: %v", err)
	}
}

func TestProviderRepositoryRejectsDuplicateSlugAndCorruptJSON(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	first := testProvider("01H00000000000000000000011", "same", nil)
	if err := repository.Create(context.Background(), first, testAudit("01H00000000000000000000012", first.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	duplicate := testProvider("01H00000000000000000000013", "same", nil)
	if err := repository.Create(context.Background(), duplicate, testAudit("01H00000000000000000000014", duplicate.ID)); !errors.Is(err, provider.ErrDuplicateProvider) {
		t.Fatalf("重复 slug 错误=%v", err)
	}
	if _, err := database.Exec(`UPDATE providers SET adapter_config_json='not-json' WHERE id=?`, first.ID); err != nil {
		t.Fatalf("写入损坏 JSON 失败: %v", err)
	}
	if _, err := repository.FindByID(context.Background(), first.ID); err == nil {
		t.Fatal("损坏 JSON 不应被静默接受")
	}
}
