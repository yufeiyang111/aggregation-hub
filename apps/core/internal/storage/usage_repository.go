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

var ErrUsageDailyNotFound = errors.New("日用量汇总不存在")

type UsageRepository struct{ database *sql.DB }

func NewUsageRepository(database *sql.DB) (*UsageRepository, error) {
	if database == nil {
		return nil, errors.New("日用量仓储数据库无效")
	}
	return &UsageRepository{database: database}, nil
}

func (repository *UsageRepository) Find(ctx context.Context, key observability.DailyUsageKey) (observability.DailyUsage, error) {
	if ctx == nil || key.DateUTC == "" || key.ProviderSlugSnapshot == "" || key.PublicModelSnapshot == "" {
		return observability.DailyUsage{}, ErrUsageDailyNotFound
	}
	var result observability.DailyUsage
	var updatedAt int64
	err := repository.database.QueryRowContext(ctx, `SELECT request_count,succeeded_count,failed_count,cancelled_count,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,input_token_reported_count,output_token_reported_count,cached_input_token_reported_count,reasoning_token_reported_count,cache_eligible_input_tokens,cache_eligible_cached_input_tokens,updated_at FROM usage_daily WHERE date_utc=? AND provider_slug_snapshot=? AND public_model_snapshot=?`, key.DateUTC, key.ProviderSlugSnapshot, key.PublicModelSnapshot).Scan(&result.RequestCount, &result.SucceededCount, &result.FailedCount, &result.CancelledCount, &result.InputTokens, &result.OutputTokens, &result.CachedInputTokens, &result.CacheWriteTokens, &result.ReasoningTokens, &result.InputTokenReportedCount, &result.OutputTokenReportedCount, &result.CachedInputTokenReportedCount, &result.ReasoningTokenReportedCount, &result.CacheEligibleInputTokens, &result.CacheEligibleCachedInputTokens, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return observability.DailyUsage{}, ErrUsageDailyNotFound
	}
	if err != nil {
		return observability.DailyUsage{}, fmt.Errorf("读取日用量汇总失败: %w", err)
	}
	result.DailyUsageKey = key
	result.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return result, nil
}

func (repository *UsageRepository) upsertTransaction(ctx context.Context, transaction *sql.Tx, key observability.DailyUsageKey, status observability.RequestStatus, usage *normalize.Usage, updatedAt time.Time) error {
	if transaction == nil || key.DateUTC == "" || key.ProviderSlugSnapshot == "" || key.PublicModelSnapshot == "" {
		return errors.New("日用量汇总输入无效")
	}
	delta := usageDailyDeltaFor(status, usage)
	_, err := transaction.ExecContext(ctx, `INSERT INTO usage_daily(date_utc,provider_slug_snapshot,public_model_snapshot,request_count,succeeded_count,failed_count,cancelled_count,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,input_token_reported_count,output_token_reported_count,cached_input_token_reported_count,reasoning_token_reported_count,cache_eligible_input_tokens,cache_eligible_cached_input_tokens,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(date_utc,provider_slug_snapshot,public_model_snapshot) DO UPDATE SET request_count=usage_daily.request_count+excluded.request_count,succeeded_count=usage_daily.succeeded_count+excluded.succeeded_count,failed_count=usage_daily.failed_count+excluded.failed_count,cancelled_count=usage_daily.cancelled_count+excluded.cancelled_count,input_tokens=usage_daily.input_tokens+excluded.input_tokens,output_tokens=usage_daily.output_tokens+excluded.output_tokens,cached_input_tokens=usage_daily.cached_input_tokens+excluded.cached_input_tokens,cache_write_tokens=usage_daily.cache_write_tokens+excluded.cache_write_tokens,reasoning_tokens=usage_daily.reasoning_tokens+excluded.reasoning_tokens,input_token_reported_count=usage_daily.input_token_reported_count+excluded.input_token_reported_count,output_token_reported_count=usage_daily.output_token_reported_count+excluded.output_token_reported_count,cached_input_token_reported_count=usage_daily.cached_input_token_reported_count+excluded.cached_input_token_reported_count,reasoning_token_reported_count=usage_daily.reasoning_token_reported_count+excluded.reasoning_token_reported_count,cache_eligible_input_tokens=usage_daily.cache_eligible_input_tokens+excluded.cache_eligible_input_tokens,cache_eligible_cached_input_tokens=usage_daily.cache_eligible_cached_input_tokens+excluded.cache_eligible_cached_input_tokens,updated_at=excluded.updated_at`, key.DateUTC, key.ProviderSlugSnapshot, key.PublicModelSnapshot, delta.requestCount, delta.succeededCount, delta.failedCount, delta.cancelledCount, delta.inputTokens, delta.outputTokens, delta.cachedInputTokens, delta.cacheWriteTokens, delta.reasoningTokens, delta.inputTokenReportedCount, delta.outputTokenReportedCount, delta.cachedInputTokenReportedCount, delta.reasoningTokenReportedCount, delta.cacheEligibleInputTokens, delta.cacheEligibleCachedInputTokens, updatedAt.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("写入日用量汇总失败: %w", err)
	}
	return nil
}

type usageDailyDelta struct {
	requestCount                   int64
	succeededCount                 int64
	failedCount                    int64
	cancelledCount                 int64
	inputTokens                    int64
	outputTokens                   int64
	cachedInputTokens              int64
	cacheWriteTokens               int64
	reasoningTokens                int64
	inputTokenReportedCount        int64
	outputTokenReportedCount       int64
	cachedInputTokenReportedCount  int64
	reasoningTokenReportedCount    int64
	cacheEligibleInputTokens       int64
	cacheEligibleCachedInputTokens int64
}

func usageDailyDeltaFor(status observability.RequestStatus, usage *normalize.Usage) usageDailyDelta {
	result := usageDailyDelta{requestCount: 1}
	switch status {
	case observability.RequestStatusSucceeded:
		result.succeededCount = 1
	case observability.RequestStatusFailed:
		result.failedCount = 1
	case observability.RequestStatusCancelled:
		result.cancelledCount = 1
	}
	if usage == nil {
		return result
	}
	result.inputTokens, result.inputTokenReportedCount = tokenValue(usage.InputTokens)
	result.outputTokens, result.outputTokenReportedCount = tokenValue(usage.OutputTokens)
	result.cachedInputTokens, result.cachedInputTokenReportedCount = tokenValue(usage.CachedInputTokens)
	result.cacheWriteTokens, _ = tokenValue(usage.CacheWriteTokens)
	result.reasoningTokens, result.reasoningTokenReportedCount = tokenValue(usage.ReasoningTokens)
	if usage.InputTokens != nil && usage.CachedInputTokens != nil {
		result.cacheEligibleInputTokens = *usage.InputTokens
		result.cacheEligibleCachedInputTokens = *usage.CachedInputTokens
	}
	return result
}

func tokenValue(value *int64) (int64, int64) {
	if value == nil {
		return 0, 0
	}
	return *value, 1
}
