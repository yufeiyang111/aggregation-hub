package openai_responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/normalize"
)

func TestHandlerRendersResponsesSSE(t *testing.T) {
	handler, err := NewHandler(responsesStreamGateway{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","stream":true,"input":"你好"}`)))
	body := response.Body.String()
	for _, expected := range []string{
		"event: response.created",
		"event: response.output_item.added",
		"event: response.output_text.delta",
		"event: response.function_call_arguments.delta",
		"event: response.output_item.done",
		"event: response.completed",
		`"status":"completed"`,
		`"input_tokens":3`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Responses SSE 缺少 %q: %s", expected, body)
		}
	}
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("Responses SSE 响应头错误: status=%d headers=%v", response.Code, response.Header())
	}
}

func TestHandlerRendersResponsesSSEError(t *testing.T) {
	handler, err := NewHandler(responsesStreamErrorGateway{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","stream":true,"input":"你好"}`)))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "event: error") || !strings.Contains(body, `"code":"upstream_error"`) || strings.Contains(body, "private upstream detail") {
		t.Fatalf("Responses SSE 错误序列化错误: status=%d body=%s", response.Code, body)
	}
}

func TestHandlerRendersResponsesSSEIncompleteAndAvoidsDuplicateTerminal(t *testing.T) {
	handler, err := NewHandler(responsesStreamIncompleteGateway{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","stream":true,"input":"你好"}`)))
	body := response.Body.String()
	if strings.Count(body, "event: response.incomplete") != 1 || strings.Contains(body, "event: error") || !strings.Contains(body, `"status":"incomplete"`) {
		t.Fatalf("Responses incomplete 终态错误: %s", body)
	}
}

type responsesStreamGateway struct{}

func (responsesStreamGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (responsesStreamGateway) Stream(ctx context.Context, input normalize.NormalizedRequest, emitter normalize.StreamEmitter) error {
	if !input.Stream {
		return context.Canceled
	}
	events := []normalize.NormalizedEvent{
		normalize.ResponseStartEvent{ResponseID: "resp_out_1", Model: "gpt-out"},
		normalize.ContentStartEvent{ContentID: "msg_out_1", Kind: normalize.PartKindText},
		normalize.TextDeltaEvent{ContentID: "msg_out_1", Text: "完成"},
		normalize.ContentEndEvent{ContentID: "msg_out_1"},
		normalize.ContentStartEvent{ContentID: "fc_out_1", Kind: normalize.PartKindToolCall},
		normalize.ToolCallStartEvent{ContentID: "fc_out_1", CallID: "call_out_1", Name: "lookup"},
		normalize.ToolCallArgumentsDeltaEvent{ContentID: "fc_out_1", CallID: "call_out_1", Delta: `{"query":"x"}`},
		normalize.ContentEndEvent{ContentID: "fc_out_1"},
		normalize.UsageUpdateEvent{Usage: normalize.Usage{InputTokens: int64Pointer(3), OutputTokens: int64Pointer(2), Source: normalize.UsageSourceUpstreamReported}},
		normalize.ResponseEndEvent{FinishReason: normalize.FinishReasonToolCalls},
	}
	for _, event := range events {
		if err := emitter.Emit(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type responsesStreamErrorGateway struct{}

func (responsesStreamErrorGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (responsesStreamErrorGateway) Stream(ctx context.Context, _ normalize.NormalizedRequest, emitter normalize.StreamEmitter) error {
	if err := emitter.Emit(ctx, normalize.ResponseStartEvent{ResponseID: "resp_error_1", Model: "gpt-out"}); err != nil {
		return err
	}
	return emitter.Emit(ctx, normalize.ErrorEvent{Code: "upstream_error", Message: "上游响应失败"})
}

func int64Pointer(value int64) *int64 { return &value }

type responsesStreamIncompleteGateway struct{}

func (responsesStreamIncompleteGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (responsesStreamIncompleteGateway) Stream(ctx context.Context, _ normalize.NormalizedRequest, emitter normalize.StreamEmitter) error {
	if err := emitter.Emit(ctx, normalize.ResponseStartEvent{ResponseID: "resp_incomplete_1", Model: "gpt-out"}); err != nil {
		return err
	}
	if err := emitter.Emit(ctx, normalize.ResponseEndEvent{FinishReason: normalize.FinishReasonLength}); err != nil {
		return err
	}
	return context.Canceled
}
