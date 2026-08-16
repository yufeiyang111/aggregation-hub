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

// Summary 聚合 UTC 日期范围内的请求计数与 Token；不读取费用字段。
func (repository *UsageRepository) Summary(ctx context.Context, query observability.UsageQuery) (observability.UsageSummary, error) {
	if ctx == nil || observability.ValidateUsageQuery(query) != nil {
		return observability.UsageSummary{}, observability.ErrInvalidRequestQuery
	}
	where, arguments := usageQueryWhere(query)
	statement := `SELECT COALESCE(SUM(request_count),0),COALESCE(SUM(succeeded_count),0),COALESCE(SUM(failed_count),0),COALESCE(SUM(cancelled_count),0),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),COALESCE(SUM(cached_input_tokens),0),COALESCE(SUM(cache_write_tokens),0),COALESCE(SUM(reasoning_tokens),0),COALESCE(SUM(input_token_reported_count),0),COALESCE(SUM(output_token_reported_count),0),COALESCE(SUM(cached_input_token_reported_count),0),COALESCE(SUM(reasoning_token_reported_count),0),COALESCE(SUM(cache_eligible_input_tokens),0),COALESCE(SUM(cache_eligible_cached_input_tokens),0) FROM usage_daily` + where
	summary, err := scanUsageSummary(repository.database.QueryRowContext(ctx, statement, arguments...))
	if err != nil {
		return observability.UsageSummary{}, fmt.Errorf("读取用量汇总失败: %w", err)
	}
	return observability.WithCacheHitRate(summary), nil
}

// TimeSeries 返回 UTC 日期升序的同一用量投影。
func (repository *UsageRepository) TimeSeries(ctx context.Context, query observability.UsageQuery) (observability.UsageTimeSeries, error) {
	if ctx == nil || observability.ValidateUsageQuery(query) != nil {
		return observability.UsageTimeSeries{}, observability.ErrInvalidRequestQuery
	}
	where, arguments := usageQueryWhere(query)
	statement := `SELECT date_utc,SUM(request_count),SUM(succeeded_count),SUM(failed_count),SUM(cancelled_count),SUM(input_tokens),SUM(output_tokens),SUM(cached_input_tokens),SUM(cache_write_tokens),SUM(reasoning_tokens),SUM(input_token_reported_count),SUM(output_token_reported_count),SUM(cached_input_token_reported_count),SUM(reasoning_token_reported_count),SUM(cache_eligible_input_tokens),SUM(cache_eligible_cached_input_tokens) FROM usage_daily` + where + ` GROUP BY date_utc ORDER BY date_utc ASC`
	rows, err := repository.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return observability.UsageTimeSeries{}, fmt.Errorf("读取用量趋势失败: %w", err)
	}
	defer rows.Close()
	result := observability.UsageTimeSeries{Data: make([]observability.UsageTimeSeriesPoint, 0)}
	for rows.Next() {
		var point observability.UsageTimeSeriesPoint
		summary, err := scanUsageSummaryWithDate(rows, &point.DateUTC)
		if err != nil {
			return observability.UsageTimeSeries{}, err
		}
		point.UsageSummary = observability.WithCacheHitRate(summary)
		result.Data = append(result.Data, point)
	}
	if err := rows.Err(); err != nil {
		return observability.UsageTimeSeries{}, fmt.Errorf("遍历用量趋势失败: %w", err)
	}
	return result, nil
}

type usageSummaryScanner interface{ Scan(dest ...any) error }

func scanUsageSummary(scanner usageSummaryScanner) (observability.UsageSummary, error) {
	return scanUsageSummaryWithDate(scanner, nil)
}
func scanUsageSummaryWithDate(scanner usageSummaryScanner, dateUTC *string) (observability.UsageSummary, error) {
	var summary observability.UsageSummary
	destinations := []any{&summary.RequestCount, &summary.SucceededCount, &summary.FailedCount, &summary.CancelledCount, &summary.InputTokens, &summary.OutputTokens, &summary.CachedInputTokens, &summary.CacheWriteTokens, &summary.ReasoningTokens, &summary.InputTokenReportedCount, &summary.OutputTokenReportedCount, &summary.CachedInputTokenReportedCount, &summary.ReasoningTokenReportedCount, &summary.CacheEligibleInputTokens, &summary.CacheEligibleCachedInputTokens}
	if dateUTC != nil {
		destinations = append([]any{dateUTC}, destinations...)
	}
	if err := scanner.Scan(destinations...); err != nil {
		return observability.UsageSummary{}, err
	}
	return summary, nil
}

func usageQueryWhere(query observability.UsageQuery) (string, []any) {
	conditions := []string{"date_utc>=?", "date_utc<=?"}
	arguments := []any{query.FromUTC.UTC().Format("2006-01-02"), query.ToUTC.UTC().Format("2006-01-02")}
	if query.ProviderSlug != "" {
		conditions, arguments = append(conditions, "provider_slug_snapshot=?"), append(arguments, query.ProviderSlug)
	}
	if query.PublicModelID != "" {
		conditions, arguments = append(conditions, "public_model_snapshot=?"), append(arguments, query.PublicModelID)
	}
	return " WHERE " + strings.Join(conditions, " AND "), arguments
}
