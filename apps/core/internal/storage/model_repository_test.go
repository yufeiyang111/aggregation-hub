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
