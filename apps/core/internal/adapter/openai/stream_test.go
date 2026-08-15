package openai_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	openaiadapter "aggregationhub.local/core/internal/adapter/openai"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
)

func TestAdapterParseStreamEmitsTextSequence(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chat_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}
	emitter := &eventCollector{}
	if err := value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "p"}, response, emitter); err != nil {
		t.Fatalf("解析流失败: %v", err)
	}
	if len(emitter.events) != 6 {
		t.Fatalf("事件数量错误: %#v", emitter.events)
	}
	if _, ok := emitter.events[0].(normalize.ResponseStartEvent); !ok {
		t.Fatalf("首事件错误: %#v", emitter.events[0])
	}
	if _, ok := emitter.events[1].(normalize.ContentStartEvent); !ok {
		t.Fatalf("内容开始错误: %#v", emitter.events[1])
	}
	if first, ok := emitter.events[2].(normalize.TextDeltaEvent); !ok || first.Text != "Hel" {
		t.Fatalf("首文本错误: %#v", emitter.events[2])
	}
	if _, ok := emitter.events[4].(normalize.ContentEndEvent); !ok {
		t.Fatalf("内容结束错误: %#v", emitter.events[4])
	}
	if _, ok := emitter.events[5].(normalize.ResponseEndEvent); !ok {
		t.Fatalf("终态错误: %#v", emitter.events[5])
	}
}

func TestAdapterParseStreamEmitsToolCallArgumentDeltas(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chat_tool_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"weather\",\"arguments\":\"{\\\"city\\\":\"}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat_tool_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"北京\\\"}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat_tool_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"))}
	emitter := &eventCollector{}
	if err := value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "p"}, response, emitter); err != nil {
		t.Fatalf("解析 Tool 流失败: %v", err)
	}
	if len(emitter.events) != 7 {
		t.Fatalf("Tool 流事件数量错误: %#v", emitter.events)
	}
	start, ok := emitter.events[2].(normalize.ToolCallStartEvent)
	if !ok || start.CallID != "call_1" || start.Name != "weather" {
		t.Fatalf("Tool Call 起始事件错误: %#v", emitter.events[2])
	}
	first, ok := emitter.events[3].(normalize.ToolCallArgumentsDeltaEvent)
	if !ok || first.Delta != "{\"city\":" {
		t.Fatalf("首个 Tool 参数增量错误: %#v", emitter.events[3])
	}
	second, ok := emitter.events[4].(normalize.ToolCallArgumentsDeltaEvent)
	if !ok || second.Delta != "北京\"}" {
		t.Fatalf("第二个 Tool 参数增量错误: %#v", emitter.events[4])
	}
	end, ok := emitter.events[6].(normalize.ResponseEndEvent)
	if !ok || end.FinishReason != normalize.FinishReasonToolCalls {
		t.Fatalf("Tool 流终态错误: %#v", emitter.events[6])
	}
}

func TestAdapterParseStreamRejectsTruncatedResponse(t *testing.T) {
	value, _ := openaiadapter.New("openai-compatible")
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chat_1\",\"model\":\"upstream\",\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"))}
	if err := value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "p"}, response, &eventCollector{}); err == nil {
		t.Fatal("截断流不应成功")
	}
}

type eventCollector struct{ events []normalize.NormalizedEvent }

func (value *eventCollector) Emit(_ context.Context, event normalize.NormalizedEvent) error {
	value.events = append(value.events, event)
	return nil
}
