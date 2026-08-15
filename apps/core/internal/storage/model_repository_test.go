package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/storage"
)

func TestModelRepositoryReconcilesModelsWithoutOverwritingUserState(t *testing.T) {
	database := openMigratedDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := testProvider("01H00000000000000000000021", "catalog", nil)
	if err := providers.Create(context.Background(), value, testAudit("01H00000000000000000000022", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	now := time.UnixMilli(1_700_000_001_000).UTC()
	first := provider.SyncedModel{UpstreamModelID: "model-x", DisplayName: "Model X", Source: provider.ModelSourceUpstream, Capabilities: provider.Capabilities{Streaming: true, Tools: true}, CapabilitySource: "upstream"}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{first}, now); err != nil {
		t.Fatalf("首次同步失败: %v", err)
	}
	model, err := models.FindByPublicID(context.Background(), "catalog/model-x")
	if err != nil {
		t.Fatalf("按公开 ID 查询模型失败: %v", err)
	}
	if model.Enabled || model.LifecycleStatus != provider.ModelStatusAvailable || model.PublicModelID != "catalog/model-x" {
		t.Fatalf("新同步模型状态错误: %+v", model)
	}
	if _, err := database.Exec(`UPDATE provider_models SET enabled=1, capability_override_json=? WHERE id=?`, `{"supports_tools":true}`, model.ID); err != nil {
		t.Fatalf("写入用户覆盖失败: %v", err)
	}
	second := first
	second.Capabilities = provider.Capabilities{Streaming: true}
	second.DisplayName = "Model X 最新"
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{second}, now.Add(time.Minute)); err != nil {
		t.Fatalf("再次同步失败: %v", err)
	}
	model, err = models.FindByPublicID(context.Background(), "catalog/model-x")
	if err != nil {
		t.Fatalf("再次读取模型失败: %v", err)
	}
	if !model.Enabled || string(model.CapabilityOverrideJSON) != `{"supports_tools":true}` || model.DisplayName != "Model X 最新" {
		t.Fatalf("同步覆盖了用户状态: %+v", model)
	}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, nil, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("标记缺失模型失败: %v", err)
	}
	model, err = models.FindByPublicID(context.Background(), "catalog/model-x")
	if err != nil || model.LifecycleStatus != provider.ModelStatusMissingUpstream {
		t.Fatalf("缺失模型状态错误: %+v, %v", model, err)
	}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{second}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("恢复模型同步失败: %v", err)
	}
	model, err = models.FindByPublicID(context.Background(), "catalog/model-x")
	if err != nil || model.LifecycleStatus != provider.ModelStatusAvailable || !model.Enabled {
		t.Fatalf("模型恢复状态错误: %+v, %v", model, err)
	}
}

func TestModelRepositoryRejectsInvalidAndDuplicateInput(t *testing.T) {
	database := openMigratedDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := testProvider("01H00000000000000000000031", "models", nil)
	if err := providers.Create(context.Background(), value, testAudit("01H00000000000000000000032", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	input := provider.SyncedModel{UpstreamModelID: "model-y", DisplayName: "Model Y", Source: provider.ModelSourceUpstream, CapabilitySource: "upstream"}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{input, input}, time.Now()); !errors.Is(err, provider.ErrInvalidModel) {
		t.Fatalf("重复模型输入错误=%v", err)
	}
	if _, err := models.FindByPublicID(context.Background(), "missing"); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("非法公开 ID 错误=%v", err)
	}
	if _, err := json.Marshal(input); err != nil {
		t.Fatalf("测试输入不应无法编码: %v", err)
	}
}

func TestModelRepositoryListsFiltersAndAuditsEnablement(t *testing.T) {
	database := openMigratedDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := testProvider("01H00000000000000000000041", "filter", nil)
	if err := providers.Create(context.Background(), value, testAudit("01H00000000000000000000042", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	now := time.UnixMilli(1_700_000_010_000).UTC()
	discovered := []provider.SyncedModel{
		{UpstreamModelID: "tool-model", DisplayName: "Tool Model", Source: provider.ModelSourceUpstream, Capabilities: provider.Capabilities{Streaming: true, Tools: true}, CapabilitySource: "upstream"},
		{UpstreamModelID: "plain-model", DisplayName: "Plain Model", Source: provider.ModelSourceUpstream, Capabilities: provider.Capabilities{Streaming: true}, CapabilitySource: "upstream"},
	}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, discovered, now); err != nil {
		t.Fatalf("同步模型失败: %v", err)
	}
	page, err := models.List(context.Background(), provider.ModelPageQuery{PageSize: 1, ProviderID: value.ID})
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("模型分页结果错误: %+v, %v", page, err)
	}
	page, err = models.List(context.Background(), provider.ModelPageQuery{PageSize: 10, ProviderID: value.ID, Capability: "tools"})
	if err != nil || len(page.Items) != 1 || page.Items[0].PublicModelID != "filter/tool-model" {
		t.Fatalf("能力筛选错误: %+v, %v", page, err)
	}
	page, err = models.List(context.Background(), provider.ModelPageQuery{PageSize: 10, Search: "Tool"})
	if err != nil || len(page.Items) != 1 || page.Items[0].DisplayName != "Tool Model" {
		t.Fatalf("搜索筛选错误: %+v, %v", page, err)
	}
	model := page.Items[0]
	enabled, err := models.SetEnabled(context.Background(), model.ID, model.Version, true, provider.AuditEvent{ID: "01H00000000000000000000043", EventType: "model_enabled", EntityType: "model", EntityID: model.ID, DetailJSON: json.RawMessage(`{"enabled":true}`), CreatedAt: now.Add(time.Minute)})
	if err != nil || !enabled.Enabled || enabled.Version != model.Version+1 {
		t.Fatalf("启用模型错误: %+v, %v", enabled, err)
	}
	filterEnabled := true
	page, err = models.List(context.Background(), provider.ModelPageQuery{PageSize: 10, Enabled: &filterEnabled})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != model.ID {
		t.Fatalf("启用状态筛选错误: %+v, %v", page, err)
	}
	if _, err := models.SetEnabled(context.Background(), model.ID, model.Version, false, provider.AuditEvent{ID: "01H00000000000000000000044", EventType: "model_disabled", EntityType: "model", EntityID: model.ID, DetailJSON: json.RawMessage(`{"enabled":false}`), CreatedAt: now.Add(2 * time.Minute)}); !errors.Is(err, provider.ErrStaleResource) {
		t.Fatalf("旧版本启停错误=%v", err)
	}
	var eventType string
	if err := database.QueryRow(`SELECT event_type FROM audit_events WHERE id=?`, "01H00000000000000000000043").Scan(&eventType); err != nil || eventType != "model_enabled" {
		t.Fatalf("模型审计事件错误: %q, %v", eventType, err)
	}
	if _, err := models.List(context.Background(), provider.ModelPageQuery{PageSize: 10, Capability: "unknown"}); !errors.Is(err, provider.ErrInvalidModel) {
		t.Fatalf("非法能力筛选错误=%v", err)
	}
}

func TestModelRepositoryDoesNotEnableMissingUpstreamModel(t *testing.T) {
	database := openMigratedDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := testProvider("01H00000000000000000000051", "missing", nil)
	if err := providers.Create(context.Background(), value, testAudit("01H00000000000000000000052", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	now := time.UnixMilli(1_700_000_020_000).UTC()
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{{UpstreamModelID: "gone", DisplayName: "Gone", Source: provider.ModelSourceUpstream, CapabilitySource: "upstream"}}, now); err != nil {
		t.Fatalf("同步模型失败: %v", err)
	}
	model, err := models.FindByPublicID(context.Background(), "missing/gone")
	if err != nil {
		t.Fatalf("读取模型失败: %v", err)
	}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, nil, now.Add(time.Minute)); err != nil {
		t.Fatalf("标记缺失模型失败: %v", err)
	}
	model, err = models.FindByID(context.Background(), model.ID)
	if err != nil {
		t.Fatalf("按 ID 读取模型失败: %v", err)
	}
	if _, err := models.SetEnabled(context.Background(), model.ID, model.Version, true, provider.AuditEvent{ID: "01H00000000000000000000053", EventType: "model_enabled", EntityType: "model", EntityID: model.ID, DetailJSON: json.RawMessage(`{"enabled":true}`), CreatedAt: now.Add(2 * time.Minute)}); !errors.Is(err, provider.ErrInvalidModel) {
		t.Fatalf("缺失模型不应启用，错误=%v", err)
	}
}
