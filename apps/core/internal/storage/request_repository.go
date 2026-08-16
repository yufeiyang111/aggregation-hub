package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
