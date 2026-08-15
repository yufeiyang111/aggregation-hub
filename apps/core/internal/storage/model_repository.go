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

func (repository *ModelRepository) FindByID(ctx context.Context, modelID string) (provider.ProviderModel, error) {
	if ctx == nil {
		return provider.ProviderModel{}, errors.New("读取模型的上下文不能为空")
	}
	if !validModelID(modelID) {
		return provider.ProviderModel{}, provider.ErrModelNotFound
	}
	value, err := scanProviderModel(repository.database.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM provider_models WHERE id=? AND deleted_at IS NULL`, modelID))
	if errors.Is(err, sql.ErrNoRows) {
		return provider.ProviderModel{}, provider.ErrModelNotFound
	}
	if err != nil {
		return provider.ProviderModel{}, err
	}
	return value, nil
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

func (repository *ModelRepository) List(ctx context.Context, query provider.ModelPageQuery) (provider.ModelPage, error) {
	if ctx == nil {
		return provider.ModelPage{}, errors.New("列出模型的上下文不能为空")
	}
	pageSize, err := normalizedPageSize(query.PageSize)
	if err != nil {
		return provider.ModelPage{}, err
	}
	if err := validateModelPageQuery(query); err != nil {
		return provider.ModelPage{}, err
	}

	conditions := []string{"deleted_at IS NULL", "id>?"}
	arguments := []any{query.Cursor}
	if query.ProviderID != "" {
		conditions = append(conditions, "provider_id=?")
		arguments = append(arguments, query.ProviderID)
	}
	if query.LifecycleStatus != "" {
		conditions = append(conditions, "lifecycle_status=?")
		arguments = append(arguments, query.LifecycleStatus)
	}
	if query.Enabled != nil {
		conditions = append(conditions, "enabled=?")
		arguments = append(arguments, boolInteger(*query.Enabled))
	}
	if query.Capability != "" {
		conditions = append(conditions, modelCapabilityColumn(query.Capability)+"=1")
	}
	if query.Search != "" {
		conditions = append(conditions, "(public_model_id LIKE ? ESCAPE '\\' OR display_name LIKE ? ESCAPE '\\')")
		pattern := "%" + escapeLike(query.Search) + "%"
		arguments = append(arguments, pattern, pattern)
	}
	arguments = append(arguments, pageSize+1)
	rows, err := repository.database.QueryContext(ctx, "SELECT "+modelColumns+" FROM provider_models WHERE "+strings.Join(conditions, " AND ")+" ORDER BY id ASC LIMIT ?", arguments...)
	if err != nil {
		return provider.ModelPage{}, fmt.Errorf("列出模型失败: %w", err)
	}
	defer rows.Close()
	page := provider.ModelPage{Items: make([]provider.ProviderModel, 0, pageSize)}
	for rows.Next() {
		value, err := scanProviderModel(rows)
		if err != nil {
			return provider.ModelPage{}, err
		}
		page.Items = append(page.Items, value)
	}
	if err := rows.Err(); err != nil {
		return provider.ModelPage{}, fmt.Errorf("遍历模型列表失败: %w", err)
	}
	if len(page.Items) > pageSize {
		page.NextCursor = page.Items[pageSize-1].ID
		page.Items = page.Items[:pageSize]
	}
	return page, nil
}

func (repository *ModelRepository) SetEnabled(ctx context.Context, modelID string, expectedVersion int64, enabled bool, audit provider.AuditEvent) (provider.ProviderModel, error) {
	if ctx == nil {
		return provider.ProviderModel{}, errors.New("设置模型启用状态的上下文不能为空")
	}
	if !validModelID(modelID) || expectedVersion < 1 {
		return provider.ProviderModel{}, provider.ErrStaleResource
	}
	if err := validateAuditEvent(audit); err != nil {
		return provider.ProviderModel{}, err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return provider.ProviderModel{}, fmt.Errorf("开始设置模型状态事务失败: %w", err)
	}
	defer transaction.Rollback()
	current, err := scanProviderModel(transaction.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM provider_models WHERE id=? AND deleted_at IS NULL`, modelID))
	if errors.Is(err, sql.ErrNoRows) {
		return provider.ProviderModel{}, provider.ErrModelNotFound
	}
	if err != nil {
		return provider.ProviderModel{}, err
	}
	if current.Version != expectedVersion {
		return provider.ProviderModel{}, provider.ErrStaleResource
	}
	if enabled && current.LifecycleStatus != provider.ModelStatusAvailable && current.LifecycleStatus != provider.ModelStatusDegraded {
		return provider.ProviderModel{}, provider.ErrInvalidModel
	}
	result, err := transaction.ExecContext(ctx, `UPDATE provider_models SET enabled=?,version=version+1,updated_at=? WHERE id=? AND version=? AND deleted_at IS NULL`, boolInteger(enabled), audit.CreatedAt.UTC().UnixMilli(), modelID, expectedVersion)
	if err != nil {
		return provider.ProviderModel{}, fmt.Errorf("设置模型启用状态失败: %w", err)
	}
	if err := requireUpdatedModel(ctx, transaction, result, modelID); err != nil {
		return provider.ProviderModel{}, err
	}
	if err := insertAuditEvent(ctx, transaction, audit); err != nil {
		return provider.ProviderModel{}, err
	}
	if err := transaction.Commit(); err != nil {
		return provider.ProviderModel{}, fmt.Errorf("提交模型状态事务失败: %w", err)
	}
	return repository.FindByID(ctx, modelID)
}

func validateModelPageQuery(query provider.ModelPageQuery) error {
	if len(query.Cursor) > 64 || (query.Cursor != "" && !validModelID(query.Cursor)) || len(query.ProviderID) > 64 || (query.ProviderID != "" && strings.TrimSpace(query.ProviderID) != query.ProviderID) || len(query.Search) > 128 || strings.TrimSpace(query.Search) != query.Search {
		return provider.ErrInvalidModel
	}
	if query.LifecycleStatus != "" && query.LifecycleStatus != provider.ModelStatusAvailable && query.LifecycleStatus != provider.ModelStatusDegraded && query.LifecycleStatus != provider.ModelStatusMissingUpstream && query.LifecycleStatus != provider.ModelStatusDisabled {
		return provider.ErrInvalidModel
	}
	if query.Capability != "" && modelCapabilityColumn(query.Capability) == "" {
		return provider.ErrInvalidModel
	}
	return nil
}

func modelCapabilityColumn(capability string) string {
	switch capability {
	case "streaming":
		return "supports_streaming"
	case "tools":
		return "supports_tools"
	case "parallel_tools":
		return "supports_parallel_tools"
	case "reasoning":
		return "supports_reasoning"
	case "thinking":
		return "supports_thinking"
	case "vision":
		return "supports_vision"
	default:
		return ""
	}
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}

func validModelID(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 64
}

func requireUpdatedModel(ctx context.Context, transaction *sql.Tx, result sql.Result, modelID string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取模型更新结果失败: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var exists int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_models WHERE id=? AND deleted_at IS NULL`, modelID).Scan(&exists); err != nil {
		return fmt.Errorf("确认模型更新冲突失败: %w", err)
	}
	if exists == 0 {
		return provider.ErrModelNotFound
	}
	return provider.ErrStaleResource
}

func (repository *ModelRepository) ListPublic(ctx context.Context) ([]provider.PublicModel, error) {
	if ctx == nil {
		return nil, errors.New("列出公开模型的上下文不能为空")
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT m.public_model_id,p.slug FROM provider_models m JOIN providers p ON p.id=m.provider_id WHERE m.deleted_at IS NULL AND m.enabled=1 AND m.lifecycle_status IN ('available','degraded') AND p.deleted_at IS NULL AND p.enabled=1 AND p.lifecycle_status IN ('enabled','degraded') ORDER BY m.public_model_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("列出公开模型失败: %w", err)
	}
	defer rows.Close()
	result := make([]provider.PublicModel, 0)
	for rows.Next() {
		var value provider.PublicModel
		if err := rows.Scan(&value.ID, &value.Owner); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
