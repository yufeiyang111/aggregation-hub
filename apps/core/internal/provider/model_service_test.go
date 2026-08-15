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
	auditIDs := &idSequence{values: []string{"01H00000000000000000000201", "01H00000000000000000000202"}}
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
	if _, err := modelService.Enable(nil, model.ID, enabled.Version); !errors.Is(err, provider.ErrInvalidModel) {
		t.Fatalf("空上下文错误=%v", err)
	}
	var detail string
	if err := database.QueryRow(`SELECT detail_json FROM audit_events WHERE id=?`, "01H00000000000000000000201").Scan(&detail); err != nil || detail != `{"enabled":true}` {
		t.Fatalf("模型审计详情错误=%q, %v", detail, err)
	}
}
