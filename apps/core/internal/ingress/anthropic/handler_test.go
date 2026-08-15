package anthropic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/ingress/anthropic"
	"aggregationhub.local/core/internal/normalize"
)

func TestHandlerNormalizesTextAndRendersAnthropicMessage(t *testing.T) {
	handler, err := anthropic.NewHandler(fakeGateway{})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"bundle/model","max_tokens":256,"system":"只用中文","messages":[{"role":"user","content":"你好"}]}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"type":"message"`) || !strings.Contains(body, `"role":"assistant"`) || !strings.Contains(body, `"text":"完成"`) || strings.Contains(body, "provider.example") {
		t.Fatalf("响应错误 status=%d body=%s", response.Code, body)
	}
}

type fakeGateway struct{}

func (fakeGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "msg_1", Model: "bundle/model", Parts: []normalize.ContentPart{normalize.TextPart{Text: "完成"}}, FinishReason: normalize.FinishReasonStop}, nil
}

func (fakeGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

func TestHandlerPreservesToolUseAndToolResultBlocks(t *testing.T) {
	gateway := &recordingGateway{}
	handler, err := anthropic.NewHandler(gateway)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
      "model":"bundle/model",
      "max_tokens":256,
      "tools":[{"name":"read_file","description":"读取文件","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
      "tool_choice":{"type":"tool","name":"read_file"},
      "messages":[
        {"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"a.txt"}}]},
        {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"文件内容"}]}
      ]
    }`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(gateway.request.Tools) != 1 || gateway.request.ToolChoice.Mode != normalize.ToolChoiceNamed || gateway.request.ToolChoice.Name != "read_file" {
		t.Fatalf("工具定义或选择未保留：%#v", gateway.request)
	}
	if len(gateway.request.Messages) != 2 || len(gateway.request.Messages[0].Parts) != 1 || len(gateway.request.Messages[1].Parts) != 1 {
		t.Fatalf("工具消息未保留：%#v", gateway.request.Messages)
	}
	call, ok := gateway.request.Messages[0].Parts[0].(normalize.ToolCallPart)
	if !ok || call.CallID != "toolu_1" || call.Name != "read_file" || call.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("tool_use 映射错误：%#v", gateway.request.Messages[0].Parts[0])
	}
	result, ok := gateway.request.Messages[1].Parts[0].(normalize.ToolResultPart)
	if !ok || result.CallID != "toolu_1" || result.Content != "文件内容" {
		t.Fatalf("tool_result 映射错误：%#v", gateway.request.Messages[1].Parts[0])
	}
}

func TestHandlerRendersToolUseResponse(t *testing.T) {
	handler, err := anthropic.NewHandler(toolGateway{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"bundle/model","max_tokens":256,"messages":[{"role":"user","content":"读取文件"}]}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"id":"toolu_2"`) || !strings.Contains(body, `"input":{"path":"a.txt"}`) || !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("Tool 响应错误 status=%d body=%s", response.Code, body)
	}
}

func TestHandlerRejectsInvalidAndUnsupportedRequests(t *testing.T) {
	handler, err := anthropic.NewHandler(fakeGateway{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		body     string
		wantCode int
		wantType string
	}{
		{name: "缺少 max tokens", body: `{"model":"bundle/model","messages":[{"role":"user","content":"你好"}]}`, wantCode: http.StatusBadRequest, wantType: "invalid_request_error"},
		{name: "未知字段", body: `{"model":"bundle/model","max_tokens":1,"messages":[{"role":"user","content":"你好"}],"danger":true}`, wantCode: http.StatusBadRequest, wantType: "invalid_request_error"},
		{name: "角色无效", body: `{"model":"bundle/model","max_tokens":1,"messages":[{"role":"system","content":"你好"}]}`, wantCode: http.StatusBadRequest, wantType: "invalid_request_error"},
		{name: "图片未支持", body: `{"model":"bundle/model","max_tokens":1,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64"}}]}]}`, wantCode: http.StatusBadRequest, wantType: "unsupported_feature"},
		{name: "流式未支持", body: `{"model":"bundle/model","max_tokens":1,"stream":true,"messages":[{"role":"user","content":"你好"}]}`, wantCode: http.StatusBadRequest, wantType: "unsupported_feature"},
		{name: "孤立工具结果", body: `{"model":"bundle/model","max_tokens":1,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_missing","content":"结果"}]}]}`, wantCode: http.StatusBadRequest, wantType: "invalid_request_error"},
		{name: "无工具的工具选择", body: `{"model":"bundle/model","max_tokens":1,"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"你好"}]}`, wantCode: http.StatusBadRequest, wantType: "invalid_request_error"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(testCase.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.wantCode || !strings.Contains(response.Body.String(), `"type":"`+testCase.wantType+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

type recordingGateway struct{ request normalize.NormalizedRequest }

func (gateway *recordingGateway) Complete(_ context.Context, input normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	gateway.request = input
	return normalize.NormalizedResponse{ID: "msg_2", Model: input.Model, Parts: []normalize.ContentPart{normalize.TextPart{Text: "已处理"}}, FinishReason: normalize.FinishReasonStop}, nil
}

func (gateway *recordingGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

type toolGateway struct{}

func (toolGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "msg_2", Model: "bundle/model", Parts: []normalize.ContentPart{normalize.ToolCallPart{CallID: "toolu_2", Name: "read_file", Arguments: `{"path":"a.txt"}`}}, FinishReason: normalize.FinishReasonToolCalls}, nil
}

func (toolGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}

func TestHandlerRejectsOversizedAndCrossTypeContent(t *testing.T) {
	handler, err := anthropic.NewHandler(fakeGateway{})
	if err != nil {
		t.Fatal(err)
	}
	oversized := `{"model":"bundle/model","max_tokens":1,"messages":[{"role":"user","content":"` + strings.Repeat("x", 8*1024*1024) + `"}]}`
	cases := []struct {
		name string
		body string
	}{
		{name: "内容块跨类型字段", body: `{"model":"bundle/model","max_tokens":1,"messages":[{"role":"user","content":[{"type":"text","text":"你好","tool_use_id":"toolu_1"}]}]}`},
		{name: "请求体过大", body: oversized},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(testCase.body)))
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"type":"invalid_request_error"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHandlerRejectsUnrepresentableGatewayTerminalState(t *testing.T) {
	handler, err := anthropic.NewHandler(cancelledGateway{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"bundle/model","max_tokens":1,"messages":[{"role":"user","content":"你好"}]}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"type":"api_error"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

type cancelledGateway struct{}

func (cancelledGateway) Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "msg_cancelled", Model: "bundle/model", Parts: []normalize.ContentPart{normalize.TextPart{Text: "部分结果"}}, FinishReason: normalize.FinishReasonCancelled}, nil
}

func (cancelledGateway) Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error {
	return nil
}
