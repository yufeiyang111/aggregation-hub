package openai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/adapter"
	openaiadapter "aggregationhub.local/core/internal/adapter/openai"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
)

func TestAdapterParsesResponsesStreamFixtures(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatal(err)
	}
	readFixture := func(name string) []byte {
		t.Helper()
		body, readErr := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "openai", "responses", "stream", name))
		if readErr != nil {
			t.Fatalf("读取 Responses stream fixture 失败: %v", readErr)
		}
		return body
	}
	emitter := &responsesRecordingEmitter{}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(&responsesFragmentedReader{data: readFixture("happy_path.sse"), size: 11})}
	err = value.ParseStream(context.Background(), routing.RoutePlan{ProviderID: "provider_stream", AdapterConfigJSON: []byte(`{"wire_api":"responses"}`)}, response, emitter)
	if err != nil {
		t.Fatalf("Responses stream 解析失败: %v", err)
	}
	if len(emitter.events) != 10 {
		t.Fatalf("Responses stream 事件数量错误: %d", len(emitter.events))
	}
	if start, ok := emitter.events[0].(normalize.ResponseStartEvent); !ok || start.ResponseID != "resp_stream_1" || start.Model != "gpt-stream" {
		t.Fatalf("response.created 转换错误: %#v", emitter.events[0])
	}
	if text, ok := emitter.events[2].(normalize.TextDeltaEvent); !ok || text.Text != "你好" {
		t.Fatalf("response.output_text.delta 转换错误: %#v", emitter.events[2])
	}
	if tool, ok := emitter.events[5].(normalize.ToolCallStartEvent); !ok || tool.CallID != "call_1" || tool.Name != "lookup" {
		t.Fatalf("Function Call 转换错误: %#v", emitter.events[5])
	}
	if arguments, ok := emitter.events[6].(normalize.ToolCallArgumentsDeltaEvent); !ok || arguments.Delta != `{"query":"fixture"}` {
		t.Fatalf("Function arguments delta 转换错误: %#v", emitter.events[6])
	}
	if usage, ok := emitter.events[8].(normalize.UsageUpdateEvent); !ok || usage.Usage.InputTokens == nil || *usage.Usage.InputTokens != 9 || usage.Usage.OutputTokens == nil || *usage.Usage.OutputTokens != 4 {
		t.Fatalf("Responses usage 转换错误: %#v", emitter.events[8])
	}
	if end, ok := emitter.events[9].(normalize.ResponseEndEvent); !ok || end.FinishReason != normalize.FinishReasonStop {
		t.Fatalf("response.completed 转换错误: %#v", emitter.events[9])
	}
}

func TestAdapterMapsResponsesStreamErrorAndRejectsInvalidTerminal(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatal(err)
	}
	readFixture := func(name string) []byte {
		t.Helper()
		body, readErr := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "openai", "responses", "stream", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		return body
	}
	route := routing.RoutePlan{ProviderID: "provider_stream", AdapterConfigJSON: []byte(`{"wire_api":"responses"}`)}
	emitter := &responsesRecordingEmitter{}
	err = value.ParseStream(context.Background(), route, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(readFixture("error.sse"))))}, emitter)
	if err != nil || len(emitter.events) != 2 {
		t.Fatalf("Responses error stream 解析错误: events=%#v err=%v", emitter.events, err)
	}
	if event, ok := emitter.events[1].(normalize.ErrorEvent); !ok || event.Code != "server_error" || strings.Contains(event.Message, "private upstream detail") {
		t.Fatalf("Responses error 未安全转换: %#v", emitter.events[1])
	}

	err = value.ParseStream(context.Background(), route, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(readFixture("truncated.sse"))))}, &responsesRecordingEmitter{})
	var gatewayErr *adapter.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "upstream_stream_truncated" {
		t.Fatalf("截断 Responses stream 未被拒绝: %v", err)
	}

	err = value.ParseStream(context.Background(), route, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(readFixture("reasoning_unsupported.sse"))))}, &responsesRecordingEmitter{})
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "unsupported_feature" {
		t.Fatalf("Reasoning stream 未被结构化拒绝: %v", err)
	}

	incomplete := &responsesRecordingEmitter{}
	err = value.ParseStream(context.Background(), route, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(readFixture("incomplete.sse"))))}, incomplete)
	if err != nil || len(incomplete.events) != 3 {
		t.Fatalf("incomplete stream 解析错误: events=%#v err=%v", incomplete.events, err)
	}
	if end, ok := incomplete.events[2].(normalize.ResponseEndEvent); !ok || end.FinishReason != normalize.FinishReasonLength {
		t.Fatalf("response.incomplete 转换错误: %#v", incomplete.events[2])
	}
}

func TestAdapterCancelsResponsesStreamAfterClientStopsReading(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "openai", "responses", "stream", "happy_path.sse"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	emitter := &cancelAfterFirstEmitter{cancel: cancel}
	err = value.ParseStream(ctx, routing.RoutePlan{ProviderID: "provider_stream", AdapterConfigJSON: []byte(`{"wire_api":"responses"}`)}, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, emitter)
	if !errors.Is(err, context.Canceled) || emitter.count != 1 {
		t.Fatalf("取消未传播到 Responses stream: count=%d err=%v", emitter.count, err)
	}
}

type responsesRecordingEmitter struct{ events []normalize.NormalizedEvent }

func (value *responsesRecordingEmitter) Emit(_ context.Context, event normalize.NormalizedEvent) error {
	value.events = append(value.events, event)
	return nil
}

type cancelAfterFirstEmitter struct {
	count  int
	cancel context.CancelFunc
}

func (value *cancelAfterFirstEmitter) Emit(_ context.Context, _ normalize.NormalizedEvent) error {
	value.count++
	if value.count == 1 {
		value.cancel()
	}
	return nil
}

type responsesFragmentedReader struct {
	data []byte
	size int
}

func (value *responsesFragmentedReader) Read(target []byte) (int, error) {
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
