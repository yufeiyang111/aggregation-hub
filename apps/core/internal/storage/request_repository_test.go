package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/storage"
)

func TestRequestRepositoryPersistsMetadataAndRejectsSecondTerminal(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewRequestRepository(database)
	if err != nil {
		t.Fatalf("创建请求仓储失败: %v", err)
	}
	createdAt := time.UnixMilli(1_700_000_000_000).UTC()
	record := observability.RequestRecord{
		ID:                    "01H00000000000000000000003",
		ProviderSlugSnapshot:  "demo",
		PublicModelSnapshot:   "demo/model-a",
		UpstreamModelSnapshot: "model-a",
		SourceProtocol:        observability.ProtocolOpenAIChat,
		Endpoint:              "/v1/chat/completions",
		Streaming:             true,
		Status:                observability.RequestStatusPending,
		CreatedAt:             createdAt,
	}
	if err := repository.Create(context.Background(), record); err != nil {
		t.Fatalf("写入请求失败: %v", err)
	}
	if err := repository.Transition(context.Background(), observability.RequestTransition{ID: record.ID, From: observability.RequestStatusPending, Status: observability.RequestStatusStreaming, At: createdAt.Add(time.Second)}); err != nil {
		t.Fatalf("转换为 streaming 失败: %v", err)
	}
	input := int64(3)
	if err := repository.Transition(context.Background(), observability.RequestTransition{ID: record.ID, From: observability.RequestStatusStreaming, Status: observability.RequestStatusSucceeded, HTTPStatus: 200, Usage: &normalize.Usage{InputTokens: &input, Source: normalize.UsageSourceUpstreamReported}, At: createdAt.Add(2 * time.Second)}); err != nil {
		t.Fatalf("转换为 succeeded 失败: %v", err)
	}
	err = repository.Transition(context.Background(), observability.RequestTransition{ID: record.ID, From: observability.RequestStatusSucceeded, Status: observability.RequestStatusFailed, ErrorCode: "gateway_error", At: createdAt.Add(3 * time.Second)})
	if !errors.Is(err, observability.ErrInvalidRequestTransition) {
		t.Fatalf("第二次终态错误=%v", err)
	}

	var status, source string
	var storedInput int64
	var startedAt, completedAt int64
	if err := database.QueryRow(`SELECT status,usage_source,input_tokens,started_stream_at,completed_at FROM requests WHERE id=?`, record.ID).Scan(&status, &source, &storedInput, &startedAt, &completedAt); err != nil {
		t.Fatalf("读取请求失败: %v", err)
	}
	if status != string(observability.RequestStatusSucceeded) || source != string(normalize.UsageSourceUpstreamReported) || storedInput != input || startedAt != createdAt.Add(time.Second).UnixMilli() || completedAt != createdAt.Add(2*time.Second).UnixMilli() {
		t.Fatalf("请求记录字段错误 status=%s source=%s input=%d started=%d completed=%d", status, source, storedInput, startedAt, completedAt)
	}
}

func TestRequestRepositoryRejectsInvalidMetadata(t *testing.T) {
	repository, err := storage.NewRequestRepository(openMigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	err = repository.Create(context.Background(), observability.RequestRecord{ID: "bad", PublicModelSnapshot: "invalid", SourceProtocol: observability.ProtocolOpenAIChat, Endpoint: "/v1/chat/completions", Status: observability.RequestStatusPending, CreatedAt: time.Now()})
	if err == nil {
		t.Fatal("无效请求元数据未被拒绝")
	}
}

func TestRequestRepositoryRejectsInvalidTransitionMetadata(t *testing.T) {
	repository, err := storage.NewRequestRepository(openMigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := []observability.RequestTransition{
		{ID: "request", From: observability.RequestStatusPending, Status: observability.RequestStatusSucceeded, HTTPStatus: 700, At: time.Now()},
		{ID: "request", From: observability.RequestStatusPending, Status: observability.RequestStatusSucceeded, Usage: &normalize.Usage{Source: normalize.UsageSource("invalid")}, At: time.Now()},
		{ID: "request", From: observability.RequestStatusPending, Status: observability.RequestStatusSucceeded, Usage: &normalize.Usage{InputTokens: negativeTokens(), Source: normalize.UsageSourceUpstreamReported}, At: time.Now()},
	}
	for _, transition := range cases {
		if err := repository.Transition(context.Background(), transition); !errors.Is(err, observability.ErrInvalidRequestTransition) {
			t.Fatalf("无效元数据错误=%v", err)
		}
	}
}

func negativeTokens() *int64 { value := int64(-1); return &value }
