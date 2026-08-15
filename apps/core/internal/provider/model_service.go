package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aggregationhub.local/core/internal/id"
)

type ModelServiceOptions struct {
	Now   func() time.Time
	NewID func(time.Time) (string, error)
}

type ModelService struct {
	repository ModelRepository
	now        func() time.Time
	newID      func(time.Time) (string, error)
}

func NewModelService(repository ModelRepository, options ModelServiceOptions) (*ModelService, error) {
	if repository == nil {
		return nil, errors.New("模型仓储不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = id.RandomULID
	}
	return &ModelService{repository: repository, now: options.Now, newID: options.NewID}, nil
}

func (service *ModelService) Enable(ctx context.Context, modelID string, expectedVersion int64) (ModelDTO, error) {
	return service.setEnabled(ctx, modelID, expectedVersion, true)
}

func (service *ModelService) Disable(ctx context.Context, modelID string, expectedVersion int64) (ModelDTO, error) {
	return service.setEnabled(ctx, modelID, expectedVersion, false)
}

// CreateManual 创建本地手工模型。模型默认禁用，只有启用后才会暴露给数据面。
func (service *ModelService) CreateManual(ctx context.Context, input CreateManualModelInput) (ModelDTO, error) {
	if ctx == nil || strings.TrimSpace(input.ProviderID) == "" || len(input.ProviderID) > 64 || strings.TrimSpace(input.UpstreamModelID) != input.UpstreamModelID || len(input.UpstreamModelID) < 1 || len(input.UpstreamModelID) > 255 || strings.TrimSpace(input.DisplayName) != input.DisplayName || len(input.DisplayName) < 1 || len(input.DisplayName) > 255 || invalidPositiveInt64(input.ContextWindowTokens) || invalidPositiveInt64(input.MaxOutputTokens) {
		return ModelDTO{}, ErrInvalidModel
	}
	now := service.now().UTC()
	modelID, err := service.newID(now)
	if err != nil {
		return ModelDTO{}, fmt.Errorf("生成手工模型标识失败: %w", err)
	}
	auditID, err := service.newID(now)
	if err != nil {
		return ModelDTO{}, fmt.Errorf("生成手工模型审计标识失败: %w", err)
	}
	detail, err := json.Marshal(struct {
		Source string `json:"source"`
	}{Source: "manual"})
	if err != nil {
		return ModelDTO{}, fmt.Errorf("编码手工模型审计详情失败: %w", err)
	}
	value, err := service.repository.CreateManual(ctx, CreateManualModelInput{ID: modelID, ProviderID: input.ProviderID, UpstreamModelID: input.UpstreamModelID, DisplayName: input.DisplayName, Capabilities: input.Capabilities, ContextWindowTokens: cloneInt64(input.ContextWindowTokens), MaxOutputTokens: cloneInt64(input.MaxOutputTokens)}, AuditEvent{ID: auditID, EventType: "manual_model_created", EntityType: "model", EntityID: modelID, DetailJSON: detail, CreatedAt: now})
	if err != nil {
		return ModelDTO{}, err
	}
	return modelDTO(value), nil
}

// UpdateLimits 覆盖模型的上下文窗口和最大输出声明；空覆盖对象代表恢复上游同步结果。
func (service *ModelService) UpdateLimits(ctx context.Context, modelID string, input UpdateModelLimitsInput) (ModelDTO, error) {
	if ctx == nil || modelID == "" || len(modelID) > 64 || input.ExpectedVersion < 1 || invalidPositiveInt64(input.LimitOverride.ContextWindowTokens) || invalidPositiveInt64(input.LimitOverride.MaxOutputTokens) {
		return ModelDTO{}, ErrInvalidModel
	}
	now := service.now().UTC()
	auditID, err := service.newID(now)
	if err != nil {
		return ModelDTO{}, fmt.Errorf("生成模型参数覆盖审计标识失败: %w", err)
	}
	mode := "custom"
	eventType := "model_limit_override_updated"
	if input.LimitOverride.Empty() {
		mode = "upstream_default"
		eventType = "model_limit_override_reset"
	}
	detail, err := json.Marshal(struct {
		Mode string `json:"mode"`
	}{Mode: mode})
	if err != nil {
		return ModelDTO{}, fmt.Errorf("编码模型参数覆盖审计详情失败: %w", err)
	}
	value, err := service.repository.SetLimitOverride(ctx, modelID, input.ExpectedVersion, input.LimitOverride, AuditEvent{ID: auditID, EventType: eventType, EntityType: "model", EntityID: modelID, DetailJSON: detail, CreatedAt: now})
	if err != nil {
		return ModelDTO{}, err
	}
	return modelDTO(value), nil
}

// DeleteManual 仅允许删除本地手工模型，避免同步目录被误删。
func (service *ModelService) DeleteManual(ctx context.Context, modelID string, expectedVersion int64) error {
	if ctx == nil || modelID == "" || len(modelID) > 64 || expectedVersion < 1 {
		return ErrInvalidModel
	}
	now := service.now().UTC()
	auditID, err := service.newID(now)
	if err != nil {
		return fmt.Errorf("生成手工模型删除审计标识失败: %w", err)
	}
	detail, err := json.Marshal(struct{}{})
	if err != nil {
		return fmt.Errorf("编码手工模型删除审计详情失败: %w", err)
	}
	return service.repository.SoftDeleteManual(ctx, modelID, expectedVersion, AuditEvent{ID: auditID, EventType: "manual_model_deleted", EntityType: "model", EntityID: modelID, DetailJSON: detail, CreatedAt: now})
}

func (service *ModelService) UpdateCapabilities(ctx context.Context, modelID string, input UpdateModelCapabilitiesInput) (ModelDTO, error) {
	if ctx == nil || modelID == "" || len(modelID) > 64 || input.ExpectedVersion < 1 {
		return ModelDTO{}, ErrInvalidModel
	}
	overrideJSON, err := input.CapabilityOverride.JSON()
	if err != nil || ValidateCapabilityOverride(overrideJSON) != nil {
		return ModelDTO{}, ErrInvalidCapabilityOverride
	}
	now := service.now().UTC()
	auditID, err := service.newID(now)
	if err != nil {
		return ModelDTO{}, fmt.Errorf("生成模型能力覆盖审计标识失败: %w", err)
	}
	mode := "custom"
	eventType := "model_capability_override_updated"
	if input.CapabilityOverride.Empty() {
		mode = "upstream_default"
		eventType = "model_capability_override_reset"
	}
	detail, err := json.Marshal(struct {
		Mode string `json:"mode"`
	}{Mode: mode})
	if err != nil {
		return ModelDTO{}, fmt.Errorf("编码模型能力覆盖审计详情失败: %w", err)
	}
	value, err := service.repository.SetCapabilityOverride(ctx, modelID, input.ExpectedVersion, input.CapabilityOverride, AuditEvent{ID: auditID, EventType: eventType, EntityType: "model", EntityID: modelID, DetailJSON: detail, CreatedAt: now})
	if err != nil {
		return ModelDTO{}, err
	}
	return modelDTO(value), nil
}

func (service *ModelService) setEnabled(ctx context.Context, modelID string, expectedVersion int64, enabled bool) (ModelDTO, error) {
	if ctx == nil {
		return ModelDTO{}, fmt.Errorf("设置模型状态: %w", ErrInvalidModel)
	}
	now := service.now().UTC()
	auditID, err := service.newID(now)
	if err != nil {
		return ModelDTO{}, fmt.Errorf("生成模型审计标识失败: %w", err)
	}
	detail, err := json.Marshal(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: enabled})
	if err != nil {
		return ModelDTO{}, fmt.Errorf("编码模型审计详情失败: %w", err)
	}
	eventType := "model_disabled"
	if enabled {
		eventType = "model_enabled"
	}
	value, err := service.repository.SetEnabled(ctx, modelID, expectedVersion, enabled, AuditEvent{ID: auditID, EventType: eventType, EntityType: "model", EntityID: modelID, DetailJSON: detail, CreatedAt: now})
	if err != nil {
		return ModelDTO{}, err
	}
	return modelDTO(value), nil
}

func modelDTO(value ProviderModel) ModelDTO {
	override, err := ParseCapabilityOverride(value.CapabilityOverrideJSON)
	if err != nil {
		override = CapabilityOverride{}
	}
	effective, err := EffectiveCapabilities(value.Capabilities, value.CapabilityOverrideJSON)
	if err != nil {
		effective = value.Capabilities
	}
	effectiveContextWindow, effectiveMaxOutput := EffectiveModelLimits(value.ContextWindowTokens, value.MaxOutputTokens, value.ContextWindowOverrideTokens, value.MaxOutputOverrideTokens)
	return ModelDTO{
		ID:                  value.ID,
		ProviderID:          value.ProviderID,
		UpstreamModelID:     value.UpstreamModelID,
		PublicModelID:       value.PublicModelID,
		DisplayName:         value.DisplayName,
		Source:              value.Source,
		LifecycleStatus:     value.LifecycleStatus,
		Enabled:             value.Enabled,
		Capabilities:        ModelCapabilitiesDTO{Streaming: effective.Streaming, Tools: effective.Tools, ParallelTools: effective.ParallelTools, Reasoning: effective.Reasoning, Thinking: effective.Thinking, Vision: effective.Vision},
		ContextWindowTokens: cloneInt64(effectiveContextWindow),
		MaxOutputTokens:     cloneInt64(effectiveMaxOutput),
		CapabilitySource:    value.CapabilitySource,
		CapabilityOverride:  override,
		LimitOverride:       ModelLimitOverride{ContextWindowTokens: cloneInt64(value.ContextWindowOverrideTokens), MaxOutputTokens: cloneInt64(value.MaxOutputOverrideTokens)},
		Version:             value.Version,
	}
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func invalidPositiveInt64(value *int64) bool { return value != nil && *value < 1 }
