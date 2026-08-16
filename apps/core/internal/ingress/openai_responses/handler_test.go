package openai_responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestHandlerRejectsUnknownFieldsAndReturnsPendingAdapter(t *testing.T) {
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
	if valid.Code != http.StatusNotImplemented || !strings.Contains(valid.Body.String(), `"code":"unsupported_feature"`) {
		t.Fatalf("合法请求未进入待实现适配器状态: status=%d body=%s", valid.Code, valid.Body.String())
	}
}

type testGateway struct{}

func (testGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (testGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}
