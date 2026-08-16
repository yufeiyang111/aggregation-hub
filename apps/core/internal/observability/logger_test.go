package observability_test

import (
	"testing"
	"time"

	"aggregationhub.local/core/internal/observability"
)

func TestSafeLoggerKeepsOnlyNewestSafeErrorSummaries(t *testing.T) {
	logger := observability.NewSafeLogger(2)
	createdAt := time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC)
	logger.Record(observability.SafeErrorEvent{EventCode: "provider.request_failed", ErrorCode: "upstream_timeout", ProviderSlug: "demo", PublicModelID: "demo/model-a", SourceProtocol: "openai_responses", HTTPStatus: 504, DurationMS: 1200, OccurredAt: createdAt})
	logger.Record(observability.SafeErrorEvent{EventCode: "provider.request_failed", ErrorCode: "upstream_unavailable", ProviderSlug: "demo", PublicModelID: "demo/model-a", SourceProtocol: "openai_responses", HTTPStatus: 503, DurationMS: 300, OccurredAt: createdAt.Add(time.Second)})
	logger.Record(observability.SafeErrorEvent{EventCode: "gateway.request_cancelled", ErrorCode: "client_cancelled", ProviderSlug: "demo", PublicModelID: "demo/model-a", SourceProtocol: "openai_responses", HTTPStatus: 499, DurationMS: 10, OccurredAt: createdAt.Add(2 * time.Second)})

	summaries := logger.RecentErrors()
	if len(summaries) != 2 {
		t.Fatalf("安全错误摘要数量=%d，期望 2", len(summaries))
	}
	if summaries[0].ErrorCode != "upstream_unavailable" || summaries[1].ErrorCode != "client_cancelled" {
		t.Fatalf("安全错误摘要 FIFO 错误: %+v", summaries)
	}
	if summaries[0].OccurredAt != createdAt.Add(time.Second) || summaries[1].OccurredAt != createdAt.Add(2*time.Second) {
		t.Fatalf("安全错误摘要时间错误: %+v", summaries)
	}
}

func TestSafeLoggerRejectsSecretLikeIdentifierFields(t *testing.T) {
	logger := observability.NewSafeLogger(1)
	secretLikeValue := "sk-diagnosticapikeysecret12345"
	logger.Record(observability.SafeErrorEvent{EventCode: "gateway.request_failed", ErrorCode: "upstream_failed", ProviderSlug: secretLikeValue, PublicModelID: "demo/model-a", OccurredAt: time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC)})

	summaries := logger.RecentErrors()
	if len(summaries) != 1 {
		t.Fatalf("安全错误摘要数量=%d，期望 1", len(summaries))
	}
	if summaries[0].ProviderSlug != "invalid" {
		t.Fatalf("疑似秘密字段不得原样记录: %+v", summaries[0])
	}
}
