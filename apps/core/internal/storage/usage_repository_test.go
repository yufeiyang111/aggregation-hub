package storage_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/storage"
)

func TestUsageRepositoryAggregatesTokensAndCacheEligibilityIdempotently(t *testing.T) {
	database := openMigratedDatabase(t)
	requests, err := storage.NewRequestRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := storage.NewUsageRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	createUsageRequest(t, requests, "01H00000000000000000000101", started)
	input, output, cached, reasoning := int64(100), int64(40), int64(25), int64(5)
	if err := requests.Transition(context.Background(), observability.RequestTransition{ID: "01H00000000000000000000101", From: observability.RequestStatusPending, Status: observability.RequestStatusSucceeded, HTTPStatus: 200, Usage: &normalize.Usage{InputTokens: &input, OutputTokens: &output, CachedInputTokens: &cached, ReasoningTokens: &reasoning, Source: normalize.UsageSourceUpstreamReported}, At: started.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := requests.Transition(context.Background(), observability.RequestTransition{ID: "01H00000000000000000000101", From: observability.RequestStatusSucceeded, Status: observability.RequestStatusFailed, At: started.Add(2 * time.Second)}); err == nil {
		t.Fatal("重复终态不应重复累计")
	}

	createUsageRequest(t, requests, "01H00000000000000000000102", started)
	if err := requests.Transition(context.Background(), observability.RequestTransition{ID: "01H00000000000000000000102", From: observability.RequestStatusPending, Status: observability.RequestStatusSucceeded, HTTPStatus: 200, Usage: &normalize.Usage{Source: normalize.UsageSourceUnknown}, At: started.Add(3 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	createUsageRequest(t, requests, "01H00000000000000000000103", started)
	if err := requests.Transition(context.Background(), observability.RequestTransition{ID: "01H00000000000000000000103", From: observability.RequestStatusPending, Status: observability.RequestStatusCancelled, ErrorCode: "client_cancelled", At: started.Add(4 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	daily, err := usage.Find(context.Background(), observability.DailyUsageKey{DateUTC: "2026-08-16", ProviderSlugSnapshot: "demo", PublicModelSnapshot: "demo/model-a"})
	if err != nil {
		t.Fatal(err)
	}
	if daily.RequestCount != 3 || daily.SucceededCount != 2 || daily.CancelledCount != 1 || daily.InputTokens != input || daily.OutputTokens != output || daily.CachedInputTokens != cached || daily.ReasoningTokens != reasoning || daily.InputTokenReportedCount != 1 || daily.OutputTokenReportedCount != 1 || daily.CachedInputTokenReportedCount != 1 || daily.CacheEligibleInputTokens != input || daily.CacheEligibleCachedInputTokens != cached {
		t.Fatalf("日汇总错误: %+v", daily)
	}
	if rate, known := daily.CacheHitRateBasisPoints(); !known || rate != 2500 {
		t.Fatalf("缓存命中率 rate=%d known=%v", rate, known)
	}
}

func TestUsageRepositoryAggregatesConcurrentTerminalRequests(t *testing.T) {
	database := openMigratedDatabase(t)
	requests, err := storage.NewRequestRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := storage.NewUsageRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	const requestTotal = 24
	input, output, cached := int64(20), int64(8), int64(5)
	for index := 0; index < requestTotal; index++ {
		createUsageRequest(t, requests, fmt.Sprintf("01H00000000000000000002%03d", index), started)
	}

	start := make(chan struct{})
	errors := make(chan error, requestTotal)
	var group sync.WaitGroup
	for index := 0; index < requestTotal; index++ {
		requestID := fmt.Sprintf("01H00000000000000000002%03d", index)
		group.Add(1)
		go func(id string) {
			defer group.Done()
			<-start
			errors <- requests.Transition(context.Background(), observability.RequestTransition{
				ID:         id,
				From:       observability.RequestStatusPending,
				Status:     observability.RequestStatusSucceeded,
				HTTPStatus: 200,
				Usage: &normalize.Usage{
					InputTokens:       &input,
					OutputTokens:      &output,
					CachedInputTokens: &cached,
					Source:            normalize.UsageSourceUpstreamReported,
				},
				At: started.Add(time.Second),
			})
		}(requestID)
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("并发终态写入失败: %v", err)
		}
	}

	daily, err := usage.Find(context.Background(), observability.DailyUsageKey{DateUTC: "2026-08-16", ProviderSlugSnapshot: "demo", PublicModelSnapshot: "demo/model-a"})
	if err != nil {
		t.Fatal(err)
	}
	if daily.RequestCount != requestTotal || daily.SucceededCount != requestTotal || daily.InputTokens != int64(requestTotal)*input || daily.OutputTokens != int64(requestTotal)*output || daily.CachedInputTokens != int64(requestTotal)*cached || daily.InputTokenReportedCount != requestTotal || daily.OutputTokenReportedCount != requestTotal || daily.CachedInputTokenReportedCount != requestTotal || daily.CacheEligibleInputTokens != int64(requestTotal)*input || daily.CacheEligibleCachedInputTokens != int64(requestTotal)*cached {
		t.Fatalf("并发日汇总错误: %+v", daily)
	}
	if rate, known := daily.CacheHitRateBasisPoints(); !known || rate != 2500 {
		t.Fatalf("并发缓存命中率 rate=%d known=%v", rate, known)
	}
}

func createUsageRequest(t *testing.T, repository *storage.RequestRepository, id string, createdAt time.Time) {
	t.Helper()
	record := observability.RequestRecord{ID: id, ProviderSlugSnapshot: "demo", PublicModelSnapshot: "demo/model-a", UpstreamModelSnapshot: "model-a", SourceProtocol: observability.ProtocolOpenAIResponses, Endpoint: "/v1/responses", Status: observability.RequestStatusPending, CreatedAt: createdAt}
	if err := repository.Create(context.Background(), record); err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
}
