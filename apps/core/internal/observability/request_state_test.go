package observability_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

func TestRequestStatusTransitionRejectsTerminalChanges(t *testing.T) {
	cases := []struct {
		from observability.RequestStatus
		to   observability.RequestStatus
		ok   bool
	}{
		{observability.RequestStatusPending, observability.RequestStatusStreaming, true},
		{observability.RequestStatusPending, observability.RequestStatusSucceeded, true},
		{observability.RequestStatusStreaming, observability.RequestStatusCancelled, true},
		{observability.RequestStatusSucceeded, observability.RequestStatusFailed, false},
		{observability.RequestStatusFailed, observability.RequestStatusStreaming, false},
		{observability.RequestStatusCancelled, observability.RequestStatusSucceeded, false},
		{observability.RequestStatusPending, observability.RequestStatusPending, false},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.from)+"_to_"+string(testCase.to), func(t *testing.T) {
			err := observability.ValidateRequestTransition(testCase.from, testCase.to)
			if testCase.ok && err != nil {
				t.Fatalf("状态转换被错误拒绝: %v", err)
			}
			if !testCase.ok && err == nil {
				t.Fatal("非法状态转换未被拒绝")
			}
		})
	}
}

func TestRecorderPersistsExactlyOneTerminalWithUsage(t *testing.T) {
	store := &memoryStore{}
	now := time.UnixMilli(1_700_000_000_000).UTC()
	recorder, err := observability.NewRecorder(store, observability.RecorderOptions{
		Clock: func() time.Time { return now },
		NewID: func(time.Time) (string, error) { return "01H00000000000000000000001", nil },
	})
	if err != nil {
		t.Fatalf("创建 Recorder 失败: %v", err)
	}
	lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{SourceProtocol: observability.ProtocolOpenAIResponses, Endpoint: "/v1/responses", PublicModelSnapshot: "demo/model-a", Streaming: true})
	if err != nil {
		t.Fatalf("开始请求记录失败: %v", err)
	}
	if err := lifecycle.MarkStreaming(context.Background()); err != nil {
		t.Fatalf("标记流式失败: %v", err)
	}
	tokens := int64(7)
	if err := lifecycle.Complete(context.Background(), observability.Completion{HTTPStatus: 200, Usage: &normalize.Usage{InputTokens: &tokens, Source: normalize.UsageSourceUpstreamReported}}); err != nil {
		t.Fatalf("完成请求失败: %v", err)
	}
	if err := lifecycle.Fail(context.Background(), observability.Failure{Code: "gateway_error"}); err == nil {
		t.Fatal("终态后的失败写入应被拒绝")
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.created.ProviderSlugSnapshot != "demo" || store.created.UpstreamModelSnapshot != "model-a" {
		t.Fatalf("模型快照错误: %+v", store.created)
	}
	if len(store.transitions) != 2 || store.transitions[0].Status != observability.RequestStatusStreaming || store.transitions[1].Status != observability.RequestStatusSucceeded {
		t.Fatalf("状态写入错误: %+v", store.transitions)
	}
	if store.transitions[1].Usage == nil || store.transitions[1].Usage.InputTokens == nil || *store.transitions[1].Usage.InputTokens != tokens {
		t.Fatalf("Usage 未被保留: %+v", store.transitions[1].Usage)
	}
}

func TestRecorderConcurrentTerminalRaceHasOneWinner(t *testing.T) {
	store := &memoryStore{}
	recorder, err := observability.NewRecorder(store, observability.RecorderOptions{
		Clock: func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() },
		NewID: func(time.Time) (string, error) { return "01H00000000000000000000002", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{SourceProtocol: observability.ProtocolOpenAIChat, Endpoint: "/v1/chat/completions", PublicModelSnapshot: "demo/model-a"})
	if err != nil {
		t.Fatal(err)
	}

	var successes int
	var successesMutex sync.Mutex
	var group sync.WaitGroup
	for _, finish := range []func() error{
		func() error {
			return lifecycle.Complete(context.Background(), observability.Completion{HTTPStatus: 200})
		},
		func() error { return lifecycle.Cancel(context.Background(), "client_cancelled") },
	} {
		group.Add(1)
		go func(finish func() error) {
			defer group.Done()
			if finish() == nil {
				successesMutex.Lock()
				successes++
				successesMutex.Unlock()
			}
		}(finish)
	}
	group.Wait()
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if successes != 1 || len(store.transitions) != 1 {
		t.Fatalf("并发终态写入次数错误 successes=%d transitions=%+v", successes, store.transitions)
	}
}

type memoryStore struct {
	mutex       sync.Mutex
	created     observability.RequestRecord
	transitions []observability.RequestTransition
}

func (store *memoryStore) Create(_ context.Context, record observability.RequestRecord) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.created = record
	return nil
}

func (store *memoryStore) Transition(_ context.Context, transition observability.RequestTransition) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.transitions = append(store.transitions, transition)
	return nil
}

func TestRecordingStreamEmitterUsesOneSuccessfulTerminal(t *testing.T) {
	store := &memoryStore{}
	recorder, err := observability.NewRecorder(store, observability.RecorderOptions{
		Clock: func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() },
		NewID: func(time.Time) (string, error) { return "01H00000000000000000000004", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{SourceProtocol: observability.ProtocolOpenAIChat, Endpoint: "/v1/chat/completions", PublicModelSnapshot: "demo/model-a", Streaming: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.MarkStreaming(context.Background()); err != nil {
		t.Fatal(err)
	}
	emitter := observability.NewRecordingStreamEmitter(discardingEmitter{}, lifecycle)
	tokens := int64(4)
	if err := emitter.Emit(context.Background(), normalize.UsageUpdateEvent{Usage: normalize.Usage{OutputTokens: &tokens, Source: normalize.UsageSourceUpstreamReported}}); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(context.Background(), normalize.ResponseEndEvent{FinishReason: normalize.FinishReasonStop}); err != nil {
		t.Fatal(err)
	}
	emitter.Finish(context.Background(), nil)
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.transitions) != 2 || store.transitions[1].Status != observability.RequestStatusSucceeded || store.transitions[1].Usage == nil || *store.transitions[1].Usage.OutputTokens != tokens {
		t.Fatalf("流式终态错误: %+v", store.transitions)
	}
}

type discardingEmitter struct{}

func (discardingEmitter) Emit(context.Context, normalize.NormalizedEvent) error { return nil }
