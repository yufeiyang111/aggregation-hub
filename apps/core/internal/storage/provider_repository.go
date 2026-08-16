package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/provider"
)

const providerColumns = `id,slug,name,adapter_type,auth_type,base_url,credential_ref,lifecycle_status,enabled,timeout_ms,adapter_config_json,version,created_at,updated_at,deleted_at`

type ProviderRepository struct{ database *sql.DB }

var _ provider.ProviderRepository = (*ProviderRepository)(nil)

func NewProviderRepository(database *sql.DB) (*ProviderRepository, error) {
	if database == nil {
		return nil, errors.New("Provider 仓储数据库无效")
	}
	return &ProviderRepository{database: database}, nil
}

func (repository *ProviderRepository) Create(ctx context.Context, value provider.Provider, audit provider.AuditEvent) error {
	if ctx == nil {
		return errors.New("创建 Provider 的上下文不能为空")
	}
	if err := validateProviderRecord(value); err != nil {
		return err
	}
	if err := validateAuditEvent(audit); err != nil {
		return err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始创建 Provider 事务失败: %w", err)
	}
	defer transaction.Rollback()
	if err := insertProvider(ctx, transaction, value); err != nil {
		return err
	}
	if err := insertAuditEvent(ctx, transaction, audit); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交创建 Provider 事务失败: %w", err)
	}
	return nil
}

func (repository *ProviderRepository) FindByID(ctx context.Context, id string) (provider.Provider, error) {
	if ctx == nil {
		return provider.Provider{}, errors.New("读取 Provider 的上下文不能为空")
	}
	if strings.TrimSpace(id) == "" {
		return provider.Provider{}, provider.ErrProviderNotFound
	}
	return findProvider(ctx, repository.database, `SELECT `+providerColumns+` FROM providers WHERE id=? AND deleted_at IS NULL`, id)
}

func (repository *ProviderRepository) FindBySlug(ctx context.Context, slug string) (provider.Provider, error) {
	if ctx == nil {
		return provider.Provider{}, errors.New("读取 Provider 的上下文不能为空")
	}
	if strings.TrimSpace(slug) == "" {
		return provider.Provider{}, provider.ErrProviderNotFound
	}
	return findProvider(ctx, repository.database, `SELECT `+providerColumns+` FROM providers WHERE slug=? AND deleted_at IS NULL`, slug)
}

func (repository *ProviderRepository) Update(ctx context.Context, value provider.Provider, expectedVersion int64, audit provider.AuditEvent) (provider.Provider, error) {
	if ctx == nil {
		return provider.Provider{}, errors.New("更新 Provider 的上下文不能为空")
	}
	if expectedVersion < 1 {
		return provider.Provider{}, provider.ErrStaleResource
	}
	if err := validateProviderRecord(value); err != nil {
		return provider.Provider{}, err
	}
	if err := validateAuditEvent(audit); err != nil {
		return provider.Provider{}, err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return provider.Provider{}, fmt.Errorf("开始更新 Provider 事务失败: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `UPDATE providers SET name=?,base_url=?,credential_ref=?,lifecycle_status=?,enabled=?,timeout_ms=?,adapter_config_json=?,version=version+1,updated_at=? WHERE id=? AND version=? AND deleted_at IS NULL`, value.Name, value.BaseURL, nullableCredentialRef(value.CredentialRef), value.LifecycleStatus, boolInteger(value.Enabled), value.Timeout.Milliseconds(), string(value.AdapterConfigJSON), value.UpdatedAt.UTC().UnixMilli(), value.ID, expectedVersion)
	if err != nil {
		return provider.Provider{}, classifyProviderWriteError("更新 Provider 失败", err)
	}
	if err := requireUpdatedProvider(ctx, transaction, result, value.ID); err != nil {
		return provider.Provider{}, err
	}
	if err := insertAuditEvent(ctx, transaction, audit); err != nil {
		return provider.Provider{}, err
	}
	if err := transaction.Commit(); err != nil {
		return provider.Provider{}, fmt.Errorf("提交更新 Provider 事务失败: %w", err)
	}
	return repository.FindByID(ctx, value.ID)
}

func (repository *ProviderRepository) SetEnabled(ctx context.Context, id string, expectedVersion int64, enabled bool, audit provider.AuditEvent) (provider.Provider, error) {
	if ctx == nil {
		return provider.Provider{}, errors.New("设置 Provider 启用状态的上下文不能为空")
	}
	if strings.TrimSpace(id) == "" || expectedVersion < 1 {
		return provider.Provider{}, provider.ErrStaleResource
	}
	if err := validateAuditEvent(audit); err != nil {
		return provider.Provider{}, err
	}
	status := provider.ProviderStatusDisabled
	if enabled {
		status = provider.ProviderStatusEnabled
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return provider.Provider{}, fmt.Errorf("开始设置 Provider 状态事务失败: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `UPDATE providers SET lifecycle_status=?,enabled=?,version=version+1,updated_at=? WHERE id=? AND version=? AND deleted_at IS NULL`, status, boolInteger(enabled), audit.CreatedAt.UTC().UnixMilli(), id, expectedVersion)
	if err != nil {
		return provider.Provider{}, fmt.Errorf("设置 Provider 启用状态失败: %w", err)
	}
	if err := requireUpdatedProvider(ctx, transaction, result, id); err != nil {
		return provider.Provider{}, err
	}
	if err := insertAuditEvent(ctx, transaction, audit); err != nil {
		return provider.Provider{}, err
	}
	if err := transaction.Commit(); err != nil {
		return provider.Provider{}, fmt.Errorf("提交 Provider 状态事务失败: %w", err)
	}
	return repository.FindByID(ctx, id)
}

func (repository *ProviderRepository) SoftDelete(ctx context.Context, id string, expectedVersion int64, audit provider.AuditEvent) error {
	if ctx == nil {
		return errors.New("删除 Provider 的上下文不能为空")
	}
	if strings.TrimSpace(id) == "" || expectedVersion < 1 {
		return provider.ErrStaleResource
	}
	if err := validateAuditEvent(audit); err != nil {
		return err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始删除 Provider 事务失败: %w", err)
	}
	defer transaction.Rollback()
	now := audit.CreatedAt.UTC().UnixMilli()
	result, err := transaction.ExecContext(ctx, `UPDATE providers SET lifecycle_status='deleted',enabled=0,version=version+1,updated_at=?,deleted_at=? WHERE id=? AND version=? AND deleted_at IS NULL`, now, now, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("软删除 Provider 失败: %w", err)
	}
	if err := requireUpdatedProvider(ctx, transaction, result, id); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE provider_models SET lifecycle_status='deleted',enabled=0,version=version+1,updated_at=?,deleted_at=? WHERE provider_id=? AND deleted_at IS NULL`, now, now, id); err != nil {
		return fmt.Errorf("软删除 Provider 模型失败: %w", err)
	}
	if err := insertAuditEvent(ctx, transaction, audit); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交删除 Provider 事务失败: %w", err)
	}
	return nil
}

func (repository *ProviderRepository) AppendAudit(ctx context.Context, audit provider.AuditEvent) error {
	if ctx == nil {
		return errors.New("写入审计的上下文不能为空")
	}
	if err := validateAuditEvent(audit); err != nil {
		return err
	}
	_, err := repository.database.ExecContext(ctx, `INSERT INTO audit_events(id,event_type,entity_type,entity_id,detail_json,created_at) VALUES(?,?,?,?,?,?)`, audit.ID, audit.EventType, audit.EntityType, nullableString(audit.EntityID), string(audit.DetailJSON), audit.CreatedAt.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("写入审计事件失败: %w", err)
	}
	return nil
}

func (repository *ProviderRepository) List(ctx context.Context, query provider.ProviderPageQuery) (provider.ProviderPage, error) {
	if ctx == nil {
		return provider.ProviderPage{}, errors.New("列出 Provider 的上下文不能为空")
	}
	pageSize, err := normalizedPageSize(query.PageSize)
	if err != nil {
		return provider.ProviderPage{}, err
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT `+providerColumns+` FROM providers WHERE deleted_at IS NULL AND id>? ORDER BY id ASC LIMIT ?`, query.Cursor, pageSize+1)
	if err != nil {
		return provider.ProviderPage{}, fmt.Errorf("列出 Provider 失败: %w", err)
	}
	defer rows.Close()
	page := provider.ProviderPage{Items: make([]provider.Provider, 0, pageSize)}
	for rows.Next() {
		value, err := scanProvider(rows)
		if err != nil {
			return provider.ProviderPage{}, err
		}
		page.Items = append(page.Items, value)
	}
	if err := rows.Err(); err != nil {
		return provider.ProviderPage{}, fmt.Errorf("遍历 Provider 列表失败: %w", err)
	}
	if len(page.Items) > pageSize {
		page.NextCursor = page.Items[pageSize-1].ID
		page.Items = page.Items[:pageSize]
	}
	return page, nil
}

type providerScanner interface{ Scan(...any) error }
type providerQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func findProvider(ctx context.Context, queryer providerQueryer, query string, argument string) (provider.Provider, error) {
	value, err := scanProvider(queryer.QueryRowContext(ctx, query, argument))
	if errors.Is(err, sql.ErrNoRows) {
		return provider.Provider{}, provider.ErrProviderNotFound
	}
	if err != nil {
		return provider.Provider{}, err
	}
	return value, nil
}

func scanProvider(scanner providerScanner) (provider.Provider, error) {
	var value provider.Provider
	var authType, status string
	var credentialRef sql.NullString
	var enabled int
	var timeoutMilliseconds, createdAt, updatedAt int64
	var adapterConfig string
	var deletedAt sql.NullInt64
	if err := scanner.Scan(&value.ID, &value.Slug, &value.Name, &value.AdapterType, &authType, &value.BaseURL, &credentialRef, &status, &enabled, &timeoutMilliseconds, &adapterConfig, &value.Version, &createdAt, &updatedAt, &deletedAt); err != nil {
		return provider.Provider{}, err
	}
	value.AuthType = provider.AuthType(authType)
	value.LifecycleStatus = provider.ProviderStatus(status)
	value.Enabled = enabled == 1
	value.Timeout = time.Duration(timeoutMilliseconds) * time.Millisecond
	value.AdapterConfigJSON = json.RawMessage(adapterConfig)
	value.CreatedAt = time.UnixMilli(createdAt).UTC()
	value.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	value.DeletedAt = timePointer(deletedAt)
	if credentialRef.Valid {
		ref := credential.Ref(credentialRef.String)
		if err := ref.Validate(); err != nil {
			return provider.Provider{}, fmt.Errorf("Provider 凭据引用已损坏: %w", err)
		}
		value.CredentialRef = &ref
	}
	if err := validateProviderRecord(value); err != nil {
		return provider.Provider{}, fmt.Errorf("Provider 记录已损坏: %w", err)
	}
	return value, nil
}

func insertProvider(ctx context.Context, transaction *sql.Tx, value provider.Provider) error {
	_, err := transaction.ExecContext(ctx, `INSERT INTO providers(id,slug,name,adapter_type,auth_type,base_url,credential_ref,lifecycle_status,enabled,timeout_ms,adapter_config_json,version,created_at,updated_at,deleted_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.Slug, value.Name, value.AdapterType, value.AuthType, value.BaseURL, nullableCredentialRef(value.CredentialRef), value.LifecycleStatus, boolInteger(value.Enabled), value.Timeout.Milliseconds(), string(value.AdapterConfigJSON), value.Version, value.CreatedAt.UTC().UnixMilli(), value.UpdatedAt.UTC().UnixMilli(), nullableTime(value.DeletedAt))
	if err != nil {
		return classifyProviderWriteError("创建 Provider 失败", err)
	}
	return nil
}

func insertAuditEvent(ctx context.Context, transaction *sql.Tx, audit provider.AuditEvent) error {
	_, err := transaction.ExecContext(ctx, `INSERT INTO audit_events(id,event_type,entity_type,entity_id,detail_json,created_at) VALUES(?,?,?,?,?,?)`, audit.ID, audit.EventType, audit.EntityType, nullableString(audit.EntityID), string(audit.DetailJSON), audit.CreatedAt.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("写入 Provider 审计事件失败: %w", err)
	}
	return nil
}

func requireUpdatedProvider(ctx context.Context, transaction *sql.Tx, result sql.Result, id string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Provider 更新结果失败: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var exists int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
		return fmt.Errorf("确认 Provider 更新冲突失败: %w", err)
	}
	if exists == 0 {
		return provider.ErrProviderNotFound
	}
	return provider.ErrStaleResource
}

func validateProviderRecord(value provider.Provider) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Slug) == "" || strings.TrimSpace(value.Name) == "" || strings.TrimSpace(value.AdapterType) == "" || strings.TrimSpace(value.BaseURL) == "" || value.Timeout < time.Second || value.Timeout > time.Hour || value.Version < 1 || !jsonObject(value.AdapterConfigJSON) {
		return provider.ErrInvalidProvider
	}
	if value.AuthType != provider.AuthTypeNone && value.AuthType != provider.AuthTypeAPIKey && value.AuthType != provider.AuthTypeBearerToken && value.AuthType != provider.AuthTypeOAuth {
		return provider.ErrInvalidProvider
	}
	if value.LifecycleStatus != provider.ProviderStatusDraft && value.LifecycleStatus != provider.ProviderStatusEnabled && value.LifecycleStatus != provider.ProviderStatusDegraded && value.LifecycleStatus != provider.ProviderStatusAuthRequired && value.LifecycleStatus != provider.ProviderStatusDisabled && value.LifecycleStatus != provider.ProviderStatusDeleted {
		return provider.ErrInvalidProvider
	}
	if value.AuthType == provider.AuthTypeNone && value.CredentialRef != nil {
		return provider.ErrInvalidProvider
	}
	if value.AuthType != provider.AuthTypeNone && (value.CredentialRef == nil || value.CredentialRef.Validate() != nil) {
		return provider.ErrInvalidProvider
	}
	return nil
}

func validateAuditEvent(event provider.AuditEvent) error {
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.EventType) == "" || strings.TrimSpace(event.EntityType) == "" || event.CreatedAt.IsZero() || !jsonObject(event.DetailJSON) {
		return errors.New("审计事件无效")
	}
	return nil
}

func jsonObject(value json.RawMessage) bool {
	if len(value) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil
}
func nullableCredentialRef(value *credential.Ref) any {
	if value == nil {
		return nil
	}
	return string(*value)
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
func normalizedPageSize(value int) (int, error) {
	if value == 0 {
		return 50, nil
	}
	if value < 1 || value > 200 {
		return 0, provider.ErrUnsupportedPagination
	}
	return value, nil
}
func classifyProviderWriteError(operation string, err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: providers.slug") {
		return fmt.Errorf("%s: %w", operation, provider.ErrDuplicateProvider)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// SetHealthStatus 仅由受控健康检查更新运行状态，不改变用户的 enabled 意图。
func (repository *ProviderRepository) SetHealthStatus(ctx context.Context, id string, status provider.ProviderStatus, updatedAt time.Time) error {
	if ctx == nil || strings.TrimSpace(id) == "" || updatedAt.IsZero() || (status != provider.ProviderStatusEnabled && status != provider.ProviderStatusDegraded && status != provider.ProviderStatusAuthRequired) {
		return provider.ErrInvalidProvider
	}
	_, err := repository.database.ExecContext(ctx, `UPDATE providers SET lifecycle_status=?,version=version+1,updated_at=? WHERE id=? AND enabled=1 AND deleted_at IS NULL`, status, updatedAt.UTC().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("更新 Provider 健康状态失败: %w", err)
	}
	return nil
}
