package routing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

func openRoutingDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := storage.Open(filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatalf("打开临时数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("迁移临时数据库失败: %v", err)
	}
	return database
}

func routingProvider(id string, slug string, authType provider.AuthType, reference *credential.Ref) provider.Provider {
	now := time.UnixMilli(1_700_000_100_000).UTC()
	return provider.Provider{ID: id, Slug: slug, Name: slug, AdapterType: "openai-compatible", AuthType: authType, BaseURL: "https://" + slug + ".example.test/v1", CredentialRef: reference, LifecycleStatus: provider.ProviderStatusEnabled, Enabled: true, Timeout: 30 * time.Second, AdapterConfigJSON: json.RawMessage(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
}

func routingAudit(id string, entityID string) provider.AuditEvent {
	return provider.AuditEvent{ID: id, EventType: "provider_created", EntityType: "provider", EntityID: entityID, DetailJSON: json.RawMessage(`{}`), CreatedAt: time.UnixMilli(1_700_000_100_000).UTC()}
}

func addRoutableModel(t *testing.T, database *sql.DB, models *storage.ModelRepository, providerID string, slug string, capabilities provider.Capabilities) {
	t.Helper()
	model := provider.SyncedModel{UpstreamModelID: "model-x", DisplayName: slug + " model", Source: provider.ModelSourceUpstream, Capabilities: capabilities, CapabilitySource: "upstream"}
	if err := models.ReconcileSyncedModels(context.Background(), providerID, slug, []provider.SyncedModel{model}, time.UnixMilli(1_700_000_101_000).UTC()); err != nil {
		t.Fatalf("同步模型失败: %v", err)
	}
	if _, err := database.Exec(`UPDATE provider_models SET enabled=1 WHERE provider_id=? AND upstream_model_id='model-x'`, providerID); err != nil {
		t.Fatalf("启用模型失败: %v", err)
	}
}

func TestRouterResolvesNamespacedSameUpstreamModelDeterministically(t *testing.T) {
	database := openRoutingDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	ref := credential.Ref("routing/provider-a/auth")
	first := routingProvider("01H00000000000000000000301", "provider-a", provider.AuthTypeAPIKey, &ref)
	second := routingProvider("01H00000000000000000000302", "provider-b", provider.AuthTypeNone, nil)
	if err := providers.Create(context.Background(), first, routingAudit("01H00000000000000000000303", first.ID)); err != nil {
		t.Fatalf("创建第一个 Provider 失败: %v", err)
	}
	if err := providers.Create(context.Background(), second, routingAudit("01H00000000000000000000304", second.ID)); err != nil {
		t.Fatalf("创建第二个 Provider 失败: %v", err)
	}
	addRoutableModel(t, database, models, first.ID, first.Slug, provider.Capabilities{Streaming: true})
	addRoutableModel(t, database, models, second.ID, second.Slug, provider.Capabilities{Tools: true})
	router, err := routing.New(providers, models)
	if err != nil {
		t.Fatalf("创建 Router 失败: %v", err)
	}
	firstPlan, err := router.Resolve(context.Background(), "provider-a/model-x", provider.RequiredCapabilities{Streaming: true})
	if err != nil {
		t.Fatalf("解析第一个命名空间模型失败: %v", err)
	}
	secondPlan, err := router.Resolve(context.Background(), "provider-b/model-x", provider.RequiredCapabilities{Tools: true})
	if err != nil {
		t.Fatalf("解析第二个命名空间模型失败: %v", err)
	}
	if firstPlan.ProviderID != first.ID || firstPlan.UpstreamModelID != "model-x" || firstPlan.CredentialRef == nil || secondPlan.ProviderID != second.ID || secondPlan.CredentialRef != nil {
		t.Fatalf("命名空间路由结果错误: first=%+v second=%+v", firstPlan, secondPlan)
	}
	*firstPlan.CredentialRef = credential.Ref("routing/mutated")
	again, err := router.Resolve(context.Background(), "provider-a/model-x", provider.RequiredCapabilities{})
	if err != nil || again.CredentialRef == nil || *again.CredentialRef != ref {
		t.Fatalf("路由计划泄露可变引用: %+v, %v", again, err)
	}
}

func TestRouterRejectsInvalidUnavailableAndUnsupportedRoutes(t *testing.T) {
	database := openRoutingDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := routingProvider("01H00000000000000000000311", "blocked", provider.AuthTypeNone, nil)
	if err := providers.Create(context.Background(), value, routingAudit("01H00000000000000000000312", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	addRoutableModel(t, database, models, value.ID, value.Slug, provider.Capabilities{Streaming: true})
	router, err := routing.New(providers, models)
	if err != nil {
		t.Fatalf("创建 Router 失败: %v", err)
	}
	if _, err := router.Resolve(context.Background(), "invalid", provider.RequiredCapabilities{}); !errors.Is(err, routing.ErrInvalidPublicModelID) {
		t.Fatalf("非法模型 ID 错误=%v", err)
	}
	if _, err := router.Resolve(context.Background(), "blocked/model-x", provider.RequiredCapabilities{Tools: true}); err == nil {
		t.Fatal("缺失 Tools 能力不应路由成功")
	} else {
		var unsupported *provider.UnsupportedCapabilityError
		if !errors.As(err, &unsupported) || unsupported.Feature != "tools" {
			t.Fatalf("能力错误=%v", err)
		}
	}
	if _, err := database.Exec(`UPDATE provider_models SET enabled=0 WHERE provider_id=?`, value.ID); err != nil {
		t.Fatalf("禁用模型失败: %v", err)
	}
	if _, err := router.Resolve(context.Background(), "blocked/model-x", provider.RequiredCapabilities{}); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("禁用模型错误=%v", err)
	}
	if _, err := database.Exec(`UPDATE provider_models SET enabled=1 WHERE provider_id=?`, value.ID); err != nil {
		t.Fatalf("重新启用模型失败: %v", err)
	}
	if _, err := database.Exec(`UPDATE providers SET lifecycle_status='auth_required' WHERE id=?`, value.ID); err != nil {
		t.Fatalf("更新 Provider 状态失败: %v", err)
	}
	if _, err := router.Resolve(context.Background(), "blocked/model-x", provider.RequiredCapabilities{}); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("auth_required Provider 错误=%v", err)
	}
}
