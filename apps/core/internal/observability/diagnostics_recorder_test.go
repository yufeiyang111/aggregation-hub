package observability_test

import (
	"context"
	"errors"
	"testing"

	"aggregationhub.local/core/internal/observability"
)

func TestSafeLoggingRecorderRecordsOnlySuccessfulFailureTransitions(t *testing.T) {
	delegateLifecycle := &recordingLifecycle{}
	delegate := recordingRecorder{lifecycle: delegateLifecycle}
	logger := observability.NewSafeLogger(10)

	recorder, err := observability.NewSafeLoggingRecorder(delegate, logger)
	if err != nil {
		t.Fatal(err)
	}

	lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{
		SourceProtocol:      observability.ProtocolOpenAIResponses,
		Endpoint:            "/v1/responses",
		PublicModelSnapshot: "demo/model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Fail(context.Background(), observability.Failure{
		HTTPStatus: 504,
		Code:       "upstream_timeout",
	}); err != nil {
		t.Fatal(err)
	}

	summaries := logger.RecentErrors()
	if len(summaries) != 1 {
		t.Fatalf("安全失败摘要数量=%d，期望 1", len(summaries))
	}
	if summaries[0].EventCode != "gateway.request_failed" || summaries[0].ErrorCode != "upstream_timeout" {
		t.Fatalf("安全失败摘要=%+v", summaries[0])
	}
	if summaries[0].ProviderSlug != "demo" || summaries[0].PublicModelID != "demo/model-a" || summaries[0].SourceProtocol != "openai_responses" {
		t.Fatalf("安全失败上下文=%+v", summaries[0])
	}
	if summaries[0].HTTPStatus != 504 || summaries[0].OccurredAt.IsZero() {
		t.Fatalf("安全失败元数据=%+v", summaries[0])
	}
}

func TestSafeLoggingRecorderDoesNotRecordCompletionOrCancellation(t *testing.T) {
	tests := []struct {
		name   string
		finish func(observability.RequestLifecycle) error
	}{
		{
			name: "completion",
			finish: func(lifecycle observability.RequestLifecycle) error {
				return lifecycle.Complete(context.Background(), observability.Completion{HTTPStatus: 200})
			},
		},
		{
			name: "cancellation",
			finish: func(lifecycle observability.RequestLifecycle) error {
				return lifecycle.Cancel(context.Background(), "client_cancelled")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := observability.NewSafeLogger(10)
			recorder, err := observability.NewSafeLoggingRecorder(recordingRecorder{lifecycle: &recordingLifecycle{}}, logger)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{
				SourceProtocol:      observability.ProtocolOpenAIChat,
				Endpoint:            "/v1/chat/completions",
				PublicModelSnapshot: "demo/model-a",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.finish(lifecycle); err != nil {
				t.Fatal(err)
			}
			if summaries := logger.RecentErrors(); len(summaries) != 0 {
				t.Fatalf("完成或取消不应写入安全失败摘要: %+v", summaries)
			}
		})
	}
}

func TestSafeLoggingRecorderRecordsFailureWhenDelegatePersistenceFails(t *testing.T) {
	logger := observability.NewSafeLogger(10)
	recorder, err := observability.NewSafeLoggingRecorder(recordingRecorder{lifecycle: &recordingLifecycle{failErr: errors.New("storage-failure")}}, logger)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{
		SourceProtocol:      observability.ProtocolOpenAIResponses,
		Endpoint:            "/v1/responses",
		PublicModelSnapshot: "demo/model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Fail(context.Background(), observability.Failure{Code: "upstream_timeout"}); err == nil {
		t.Fatal("委托终态失败必须返回错误")
	}
	summaries := logger.RecentErrors()
	if len(summaries) != 1 || summaries[0].ErrorCode != "upstream_timeout" {
		t.Fatalf("持久化失败时仍应保留安全失败摘要: %+v", summaries)
	}
}

func TestSafeLoggingRecorderRejectsDuplicateTerminalTransition(t *testing.T) {
	logger := observability.NewSafeLogger(10)
	recorder, err := observability.NewSafeLoggingRecorder(recordingRecorder{lifecycle: &recordingLifecycle{}}, logger)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := recorder.Start(context.Background(), observability.RequestStart{
		SourceProtocol:      observability.ProtocolOpenAIResponses,
		Endpoint:            "/v1/responses",
		PublicModelSnapshot: "demo/model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Complete(context.Background(), observability.Completion{HTTPStatus: 200}); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Fail(context.Background(), observability.Failure{Code: "upstream_timeout"}); !errors.Is(err, observability.ErrInvalidRequestTransition) {
		t.Fatalf("重复终态错误=%v", err)
	}
	if summaries := logger.RecentErrors(); len(summaries) != 0 {
		t.Fatalf("重复终态不应写入诊断摘要: %+v", summaries)
	}
}

func TestSafeLoggingRecorderRejectsMissingDependencies(t *testing.T) {
	logger := observability.NewSafeLogger(1)
	if _, err := observability.NewSafeLoggingRecorder(nil, logger); err == nil {
		t.Fatal("缺少委托 Recorder 时必须拒绝")
	}
	if _, err := observability.NewSafeLoggingRecorder(recordingRecorder{}, nil); err == nil {
		t.Fatal("缺少安全日志时必须拒绝")
	}
}

type recordingRecorder struct {
	lifecycle observability.RequestLifecycle
}

func (recorder recordingRecorder) Start(context.Context, observability.RequestStart) (observability.RequestLifecycle, error) {
	return recorder.lifecycle, nil
}

type recordingLifecycle struct {
	failErr error
}

func (lifecycle *recordingLifecycle) MarkStreaming(context.Context) error {
	return nil
}

func (lifecycle *recordingLifecycle) Complete(context.Context, observability.Completion) error {
	return nil
}

func (lifecycle *recordingLifecycle) Fail(context.Context, observability.Failure) error {
	return lifecycle.failErr
}

func (lifecycle *recordingLifecycle) Cancel(context.Context, string) error {
	return nil
}
