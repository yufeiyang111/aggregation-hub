package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/storage"
)

func TestRequestRepositoryListsStableCursorAndSafeProjection(t *testing.T) {
	database := openMigratedDatabase(t)
	base := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	insertRequestRow(t, database, "r3", base.Add(3*time.Minute), "succeeded", "demo", "demo/model", "openai_responses", 4)
	insertRequestRow(t, database, "r2", base.Add(2*time.Minute), "failed", "demo", "demo/model", "openai_responses", 3)
	insertRequestRow(t, database, "r1", base.Add(time.Minute), "succeeded", "other", "other/model", "openai_chat", 2)

	repository, err := storage.NewRequestRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.List(context.Background(), observability.RequestListQuery{PageSize: 2, ProviderSlug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != 2 || first.Data[0].ID != "r3" || first.Data[1].ID != "r2" || first.NextCursor != nil {
		t.Fatalf("首页=%+v", first)
	}
	filtered, err := repository.List(context.Background(), observability.RequestListQuery{PageSize: 1, Status: observability.RequestStatusSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Data) != 1 || filtered.Data[0].ID != "r3" || filtered.NextCursor == nil {
		t.Fatalf("筛选页=%+v", filtered)
	}
	next, err := repository.List(context.Background(), observability.RequestListQuery{PageSize: 1, Status: observability.RequestStatusSucceeded, Cursor: *filtered.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Data) != 1 || next.Data[0].ID != "r1" {
		t.Fatalf("下一页=%+v", next)
	}
	if first.Data[0].InputTokens == nil || *first.Data[0].InputTokens != 4 || first.Data[0].DurationMS == nil {
		t.Fatalf("Token 或时延投影异常=%+v", first.Data[0])
	}
}

func TestRequestRepositoryRejectsInvalidCursorAndReturnsNotFound(t *testing.T) {
	repository, err := storage.NewRequestRepository(openMigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.List(context.Background(), observability.RequestListQuery{PageSize: 1, Cursor: "not a cursor"}); !errors.Is(err, observability.ErrInvalidRequestQuery) {
		t.Fatalf("Cursor 错误=%v", err)
	}
	if _, err := repository.Get(context.Background(), "missing"); !errors.Is(err, observability.ErrRequestNotFound) {
		t.Fatalf("不存在请求错误=%v", err)
	}
}

func TestUsageRepositorySummarizesTokensWithoutCostAndPreservesUnknownCacheRate(t *testing.T) {
	database := openMigratedDatabase(t)
	_, err := database.Exec(`INSERT INTO usage_daily(date_utc,provider_slug_snapshot,public_model_snapshot,request_count,succeeded_count,failed_count,cancelled_count,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,input_token_reported_count,output_token_reported_count,cached_input_token_reported_count,reasoning_token_reported_count,cache_eligible_input_tokens,cache_eligible_cached_input_tokens,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "2026-08-15", "demo", "demo/model", 2, 1, 1, 0, 10, 5, 3, 1, 2, 2, 2, 2, 2, 10, 3, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO usage_daily(date_utc,provider_slug_snapshot,public_model_snapshot,request_count,succeeded_count,failed_count,cancelled_count,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,input_token_reported_count,output_token_reported_count,cached_input_token_reported_count,reasoning_token_reported_count,cache_eligible_input_tokens,cache_eligible_cached_input_tokens,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "2026-08-16", "demo", "demo/model", 1, 1, 0, 0, 0, 4, 0, 0, 0, 0, 1, 0, 0, 0, 0, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := storage.NewUsageRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	query := observability.UsageQuery{ProviderSlug: "demo", FromUTC: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), ToUTC: time.Date(2026, 8, 16, 23, 59, 59, 0, time.UTC)}
	summary, err := repository.Summary(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RequestCount != 3 || summary.OutputTokens != 9 || summary.CacheHitRateBasisPoints == nil || *summary.CacheHitRateBasisPoints != 3000 {
		t.Fatalf("用量汇总=%+v", summary)
	}
	series, err := repository.TimeSeries(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Data) != 2 || series.Data[1].CacheHitRateBasisPoints != nil {
		t.Fatalf("趋势=%+v", series)
	}
}

func insertRequestRow(t *testing.T, database *sql.DB, id string, createdAt time.Time, status, providerSlug, modelID, protocol string, inputTokens int64) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO requests(id,provider_slug_snapshot,public_model_snapshot,upstream_model_snapshot,source_protocol,endpoint,streaming,status,http_status,error_code,retryable,input_tokens,duration_ms,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, providerSlug, modelID, "upstream", protocol, "/v1/responses", 0, status, 200, nil, 0, inputTokens, 12, createdAt.UnixMilli(), createdAt.Add(time.Second).UnixMilli())
	if err != nil {
		t.Fatalf("插入请求 %s 失败: %v", id, err)
	}
}
