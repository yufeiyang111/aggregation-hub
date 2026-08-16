package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/controlplane"
	"aggregationhub.local/core/internal/observability"
)

type fakeRequestReader struct {
	page  observability.RequestPage
	item  observability.RequestMetadata
	err   error
	query observability.RequestListQuery
}

func (reader *fakeRequestReader) List(_ context.Context, query observability.RequestListQuery) (observability.RequestPage, error) {
	reader.query = query
	return reader.page, reader.err
}
func (reader *fakeRequestReader) Get(context.Context, string) (observability.RequestMetadata, error) {
	return reader.item, reader.err
}

type fakeUsageReader struct {
	summary observability.UsageSummary
	series  observability.UsageTimeSeries
	err     error
	query   observability.UsageQuery
}

func (reader *fakeUsageReader) Summary(_ context.Context, query observability.UsageQuery) (observability.UsageSummary, error) {
	reader.query = query
	return reader.summary, reader.err
}
func (reader *fakeUsageReader) TimeSeries(_ context.Context, query observability.UsageQuery) (observability.UsageTimeSeries, error) {
	reader.query = query
	return reader.series, reader.err
}

func TestObservabilityEndpointsValidateQueriesAndExcludeUnsafeFields(t *testing.T) {
	requests := &fakeRequestReader{page: observability.RequestPage{Data: []observability.RequestMetadata{{ID: "request-1", ProviderSlug: "demo", PublicModelID: "demo/model", Status: observability.RequestStatusSucceeded, SourceProtocol: observability.ProtocolOpenAIResponses, CreatedAt: time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)}}}}
	usage := &fakeUsageReader{summary: observability.WithCacheHitRate(observability.UsageSummary{RequestCount: 2, CacheEligibleInputTokens: 10, CacheEligibleCachedInputTokens: 4})}
	server := newObservabilityServer(t, requests, usage)
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/requests?page_size=1&status=succeeded&from_utc=2026-08-15T00:00:00Z&to_utc=2026-08-16T00:00:00Z", nil)
	request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("请求列表 status=%d body=%s", response.Code, response.Body.String())
	}
	if requests.query.PageSize != 1 || requests.query.Status != observability.RequestStatusSucceeded || requests.query.FromUTC == nil {
		t.Fatalf("解析查询=%+v", requests.query)
	}
	for _, forbidden := range []string{"prompt", "body", "header", "tool", "cost", "credential"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("响应暴露字段 %q: %s", forbidden, response.Body.String())
		}
	}
	var decoded struct {
		Data []observability.RequestMetadata `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil || len(decoded.Data) != 1 {
		t.Fatalf("请求 JSON=%s err=%v", response.Body.String(), err)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/internal/v1/requests?page_size=101", nil)
	invalid.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	invalidResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("page_size 上限 status=%d", invalidResponse.Code)
	}
	unsupported := httptest.NewRequest(http.MethodGet, "/internal/v1/requests?sort=cost", nil)
	unsupported.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	unsupportedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unsupportedResponse, unsupported)
	if unsupportedResponse.Code != http.StatusBadRequest {
		t.Fatalf("sort allowlist status=%d", unsupportedResponse.Code)
	}
}

func TestUsageEndpointsHandleUnknownRateAndDoNotLeakFailures(t *testing.T) {
	usage := &fakeUsageReader{summary: observability.UsageSummary{RequestCount: 1}, series: observability.UsageTimeSeries{Data: []observability.UsageTimeSeriesPoint{{DateUTC: "2026-08-16", UsageSummary: observability.UsageSummary{OutputTokens: 3}}}}}
	server := newObservabilityServer(t, &fakeRequestReader{}, usage)
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/usage/summary?provider_slug=demo&from_utc=2026-08-16T00:00:00Z&to_utc=2026-08-16T23:59:59Z", nil)
	request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"cache_hit_rate_basis_points\":null") {
		t.Fatalf("未知缓存命中率=%d %s", response.Code, response.Body.String())
	}
	if usage.query.ProviderSlug != "demo" {
		t.Fatalf("用量筛选=%+v", usage.query)
	}
	usage.err = errors.New("provider-secret")
	failed := httptest.NewRequest(http.MethodGet, "/internal/v1/usage/timeseries", nil)
	failed.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	failedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(failedResponse, failed)
	if failedResponse.Code != http.StatusServiceUnavailable || strings.Contains(failedResponse.Body.String(), "provider-secret") {
		t.Fatalf("失败泄漏=%d %s", failedResponse.Code, failedResponse.Body.String())
	}
}

func newObservabilityServer(t *testing.T, requests controlplane.RequestReader, usage controlplane.UsageReader) *controlplane.Server {
	t.Helper()
	server, err := controlplane.NewServer(controlplane.Options{ManagementToken: testManagementToken, Runtime: func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} }, Shutdown: func(context.Context) error { return nil }, RequestReader: requests, UsageReader: usage})
	if err != nil {
		t.Fatal(err)
	}
	return server
}
