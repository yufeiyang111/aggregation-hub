package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

func TestHandlerRecordsAnthropicLifecycle(t *testing.T) {
	recorder := &observationRecorder{lifecycle: &observationLifecycle{}}
	handler, err := NewHandler(observationGateway{}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"demo/model-a","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)))
	if response.Code != http.StatusOK || recorder.start.SourceProtocol != observability.ProtocolAnthropicMessages || recorder.start.Endpoint != "/v1/messages" || recorder.lifecycle.completed != 1 {
		t.Fatalf("观测生命周期错误 status=%d start=%+v lifecycle=%+v", response.Code, recorder.start, recorder.lifecycle)
	}
}

type observationGateway struct{}

func (observationGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "msg_obs", Model: "demo/model-a", Parts: []normalize.ContentPart{normalize.TextPart{Text: "ok"}}, FinishReason: normalize.FinishReasonStop}, nil
}
func (observationGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

type observationRecorder struct {
	start     observability.RequestStart
	lifecycle *observationLifecycle
}

func (value *observationRecorder) Start(_ context.Context, start observability.RequestStart) (observability.RequestLifecycle, error) {
	value.start = start
	return value.lifecycle, nil
}

type observationLifecycle struct{ streaming, completed, failed, cancelled int }

func (value *observationLifecycle) MarkStreaming(context.Context) error {
	value.streaming++
	return nil
}
func (value *observationLifecycle) Complete(context.Context, observability.Completion) error {
	value.completed++
	return nil
}
func (value *observationLifecycle) Fail(context.Context, observability.Failure) error {
	value.failed++
	return nil
}
func (value *observationLifecycle) Cancel(context.Context, string) error {
	value.cancelled++
	return nil
}

func TestHandlerRecordsAnthropicStreamingLifecycle(t *testing.T) {
	recorder := &observationRecorder{lifecycle: &observationLifecycle{}}
	handler, err := NewHandler(observationStreamingGateway{}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"demo/model-a","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hello"}]}`)))
	if response.Code != http.StatusOK || recorder.lifecycle.streaming != 1 || recorder.lifecycle.completed != 1 || recorder.lifecycle.failed != 0 {
		t.Fatalf("流式观测错误 status=%d lifecycle=%+v", response.Code, recorder.lifecycle)
	}
}

type observationStreamingGateway struct{}

func (observationStreamingGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (observationStreamingGateway) Stream(ctx context.Context, _ normalize.NormalizedRequest, emit normalize.StreamEmitter) error {
	for _, event := range []normalize.NormalizedEvent{normalize.ResponseStartEvent{ResponseID: "msg_stream", Model: "demo/model-a"}, normalize.ResponseEndEvent{FinishReason: normalize.FinishReasonStop}} {
		if err := emit.Emit(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
