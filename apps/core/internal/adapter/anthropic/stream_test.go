package anthropic_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/adapter/anthropic"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
)

func TestAdapterParsesFragmentedAnthropicMessagesStream(t *testing.T) {
	value, err := anthropic.New("anthropic-compatible")
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Join([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-model\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
		"event: future_event\ndata: {\"type\":\"future_event\"}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"你好\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read_file\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"a.txt\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":2}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")
	emitter := &recordingEmitter{}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(&fragmentedReader{data: []byte(payload), size: 7})}

	if err := value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "provider_1"}, response, emitter); err != nil {
		t.Fatalf("解析流式响应失败: %v", err)
	}
	if len(emitter.events) != 10 {
		t.Fatalf("事件数量错误: %d", len(emitter.events))
	}
	if start, ok := emitter.events[0].(normalize.ResponseStartEvent); !ok || start.ResponseID != "msg_1" || start.Model != "claude-model" {
		t.Fatalf("message_start 转换错误: %#v", emitter.events[0])
	}
	if delta, ok := emitter.events[2].(normalize.TextDeltaEvent); !ok || delta.Text != "你好" {
		t.Fatalf("text_delta 转换错误: %#v", emitter.events[2])
	}
	if call, ok := emitter.events[5].(normalize.ToolCallStartEvent); !ok || call.CallID != "toolu_1" || call.Name != "read_file" {
		t.Fatalf("tool_use 转换错误: %#v", emitter.events[5])
	}
	if delta, ok := emitter.events[6].(normalize.ToolCallArgumentsDeltaEvent); !ok || delta.Delta != `{"path":"a.txt"}` {
		t.Fatalf("input_json_delta 转换错误: %#v", emitter.events[6])
	}
	if usage, ok := emitter.events[8].(normalize.UsageUpdateEvent); !ok || usage.Usage.InputTokens == nil || *usage.Usage.InputTokens != 10 || usage.Usage.OutputTokens == nil || *usage.Usage.OutputTokens != 2 {
		t.Fatalf("usage 转换错误: %#v", emitter.events[8])
	}
	if end, ok := emitter.events[9].(normalize.ResponseEndEvent); !ok || end.FinishReason != normalize.FinishReasonToolCalls {
		t.Fatalf("message_stop 转换错误: %#v", emitter.events[9])
	}
}

func TestAdapterRejectsTruncatedAnthropicStream(t *testing.T) {
	value, err := anthropic.New("anthropic-compatible")
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-model\"}}\n\n"))}
	err = value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "provider_1"}, response, &recordingEmitter{})
	var gatewayErr *adapter.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "upstream_stream_truncated" {
		t.Fatalf("截断流未返回预期错误: err=%v", err)
	}
}

func TestAdapterRejectsOutOfOrderAnthropicContentBlock(t *testing.T) {
	value, err := anthropic.New("anthropic-compatible")
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Join([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-model\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
	}, "")
	err = value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "provider_1"}, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, &recordingEmitter{})
	var gatewayErr *adapter.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "upstream_stream_invalid" {
		t.Fatalf("乱序内容块未返回预期错误: err=%v", err)
	}
}

type recordingEmitter struct{ events []normalize.NormalizedEvent }

func (value *recordingEmitter) Emit(_ context.Context, event normalize.NormalizedEvent) error {
	value.events = append(value.events, event)
	return nil
}

type fragmentedReader struct {
	data []byte
	size int
}

func (value *fragmentedReader) Read(target []byte) (int, error) {
	if len(value.data) == 0 {
		return 0, io.EOF
	}
	count := value.size
	if count > len(value.data) {
		count = len(value.data)
	}
	if count > len(target) {
		count = len(target)
	}
	copy(target, value.data[:count])
	value.data = value.data[count:]
	return count, nil
}
