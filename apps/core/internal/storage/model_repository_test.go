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

func TestModelRepositoryPersistsCapabilityOverrideAcrossSync(t *testing.T) {
	database := openMigratedDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := testProvider("01H00000000000000000000061", "override", nil)
	if err := providers.Create(context.Background(), value, testAudit("01H00000000000000000000062", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	now := time.UnixMilli(1_700_000_030_000).UTC()
	discovered := provider.SyncedModel{UpstreamModelID: "override-model", DisplayName: "Override Model", Source: provider.ModelSourceUpstream, Capabilities: provider.Capabilities{Streaming: true}, CapabilitySource: "upstream"}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{discovered}, now); err != nil {
		t.Fatalf("同步模型失败: %v", err)
	}
	model, err := models.FindByPublicID(context.Background(), "override/override-model")
	if err != nil {
		t.Fatalf("读取模型失败: %v", err)
	}
	tools := true
	updated, err := models.SetCapabilityOverride(context.Background(), model.ID, model.Version, provider.CapabilityOverride{Tools: &tools}, provider.AuditEvent{ID: "01H00000000000000000000063", EventType: "model_capability_override_updated", EntityType: "model", EntityID: model.ID, DetailJSON: json.RawMessage(`{"mode":"custom"}`), CreatedAt: now.Add(time.Minute)})
	if err != nil || updated.Version != model.Version+1 {
		t.Fatalf("设置模型能力覆盖错误: %+v, %v", updated, err)
	}
	effective, err := provider.EffectiveCapabilities(updated.Capabilities, updated.CapabilityOverrideJSON)
	if err != nil || !effective.Tools {
		t.Fatalf("更新后有效能力错误: %+v, %v", effective, err)
	}
	page, err := models.List(context.Background(), provider.ModelPageQuery{PageSize: 10, Capability: "tools"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != model.ID {
		t.Fatalf("能力筛选必须使用覆盖后的有效能力: %+v, %v", page, err)
	}
	if _, err := models.SetCapabilityOverride(context.Background(), model.ID, model.Version, provider.CapabilityOverride{}, provider.AuditEvent{ID: "01H00000000000000000000064", EventType: "model_capability_override_reset", EntityType: "model", EntityID: model.ID, DetailJSON: json.RawMessage(`{"mode":"upstream_default"}`), CreatedAt: now.Add(2 * time.Minute)}); !errors.Is(err, provider.ErrStaleResource) {
		t.Fatalf("旧版本不应覆盖模型能力，错误=%v", err)
	}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{discovered}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("再次同步模型失败: %v", err)
	}
	synced, err := models.FindByID(context.Background(), model.ID)
	if err != nil {
		t.Fatalf("读取同步后模型失败: %v", err)
	}
	effective, err = provider.EffectiveCapabilities(synced.Capabilities, synced.CapabilityOverrideJSON)
	if err != nil || !effective.Tools {
		t.Fatalf("同步不应清除用户覆盖: %+v, %v", effective, err)
	}
}

func TestModelRepositoryCreatesManualModelsAndPersistsLimitOverrides(t *testing.T) {
	database := openMigratedDatabase(t)
	providers, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	value := testProvider("01H00000000000000000000101", "manual", nil)
	if err := providers.Create(context.Background(), value, testAudit("01H00000000000000000000102", value.ID)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	contextWindow, maxOutput := int64(128000), int64(8192)
	created, err := models.CreateManual(context.Background(), provider.CreateManualModelInput{
		ID:                  "01H00000000000000000000103",
		ProviderID:          value.ID,
		UpstreamModelID:     "manual-model",
		DisplayName:         "手工模型",
		Capabilities:        provider.Capabilities{Streaming: true, Tools: true},
		ContextWindowTokens: &contextWindow,
		MaxOutputTokens:     &maxOutput,
	}, provider.AuditEvent{ID: "01H00000000000000000000104", EventType: "manual_model_created", EntityType: "model", EntityID: "01H00000000000000000000103", DetailJSON: json.RawMessage(`{"source":"manual"}`), CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("创建手工模型失败: %v", err)
	}
	if created.Source != provider.ModelSourceManual || created.Enabled || created.CapabilitySource != "manual" || created.LifecycleStatus != provider.ModelStatusAvailable {
		t.Fatalf("手工模型默认状态错误: %+v", created)
	}
	if created.ContextWindowTokens == nil || *created.ContextWindowTokens != contextWindow || created.MaxOutputTokens == nil || *created.MaxOutputTokens != maxOutput {
		t.Fatalf("手工模型基础参数错误: %+v", created)
	}
	if err := models.ReconcileSyncedModels(context.Background(), value.ID, value.Slug, []provider.SyncedModel{{UpstreamModelID: "manual-model", DisplayName: "上游同名模型", Source: provider.ModelSourceUpstream, Capabilities: provider.Capabilities{Streaming: false}, CapabilitySource: "upstream"}}, time.Now()); err != nil {
		t.Fatalf("同步同名上游模型失败: %v", err)
	}
	preserved, err := models.FindByID(context.Background(), created.ID)
	if err != nil || preserved.DisplayName != "手工模型" || preserved.Source != provider.ModelSourceManual || preserved.Capabilities.Streaming != created.Capabilities.Streaming {
		t.Fatalf("同步不应覆盖手工模型: %+v, %v", preserved, err)
	}
	overrideContext, overrideOutput := int64(100000), int64(4096)
	updated, err := models.SetLimitOverride(context.Background(), created.ID, created.Version, provider.ModelLimitOverride{ContextWindowTokens: &overrideContext, MaxOutputTokens: &overrideOutput}, provider.AuditEvent{ID: "01H00000000000000000000105", EventType: "model_limit_override_updated", EntityType: "model", EntityID: created.ID, DetailJSON: json.RawMessage(`{"mode":"custom"}`), CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("保存模型参数覆盖失败: %v", err)
	}
	if updated.ContextWindowOverrideTokens == nil || *updated.ContextWindowOverrideTokens != overrideContext || updated.MaxOutputOverrideTokens == nil || *updated.MaxOutputOverrideTokens != overrideOutput || updated.Version != created.Version+1 {
		t.Fatalf("模型参数覆盖保存错误: %+v", updated)
	}
	reset, err := models.SetLimitOverride(context.Background(), created.ID, updated.Version, provider.ModelLimitOverride{}, provider.AuditEvent{ID: "01H00000000000000000000106", EventType: "model_limit_override_reset", EntityType: "model", EntityID: created.ID, DetailJSON: json.RawMessage(`{"mode":"upstream_default"}`), CreatedAt: time.Now()})
	if err != nil || reset.ContextWindowOverrideTokens != nil || reset.MaxOutputOverrideTokens != nil {
		t.Fatalf("模型参数覆盖恢复错误: %+v, %v", reset, err)
	}
	if err := models.SoftDeleteManual(context.Background(), created.ID, reset.Version, provider.AuditEvent{ID: "01H00000000000000000000107", EventType: "manual_model_deleted", EntityType: "model", EntityID: created.ID, DetailJSON: json.RawMessage(`{}`), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("删除手工模型失败: %v", err)
	}
	if _, err := models.FindByID(context.Background(), created.ID); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("删除后的手工模型仍可见: %v", err)
	}
}
