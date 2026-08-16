package openai_responses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/normalize"
)

func TestNormalizeResponsesTextAndFunctionItems(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","instructions":"规则","input":[{"type":"message","role":"user","content":"读取文件"},{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"a.txt\"}"},{"type":"function_call_output","call_id":"call_1","output":"完成"}],"tools":[{"type":"function","name":"read_file","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"read_file"}}`))
	value, err := decodeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeInput(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Messages) != 3 || len(normalized.System) != 1 || len(normalized.Tools) != 1 {
		t.Fatalf("规范化结果错误: %#v", normalized)
	}
	if normalized.Messages[1].Role != normalize.RoleAssistant || normalized.Messages[2].Role != normalize.RoleTool {
		t.Fatalf("Function Call/Output 角色错误: %#v", normalized.Messages)
	}
	if normalized.ToolChoice.Mode != normalize.ToolChoiceNamed || normalized.ToolChoice.Name != "read_file" {
		t.Fatalf("tool_choice 规范化错误: %#v", normalized.ToolChoice)
	}
}

func TestNormalizeResponsesStringInput(t *testing.T) {
	value, err := decodeRequest(httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","input":"你好"}`)))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeInput(value)
	if err != nil || len(normalized.Messages) != 1 {
		t.Fatalf("字符串 input 规范化失败: normalized=%#v err=%v", normalized, err)
	}
}

func TestNormalizeResponsesRejectsUnsupportedItems(t *testing.T) {
	cases := []string{
		`{"model":"bundle/model","input":[{"type":"computer_call","id":"x"}]}`,
		`{"model":"bundle/model","stream":true,"input":"你好"}`,
		`{"model":"bundle/model","reasoning":{"effort":"high"},"input":"你好"}`,
		`{"model":"bundle/model","input":[{"type":"function_call_output","call_id":"missing","output":"x"}]}`,
	}
	for _, body := range cases {
		value, err := decodeRequest(httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := normalizeInput(value); err == nil {
			t.Fatalf("未拒绝 Responses 请求: %s", body)
		}
	}
}

func TestHandlerRendersResponsesTextAndFunctionCall(t *testing.T) {
	handler, err := NewHandler(responseGateway{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","input":"你好"}`)))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"object":"response"`) || !strings.Contains(body, `"type":"output_text"`) || !strings.Contains(body, `"text":"完成"`) || !strings.Contains(body, `"type":"function_call"`) {
		t.Fatalf("Responses 响应错误: status=%d body=%s", response.Code, body)
	}
}

func TestHandlerRejectsUnknownFieldsAndCompletesValidRequest(t *testing.T) {
	handler, err := NewHandler(testGateway{})
	if err != nil {
		t.Fatal(err)
	}
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","input":"你好","danger":true}`)))
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("未知字段未被拒绝: status=%d", unknown.Code)
	}
	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","input":"你好"}`)))
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"object":"response"`) {
		t.Fatalf("合法请求未完成: status=%d body=%s", valid.Code, valid.Body.String())
	}
}

type testGateway struct{}

type responseGateway struct{}

func (responseGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "resp_1", Model: "upstream", Parts: []normalize.ContentPart{normalize.TextPart{Text: "完成"}, normalize.ToolCallPart{CallID: "call_1", Name: "lookup", Arguments: `{"query":"x"}`}}, FinishReason: normalize.FinishReasonToolCalls}, nil
}
func (responseGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

func (testGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (testGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

type gatewayErrorGateway struct{}

func (gatewayErrorGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, &adapter.GatewayError{Code: "upstream_rate_limited", SafeMessage: "上游服务限流", HTTPStatus: http.StatusTooManyRequests, Cause: errors.New("private upstream detail")}
}
func (gatewayErrorGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

func TestHandlerMapsGatewayErrorWithoutLeakingCause(t *testing.T) {
	handler, err := NewHandler(gatewayErrorGateway{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"bundle/model","input":"你好"}`)))
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"code":"upstream_rate_limited"`) || strings.Contains(response.Body.String(), "private upstream detail") {
		t.Fatalf("GatewayError 映射错误: status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("错误响应不是 JSON: %v", err)
	}
}
