package anthropic_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/adapter/anthropic"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
)

func TestAdapterBuildsMessagesRequestAndParsesResponse(t *testing.T) {
	value, err := anthropic.New("anthropic-compatible")
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := value.BuildRequest(context.Background(), routing.RoutePlan{ProviderID: "provider_1", BaseURL: "https://provider.example/api", UpstreamModelID: "claude-model"}, normalize.NormalizedRequest{Model: "bundle/model", MaxOutputTokens: int64ptr(128), Stream: true, System: []normalize.TextPart{{Text: "规则"}}, Messages: []normalize.Message{{Role: normalize.RoleUser, Parts: []normalize.ContentPart{normalize.TextPart{Text: "你好"}}}}}, adapter.Credential{AuthType: provider.AuthTypeAPIKey, Secret: credential.SecretValue{Bytes: []byte("test-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, upstream.Body)
	if upstream.URL.Path != "/v1/messages" || upstream.Header.Get("X-API-Key") != "test-secret" || upstream.Header.Get("Anthropic-Version") != "2023-06-01" || !strings.Contains(body, `"max_tokens":128`) || !strings.Contains(body, `"stream":true`) {
		t.Fatalf("请求错误 url=%s headers=%v body=%s", upstream.URL, upstream.Header, body)
	}
	response, err := value.ParseResponse(context.Background(), routing.RoutePlan{ProviderID: "provider_1"}, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"claude-model","content":[{"type":"text","text":"完成"},{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"a.txt"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":2}}`))})
	if err != nil || response.FinishReason != normalize.FinishReasonToolCalls || len(response.Parts) != 2 {
		t.Fatalf("响应错误 result=%#v err=%v", response, err)
	}
}

func int64ptr(value int64) *int64 { return &value }
func readBody(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseConfigRejectsSecretsAndUnsafePaths(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"api_key":"must-not-store"}`),
		[]byte(`{"messages_path":"https://provider.example/v1/messages"}`),
		[]byte(`{"anthropic_version":"unknown"}`),
		[]byte(`{} {}`),
	} {
		if _, err := anthropic.ParseConfig(raw); err == nil {
			t.Fatalf("配置未被拒绝：%s", raw)
		}
	}
}

func TestAdapterRejectsMissingMaxTokens(t *testing.T) {
	value, err := anthropic.New("anthropic-compatible")
	if err != nil {
		t.Fatal(err)
	}
	_, err = value.BuildRequest(context.Background(), routing.RoutePlan{ProviderID: "provider_1", BaseURL: "https://provider.example", UpstreamModelID: "claude-model"}, normalize.NormalizedRequest{Model: "bundle/model", Messages: []normalize.Message{{Role: normalize.RoleUser, Parts: []normalize.ContentPart{normalize.TextPart{Text: "你好"}}}}}, adapter.Credential{AuthType: provider.AuthTypeAPIKey, Secret: credential.SecretValue{Bytes: []byte("test-secret")}})
	if err == nil {
		t.Fatal("缺少 max_tokens 的 Anthropic 请求不应被接受")
	}
}
