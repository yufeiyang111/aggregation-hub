package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/storage"
)

func TestModelServiceSetsStateWithOptimisticVersionAndSafeDTO(t *testing.T) {
	database, _, _, providerService := openProviderService(t)
	created, err := providerService.Create(context.Background(), provider.CreateProviderInput{
		Slug:              "model-service",
		Name:              "模型服务测试",
		AdapterType:       "openai-compatible",
		AuthType:          provider.AuthTypeNone,
		BaseURL:           "https://example.test/v1",
		Timeout:           30 * time.Second,
		AdapterConfigJSON: json.RawMessage(`{"wire_api":"chat_completions"}`),
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	now := time.UnixMilli(1_700_000_030_000).UTC()
	if err := models.ReconcileSyncedModels(context.Background(), created.ID, created.Slug, []provider.SyncedModel{{UpstreamModelID: "model-service-upstream", DisplayName: "可启用模型", Source: provider.ModelSourceUpstream, Capabilities: provider.Capabilities{Streaming: true, Tools: true}, CapabilitySource: "upstream"}}, now); err != nil {
		t.Fatalf("同步模型失败: %v", err)
	}
	model, err := models.FindByPublicID(context.Background(), "model-service/model-service-upstream")
	if err != nil {
		t.Fatalf("读取模型失败: %v", err)
	}
	auditIDs := &idSequence{values: []string{"01H00000000000000000000201", "01H00000000000000000000202", "01H00000000000000000000203", "01H00000000000000000000204"}}
	modelService, err := provider.NewModelService(models, provider.ModelServiceOptions{Now: func() time.Time { return now }, NewID: auditIDs.next})
	if err != nil {
		t.Fatalf("创建模型服务失败: %v", err)
	}
	enabled, err := modelService.Enable(context.Background(), model.ID, model.Version)
	if err != nil {
		t.Fatalf("启用模型失败: %v", err)
	}
	if !enabled.Enabled || enabled.Version != model.Version+1 || !enabled.Capabilities.Tools || enabled.ProviderID != created.ID {
		t.Fatalf("模型 DTO 错误: %+v", enabled)
	}
	if _, err := modelService.Disable(context.Background(), model.ID, model.Version); !errors.Is(err, provider.ErrStaleResource) {
		t.Fatalf("旧版本禁用错误=%v", err)
	}
	tools := false
	updated, err := modelService.UpdateCapabilities(context.Background(), model.ID, provider.UpdateModelCapabilitiesInput{ExpectedVersion: enabled.Version, CapabilityOverride: provider.CapabilityOverride{Tools: &tools}})
	if err != nil || updated.Capabilities.Tools || updated.CapabilityOverride.Tools == nil || *updated.CapabilityOverride.Tools || updated.Version != enabled.Version+1 {
		t.Fatalf("更新模型能力覆盖错误: %+v, %v", updated, err)
	}
	reset, err := modelService.UpdateCapabilities(context.Background(), model.ID, provider.UpdateModelCapabilitiesInput{ExpectedVersion: updated.Version, CapabilityOverride: provider.CapabilityOverride{}})
	if err != nil || !reset.Capabilities.Tools || !reset.CapabilityOverride.Empty() || reset.Version != updated.Version+1 {
		t.Fatalf("恢复上游模型能力错误: %+v, %v", reset, err)
	}
	if _, err := modelService.Enable(nil, model.ID, enabled.Version); !errors.Is(err, provider.ErrInvalidModel) {
		t.Fatalf("空上下文错误=%v", err)
	}
	var detail string
	if err := database.QueryRow(`SELECT detail_json FROM audit_events WHERE id=?`, "01H00000000000000000000201").Scan(&detail); err != nil || detail != `{"enabled":true}` {
		t.Fatalf("模型审计详情错误=%q, %v", detail, err)
	}
	if err := database.QueryRow(`SELECT detail_json FROM audit_events WHERE id=?`, "01H00000000000000000000204").Scan(&detail); err != nil || detail != `{"mode":"upstream_default"}` {
		t.Fatalf("模型能力恢复审计详情错误=%q, %v", detail, err)
	}
}

func TestModelServiceCreatesManualModelAndUpdatesLimits(t *testing.T) {
	database, _, _, providerService := openProviderService(t)
	createdProvider, err := providerService.Create(context.Background(), provider.CreateProviderInput{
		Slug:              "manual-service",
		Name:              "手工模型服务测试",
		AdapterType:       "openai-compatible",
		AuthType:          provider.AuthTypeNone,
		BaseURL:           "https://example.test/v1",
		Timeout:           30 * time.Second,
		AdapterConfigJSON: json.RawMessage(`{"wire_api":"chat_completions"}`),
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	models, err := storage.NewModelRepository(database)
	if err != nil {
		t.Fatalf("创建模型仓储失败: %v", err)
	}
	ids := &idSequence{values: []string{"01H00000000000000000000301", "01H00000000000000000000302", "01H00000000000000000000303", "01H00000000000000000000304"}}
	modelService, err := provider.NewModelService(models, provider.ModelServiceOptions{Now: func() time.Time { return time.UnixMilli(1_700_000_040_000).UTC() }, NewID: ids.next})
	if err != nil {
		t.Fatalf("创建模型服务失败: %v", err)
	}
	contextWindow, maxOutput := int64(128000), int64(8192)
	manual, err := modelService.CreateManual(context.Background(), provider.CreateManualModelInput{ProviderID: createdProvider.ID, UpstreamModelID: "manual-service-model", DisplayName: "手工服务模型", Capabilities: provider.Capabilities{Streaming: true, Tools: true}, ContextWindowTokens: &contextWindow, MaxOutputTokens: &maxOutput})
	if err != nil {
		t.Fatalf("创建手工模型失败: %v", err)
	}
	if manual.Source != provider.ModelSourceManual || manual.Enabled || manual.PublicModelID != "manual-service/manual-service-model" || manual.ContextWindowTokens == nil || *manual.ContextWindowTokens != contextWindow {
		t.Fatalf("手工模型 DTO 错误: %+v", manual)
	}
	overrideContext, overrideOutput := int64(100000), int64(4096)
	updated, err := modelService.UpdateLimits(context.Background(), manual.ID, provider.UpdateModelLimitsInput{ExpectedVersion: manual.Version, LimitOverride: provider.ModelLimitOverride{ContextWindowTokens: &overrideContext, MaxOutputTokens: &overrideOutput}})
	if err != nil || updated.ContextWindowTokens == nil || *updated.ContextWindowTokens != overrideContext || updated.MaxOutputTokens == nil || *updated.MaxOutputTokens != overrideOutput || updated.LimitOverride.ContextWindowTokens == nil {
		t.Fatalf("模型参数 DTO 错误: %+v, %v", updated, err)
	}
	if err := modelService.DeleteManual(context.Background(), manual.ID, updated.Version); err != nil {
		t.Fatalf("删除手工模型失败: %v", err)
	}
	if _, err := models.FindByID(context.Background(), manual.ID); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("删除后的模型仍可查询: %v", err)
	}
}
