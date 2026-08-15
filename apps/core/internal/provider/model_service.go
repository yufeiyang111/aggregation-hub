package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	return ModelDTO{
		ID:                     value.ID,
		ProviderID:             value.ProviderID,
		UpstreamModelID:        value.UpstreamModelID,
		PublicModelID:          value.PublicModelID,
		DisplayName:            value.DisplayName,
		Source:                 value.Source,
		LifecycleStatus:        value.LifecycleStatus,
		Enabled:                value.Enabled,
		Capabilities:           ModelCapabilitiesDTO{Streaming: value.Capabilities.Streaming, Tools: value.Capabilities.Tools, ParallelTools: value.Capabilities.ParallelTools, Reasoning: value.Capabilities.Reasoning, Thinking: value.Capabilities.Thinking, Vision: value.Capabilities.Vision},
		ContextWindowTokens:    cloneInt64(value.ContextWindowTokens),
		MaxOutputTokens:        cloneInt64(value.MaxOutputTokens),
		CapabilitySource:       value.CapabilitySource,
		CapabilityOverrideJSON: append(json.RawMessage(nil), value.CapabilityOverrideJSON...),
		Version:                value.Version,
	}
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
