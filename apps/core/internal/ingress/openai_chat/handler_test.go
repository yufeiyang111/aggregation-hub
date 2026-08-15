package openai_chat_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/ingress/openai_chat"
	"aggregationhub.local/core/internal/normalize"
)

func TestHandlerConvertsTextRequestAndWritesOpenAIResponse(t *testing.T) {
	handler, err := openai_chat.NewHandler(fakeGateway{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"bundle/model","messages":[{"role":"system","content":"规则"},{"role":"user","content":"你好"}]}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"object":"chat.completion"`) || !strings.Contains(response.Body.String(), `"content":"完成"`) || strings.Contains(response.Body.String(), "provider.example") {
		t.Fatalf("响应错误 status=%d body=%s", response.Code, response.Body.String())
	}
}
func TestHandlerRejectsUnknownFields(t *testing.T) {
	handler, _ := openai_chat.NewHandler(fakeGateway{})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"bundle/model","messages":[],"danger":true}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

type fakeGateway struct{}

func (fakeGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "resp_1", Model: "upstream", Parts: []normalize.ContentPart{normalize.TextPart{Text: "完成"}}, FinishReason: normalize.FinishReasonStop}, nil
}
func (fakeGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

func TestHandlerStreamsOpenAICompatibleSSE(t *testing.T) {
	handler, err := openai_chat.NewHandler(streamGateway{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"bundle/model","stream":true,"messages":[{"role":"user","content":"你好"}]}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(body, `"content":"分块"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("SSE 响应错误 status=%d headers=%v body=%s", response.Code, response.Header(), body)
	}
}

type streamGateway struct{}

func (streamGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (streamGateway) Stream(ctx context.Context, _ normalize.NormalizedRequest, emit normalize.StreamEmitter) error {
	if err := emit.Emit(ctx, normalize.ResponseStartEvent{ResponseID: "resp_stream", Model: "model"}); err != nil {
		return err
	}
	if err := emit.Emit(ctx, normalize.ContentStartEvent{ContentID: "text_0", Kind: normalize.PartKindText}); err != nil {
		return err
	}
	if err := emit.Emit(ctx, normalize.TextDeltaEvent{ContentID: "text_0", Text: "分块"}); err != nil {
		return err
	}
	if err := emit.Emit(ctx, normalize.ContentEndEvent{ContentID: "text_0"}); err != nil {
		return err
	}
	return emit.Emit(ctx, normalize.ResponseEndEvent{FinishReason: normalize.FinishReasonStop})
}
