package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aggregationhub.local/core/internal/id"
	"aggregationhub.local/core/internal/provider"
)

const modelColumns = `id,provider_id,upstream_model_id,public_model_id,display_name,source,lifecycle_status,enabled,supports_streaming,supports_tools,supports_parallel_tools,supports_reasoning,supports_thinking,supports_vision,context_window_tokens,max_output_tokens,capability_source,capability_override_json,version,created_at,updated_at,deleted_at`

type ModelRepository struct{ database *sql.DB }

var _ provider.ModelRepository = (*ModelRepository)(nil)

func NewModelRepository(database *sql.DB) (*ModelRepository, error) {
	if database == nil {
		return nil, errors.New("模型仓储数据库无效")
	}
	return &ModelRepository{database: database}, nil
}

func (repository *ModelRepository) FindByPublicID(ctx context.Context, publicModelID string) (provider.ProviderModel, error) {
	if ctx == nil {
		return provider.ProviderModel{}, errors.New("读取模型的上下文不能为空")
	}
	if !validPublicModelID(publicModelID) {
		return provider.ProviderModel{}, provider.ErrModelNotFound
	}
	value, err := scanProviderModel(repository.database.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM provider_models WHERE public_model_id=? AND deleted_at IS NULL`, publicModelID))
	if errors.Is(err, sql.ErrNoRows) {
		return provider.ProviderModel{}, provider.ErrModelNotFound
	}
	if err != nil {
		return provider.ProviderModel{}, err
	}
	return value, nil
}

func (repository *ModelRepository) ReconcileSyncedModels(ctx context.Context, providerID string, providerSlug string, discovered []provider.SyncedModel, now time.Time) error {
	if ctx == nil {
		return errors.New("同步模型的上下文不能为空")
	}
	if strings.TrimSpace(providerID) == "" || !validProviderSlug(providerSlug) {
		return provider.ErrInvalidProvider
	}
	for _, model := range discovered {
		if err := validateSyncedModel(model); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(discovered))
	for _, model := range discovered {
		if _, exists := seen[model.UpstreamModelID]; exists {
			return provider.ErrInvalidModel
		}
		seen[model.UpstreamModelID] = struct{}{}
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始同步模型事务失败: %w", err)
	}
	defer transaction.Rollback()
	var providerCount int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM providers WHERE id=? AND deleted_at IS NULL`, providerID).Scan(&providerCount); err != nil {
		return fmt.Errorf("读取 Provider 同步状态失败: %w", err)
	}
	if providerCount != 1 {
		return provider.ErrProviderNotFound
	}

	existing, err := loadExistingModels(ctx, transaction, providerID)
	if err != nil {
		return err
	}
	now = now.UTC()
	for _, model := range discovered {
		if current, exists := existing[model.UpstreamModelID]; exists {
			if current.DeletedAt != nil {
				continue
			}
			if err := updateSyncedModel(ctx, transaction, current, model, now); err != nil {
				return err
			}
			delete(existing, model.UpstreamModelID)
			continue
		}
		if err := insertSyncedModel(ctx, transaction, providerID, providerSlug, model, now); err != nil {
			return err
		}
	}
	for _, current := range existing {
		if current.DeletedAt != nil || current.LifecycleStatus == provider.ModelStatusMissingUpstream {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE provider_models SET lifecycle_status='missing_upstream',version=version+1,updated_at=? WHERE id=? AND deleted_at IS NULL`, now.UnixMilli(), current.ID); err != nil {
			return fmt.Errorf("标记缺失上游模型失败: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交模型同步事务失败: %w", err)
	}
	return nil
}

type providerModelScanner interface{ Scan(...any) error }

func scanProviderModel(scanner providerModelScanner) (provider.ProviderModel, error) {
	var value provider.ProviderModel
	var source, status string
	var enabled, streaming, tools, parallelTools, reasoning, thinking, vision int
	var contextWindow, maxOutput, deletedAt sql.NullInt64
	var capabilityOverride string
	var createdAt, updatedAt int64
	if err := scanner.Scan(&value.ID, &value.ProviderID, &value.UpstreamModelID, &value.PublicModelID, &value.DisplayName, &source, &status, &enabled, &streaming, &tools, &parallelTools, &reasoning, &thinking, &vision, &contextWindow, &maxOutput, &value.CapabilitySource, &capabilityOverride, &value.Version, &createdAt, &updatedAt, &deletedAt); err != nil {
		return provider.ProviderModel{}, err
	}
	value.Source = provider.ModelSource(source)
	value.LifecycleStatus = provider.ModelStatus(status)
	value.Enabled = enabled == 1
	value.Capabilities = provider.Capabilities{Streaming: streaming == 1, Tools: tools == 1, ParallelTools: parallelTools == 1, Reasoning: reasoning == 1, Thinking: thinking == 1, Vision: vision == 1}
	value.ContextWindowTokens = positiveIntPointer(contextWindow)
	value.MaxOutputTokens = positiveIntPointer(maxOutput)
	value.CapabilityOverrideJSON = json.RawMessage(capabilityOverride)
	value.CreatedAt = time.UnixMilli(createdAt).UTC()
	value.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	value.DeletedAt = timePointer(deletedAt)
	if err := validateProviderModelRecord(value); err != nil {
		return provider.ProviderModel{}, fmt.Errorf("Provider 模型记录已损坏: %w", err)
	}
	return value, nil
}

func loadExistingModels(ctx context.Context, transaction *sql.Tx, providerID string) (map[string]provider.ProviderModel, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT `+modelColumns+` FROM provider_models WHERE provider_id=?`, providerID)
	if err != nil {
		return nil, fmt.Errorf("读取现有模型失败: %w", err)
	}
	defer rows.Close()
	values := make(map[string]provider.ProviderModel)
	for rows.Next() {
		value, err := scanProviderModel(rows)
		if err != nil {
			return nil, err
		}
		values[value.UpstreamModelID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历现有模型失败: %w", err)
	}
	return values, nil
}

func insertSyncedModel(ctx context.Context, transaction *sql.Tx, providerID string, providerSlug string, model provider.SyncedModel, now time.Time) error {
	modelID, err := id.RandomULID(now)
	if err != nil {
		return fmt.Errorf("生成同步模型标识失败: %w", err)
	}
	publicModelID := providerSlug + "/" + model.UpstreamModelID
	_, err = transaction.ExecContext(ctx, `INSERT INTO provider_models(id,provider_id,upstream_model_id,public_model_id,display_name,source,lifecycle_status,enabled,supports_streaming,supports_tools,supports_parallel_tools,supports_reasoning,supports_thinking,supports_vision,context_window_tokens,max_output_tokens,capability_source,capability_override_json,version,created_at,updated_at,deleted_at) VALUES(?,?,?,?,?,?,?,0,?,?,?,?,?,?,?,?,?,'{}',1,?,?,NULL)`, modelID, providerID, model.UpstreamModelID, publicModelID, model.DisplayName, model.Source, provider.ModelStatusAvailable, boolInteger(model.Capabilities.Streaming), boolInteger(model.Capabilities.Tools), boolInteger(model.Capabilities.ParallelTools), boolInteger(model.Capabilities.Reasoning), boolInteger(model.Capabilities.Thinking), boolInteger(model.Capabilities.Vision), nullableInt64(model.ContextWindowTokens), nullableInt64(model.MaxOutputTokens), model.CapabilitySource, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return fmt.Errorf("创建同步模型失败: %w", err)
	}
	return nil
}

func updateSyncedModel(ctx context.Context, transaction *sql.Tx, current provider.ProviderModel, model provider.SyncedModel, now time.Time) error {
	_, err := transaction.ExecContext(ctx, `UPDATE provider_models SET display_name=?,source=?,lifecycle_status='available',supports_streaming=?,supports_tools=?,supports_parallel_tools=?,supports_reasoning=?,supports_thinking=?,supports_vision=?,context_window_tokens=?,max_output_tokens=?,capability_source=?,version=version+1,updated_at=? WHERE id=? AND deleted_at IS NULL`, model.DisplayName, model.Source, boolInteger(model.Capabilities.Streaming), boolInteger(model.Capabilities.Tools), boolInteger(model.Capabilities.ParallelTools), boolInteger(model.Capabilities.Reasoning), boolInteger(model.Capabilities.Thinking), boolInteger(model.Capabilities.Vision), nullableInt64(model.ContextWindowTokens), nullableInt64(model.MaxOutputTokens), model.CapabilitySource, now.UnixMilli(), current.ID)
	if err != nil {
		return fmt.Errorf("更新同步模型失败: %w", err)
	}
	return nil
}

func validateSyncedModel(model provider.SyncedModel) error {
	if strings.TrimSpace(model.UpstreamModelID) == "" || len(model.UpstreamModelID) > 255 || strings.TrimSpace(model.DisplayName) == "" || len(model.DisplayName) > 255 || (model.Source != provider.ModelSourceUpstream && model.Source != provider.ModelSourceAdapterDefault && model.Source != provider.ModelSourceManual && model.Source != provider.ModelSourceOAuth) || strings.TrimSpace(model.CapabilitySource) == "" || invalidTokenPointer(model.ContextWindowTokens) || invalidTokenPointer(model.MaxOutputTokens) {
		return provider.ErrInvalidModel
	}
	return nil
}

func validateProviderModelRecord(value provider.ProviderModel) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.ProviderID) == "" || strings.TrimSpace(value.UpstreamModelID) == "" || !validPublicModelID(value.PublicModelID) || strings.TrimSpace(value.DisplayName) == "" || strings.TrimSpace(value.CapabilitySource) == "" || value.Version < 1 || !jsonObject(value.CapabilityOverrideJSON) || invalidTokenPointer(value.ContextWindowTokens) || invalidTokenPointer(value.MaxOutputTokens) {
		return provider.ErrInvalidModel
	}
	if value.Source != provider.ModelSourceUpstream && value.Source != provider.ModelSourceAdapterDefault && value.Source != provider.ModelSourceManual && value.Source != provider.ModelSourceOAuth {
		return provider.ErrInvalidModel
	}
	if value.LifecycleStatus != provider.ModelStatusAvailable && value.LifecycleStatus != provider.ModelStatusDegraded && value.LifecycleStatus != provider.ModelStatusMissingUpstream && value.LifecycleStatus != provider.ModelStatusDisabled && value.LifecycleStatus != provider.ModelStatusDeleted {
		return provider.ErrInvalidModel
	}
	return nil
}

func validProviderSlug(value string) bool {
	if len(value) < 1 || len(value) > 48 || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}
func validPublicModelID(value string) bool {
	separator := strings.IndexByte(value, '/')
	return separator > 0 && separator < len(value)-1 && len(value) <= 304
}
func invalidTokenPointer(value *int64) bool { return value != nil && *value <= 0 }
func positiveIntPointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
