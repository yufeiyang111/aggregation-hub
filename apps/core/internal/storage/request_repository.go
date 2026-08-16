package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

type RequestRepository struct {
	database *sql.DB
	usage    *UsageRepository
}

func NewRequestRepository(database *sql.DB) (*RequestRepository, error) {
	if database == nil {
		return nil, errors.New("请求观测仓储数据库无效")
	}
	usage, err := NewUsageRepository(database)
	if err != nil {
		return nil, err
	}
	return &RequestRepository{database: database, usage: usage}, nil
}

func (repository *RequestRepository) Create(ctx context.Context, record observability.RequestRecord) error {
	if ctx == nil {
		return errors.New("创建请求观测记录的上下文不能为空")
	}
	if err := observability.ValidateRequestRecord(record); err != nil {
		return err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始请求观测事务失败: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	_, err = transaction.ExecContext(ctx, `INSERT INTO requests(id,provider_slug_snapshot,public_model_snapshot,upstream_model_snapshot,source_protocol,endpoint,streaming,status,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, record.ID, record.ProviderSlugSnapshot, record.PublicModelSnapshot, record.UpstreamModelSnapshot, record.SourceProtocol, record.Endpoint, boolToInteger(record.Streaming), record.Status, record.CreatedAt.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("写入请求观测记录失败: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交请求观测事务失败: %w", err)
	}
	return nil
}

func (repository *RequestRepository) Transition(ctx context.Context, transition observability.RequestTransition) error {
	if ctx == nil {
		return observability.ErrInvalidRequestTransition
	}
	if err := observability.ValidateRequestTransitionData(transition); err != nil {
		return err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始请求状态事务失败: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	timestamp := transition.At.UTC().UnixMilli()
	var result sql.Result
	if transition.Status == observability.RequestStatusStreaming {
		result, err = transaction.ExecContext(ctx, `UPDATE requests SET status=?,started_stream_at=? WHERE id=? AND status=?`, transition.Status, timestamp, transition.ID, transition.From)
	} else {
		key, keyErr := requestDailyUsageKey(ctx, transaction, transition.ID, transition.From, transition.At)
		if keyErr != nil {
			return keyErr
		}
		usage := usageColumns(transition.Usage)
		result, err = transaction.ExecContext(ctx, `UPDATE requests SET status=?,http_status=?,error_code=?,retryable=?,usage_source=?,input_tokens=?,output_tokens=?,cached_input_tokens=?,cache_write_tokens=?,reasoning_tokens=?,duration_ms=CASE WHEN ? >= created_at THEN ? - created_at ELSE 0 END,completed_at=? WHERE id=? AND status=?`, transition.Status, nullableHTTPStatus(transition.HTTPStatus), nullableRequestString(transition.ErrorCode), boolToInteger(transition.Retryable), usage.source, usage.inputTokens, usage.outputTokens, usage.cachedInputTokens, usage.cacheWriteTokens, usage.reasoningTokens, timestamp, timestamp, timestamp, transition.ID, transition.From)
		if err == nil {
			affected, affectedErr := result.RowsAffected()
			if affectedErr != nil {
				return fmt.Errorf("读取请求状态结果失败: %w", affectedErr)
			}
			if affected != 1 {
				return observability.ErrInvalidRequestTransition
			}
			if err := repository.usage.upsertTransaction(ctx, transaction, key, transition.Status, transition.Usage, transition.At); err != nil {
				return err
			}
			if err := transaction.Commit(); err != nil {
				return fmt.Errorf("提交请求状态事务失败: %w", err)
			}
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("更新请求状态失败: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取请求状态结果失败: %w", err)
	}
	if affected != 1 {
		return observability.ErrInvalidRequestTransition
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交请求状态事务失败: %w", err)
	}
	return nil
}

func requestDailyUsageKey(ctx context.Context, transaction *sql.Tx, requestID string, expectedStatus observability.RequestStatus, completedAt time.Time) (observability.DailyUsageKey, error) {
	var providerSlug, publicModel string
	if err := transaction.QueryRowContext(ctx, `SELECT provider_slug_snapshot,public_model_snapshot FROM requests WHERE id=? AND status=?`, requestID, expectedStatus).Scan(&providerSlug, &publicModel); errors.Is(err, sql.ErrNoRows) {
		return observability.DailyUsageKey{}, observability.ErrInvalidRequestTransition
	} else if err != nil {
		return observability.DailyUsageKey{}, fmt.Errorf("读取请求汇总快照失败: %w", err)
	}
	return observability.DailyUsageKey{DateUTC: completedAt.UTC().Format("2006-01-02"), ProviderSlugSnapshot: providerSlug, PublicModelSnapshot: publicModel}, nil
}

type storedUsage struct {
	source            any
	inputTokens       any
	outputTokens      any
	cachedInputTokens any
	cacheWriteTokens  any
	reasoningTokens   any
}

func usageColumns(usage *normalize.Usage) storedUsage {
	if usage == nil {
		return storedUsage{}
	}
	return storedUsage{source: string(usage.Source), inputTokens: nullableRequestInt64(usage.InputTokens), outputTokens: nullableRequestInt64(usage.OutputTokens), cachedInputTokens: nullableRequestInt64(usage.CachedInputTokens), cacheWriteTokens: nullableRequestInt64(usage.CacheWriteTokens), reasoningTokens: nullableRequestInt64(usage.ReasoningTokens)}
}

func nullableRequestInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
func nullableHTTPStatus(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}
func nullableRequestString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func boolToInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

var _ observability.RequestStore = (*RequestRepository)(nil)

// List 返回固定 created_at DESC、id DESC 顺序的脱敏请求元数据。
func (repository *RequestRepository) List(ctx context.Context, query observability.RequestListQuery) (observability.RequestPage, error) {
	if ctx == nil || observability.ValidateRequestListQuery(query) != nil {
		return observability.RequestPage{}, observability.ErrInvalidRequestQuery
	}
	where, arguments, err := requestQueryWhere(query)
	if err != nil {
		return observability.RequestPage{}, err
	}
	arguments = append(arguments, query.PageSize+1)
	statement := `SELECT id,created_at,completed_at,source_protocol,provider_slug_snapshot,public_model_snapshot,streaming,status,http_status,error_code,retryable,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,duration_ms FROM requests` + where + ` ORDER BY created_at DESC,id DESC LIMIT ?`
	rows, err := repository.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return observability.RequestPage{}, fmt.Errorf("读取请求记录失败: %w", err)
	}
	defer rows.Close()

	items := make([]observability.RequestMetadata, 0, query.PageSize)
	for rows.Next() {
		item, err := scanRequestMetadata(rows)
		if err != nil {
			return observability.RequestPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return observability.RequestPage{}, fmt.Errorf("遍历请求记录失败: %w", err)
	}
	page := observability.RequestPage{Data: items}
	if len(items) > query.PageSize {
		last := items[query.PageSize-1]
		cursor := observability.EncodeRequestCursor(last.CreatedAt, last.ID)
		page.Data = items[:query.PageSize]
		page.NextCursor = &cursor
	}
	return page, nil
}

// Get 返回单条脱敏请求元数据，不存在时不泄漏数据库错误。
func (repository *RequestRepository) Get(ctx context.Context, id string) (observability.RequestMetadata, error) {
	if ctx == nil || id == "" || len(id) > 64 {
		return observability.RequestMetadata{}, observability.ErrRequestNotFound
	}
	row := repository.database.QueryRowContext(ctx, `SELECT id,created_at,completed_at,source_protocol,provider_slug_snapshot,public_model_snapshot,streaming,status,http_status,error_code,retryable,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,duration_ms FROM requests WHERE id=?`, id)
	item, err := scanRequestMetadata(row)
	if errors.Is(err, sql.ErrNoRows) {
		return observability.RequestMetadata{}, observability.ErrRequestNotFound
	}
	return item, err
}

type requestMetadataScanner interface {
	Scan(dest ...any) error
}

func scanRequestMetadata(scanner requestMetadataScanner) (observability.RequestMetadata, error) {
	var item observability.RequestMetadata
	var createdAt int64
	var completedAt sql.NullInt64
	var streaming, retryable int
	var status string
	var source string
	var httpStatus sql.NullInt64
	var errorCode sql.NullString
	var inputTokens, outputTokens, cachedInputTokens, cacheWriteTokens, reasoningTokens, durationMS sql.NullInt64
	if err := scanner.Scan(&item.ID, &createdAt, &completedAt, &source, &item.ProviderSlug, &item.PublicModelID, &streaming, &status, &httpStatus, &errorCode, &retryable, &inputTokens, &outputTokens, &cachedInputTokens, &cacheWriteTokens, &reasoningTokens, &durationMS); err != nil {
		return observability.RequestMetadata{}, err
	}
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.SourceProtocol = observability.SourceProtocol(source)
	item.Status = observability.RequestStatus(status)
	item.Streaming = streaming != 0
	item.Retryable = retryable != 0
	item.CompletedAt = nullableRequestTime(completedAt)
	item.HTTPStatus = nullableRequestInt(httpStatus)
	item.ErrorCode = nullableRequestStringPointer(errorCode)
	item.InputTokens = nullableRequestMetadataInt64(inputTokens)
	item.OutputTokens = nullableRequestMetadataInt64(outputTokens)
	item.CachedInputTokens = nullableRequestMetadataInt64(cachedInputTokens)
	item.CacheWriteTokens = nullableRequestMetadataInt64(cacheWriteTokens)
	item.ReasoningTokens = nullableRequestMetadataInt64(reasoningTokens)
	item.DurationMS = nullableRequestMetadataInt64(durationMS)
	return item, nil
}

func requestQueryWhere(query observability.RequestListQuery) (string, []any, error) {
	conditions := make([]string, 0, 8)
	arguments := make([]any, 0, 10)
	if query.Status != "" {
		conditions, arguments = append(conditions, "status=?"), append(arguments, query.Status)
	}
	if query.ProviderSlug != "" {
		conditions, arguments = append(conditions, "provider_slug_snapshot=?"), append(arguments, query.ProviderSlug)
	}
	if query.PublicModelID != "" {
		conditions, arguments = append(conditions, "public_model_snapshot=?"), append(arguments, query.PublicModelID)
	}
	if query.SourceProtocol != "" {
		conditions, arguments = append(conditions, "source_protocol=?"), append(arguments, query.SourceProtocol)
	}
	if query.FromUTC != nil {
		conditions, arguments = append(conditions, "created_at>=?"), append(arguments, query.FromUTC.UTC().UnixMilli())
	}
	if query.ToUTC != nil {
		conditions, arguments = append(conditions, "created_at<=?"), append(arguments, query.ToUTC.UTC().UnixMilli())
	}
	if query.Cursor != "" {
		createdAt, id, err := observability.DecodeRequestCursor(query.Cursor)
		if err != nil {
			return "", nil, observability.ErrInvalidRequestQuery
		}
		conditions, arguments = append(conditions, "(created_at<? OR (created_at=? AND id<?))"), append(arguments, createdAt.UnixMilli(), createdAt.UnixMilli(), id)
	}
	if len(conditions) == 0 {
		return "", arguments, nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), arguments, nil
}

func nullableRequestTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64).UTC()
	return &result
}
func nullableRequestInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}
func nullableRequestStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
func nullableRequestMetadataInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
