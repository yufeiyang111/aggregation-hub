package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/adapter"
	openaiadapter "aggregationhub.local/core/internal/adapter/openai"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
)

func TestAdapterBuildRequestUsesUpstreamModelAndProtectedAuthentication(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatalf("创建 Adapter 失败: %v", err)
	}
	temperature := 0.3
	request, err := value.BuildRequest(context.Background(), routing.RoutePlan{ProviderID: "provider_1", AdapterType: "openai-compatible", BaseURL: "https://provider.example/api", UpstreamModelID: "gpt-upstream"}, normalize.NormalizedRequest{
		Model: "provider_1/public-name", System: []normalize.TextPart{{Text: "遵守规则"}}, Messages: []normalize.Message{{Role: normalize.RoleUser, Parts: []normalize.ContentPart{normalize.TextPart{Text: "你好"}}}}, Temperature: &temperature,
	}, adapter.Credential{AuthType: provider.AuthTypeAPIKey, Secret: credential.SecretValue{Bytes: []byte("test-key")}})
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if request.URL.String() != "https://provider.example/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer test-key" || request.Header.Get("X-API-Key") != "" {
		t.Fatalf("上游请求不正确: url=%s headers=%v", request.URL, request.Header)
	}
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "provider_1/public-name") || !strings.Contains(string(contents), `"model":"gpt-upstream"`) {
		t.Fatalf("请求未使用上游模型 ID: %s", contents)
	}
}

func TestAdapterParseResponseMapsTextToolsUsageAndSafeErrors(t *testing.T) {
	value, err := openaiadapter.New("openai-compatible")
	if err != nil {
		t.Fatalf("创建 Adapter 失败: %v", err)
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"gpt-upstream","choices":[{"message":{"content":"完成","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"x\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":5}}`))}
	parsed, err := value.ParseResponse(context.Background(), routing.RoutePlan{ProviderID: "provider_1"}, response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if parsed.ID != "chatcmpl_1" || parsed.Usage == nil || parsed.Usage.InputTokens == nil || *parsed.Usage.InputTokens != 3 || parsed.FinishReason != normalize.FinishReasonToolCalls || len(parsed.Parts) != 2 {
		t.Fatalf("归一化响应错误: %+v", parsed)
	}
	if _, ok := parsed.Parts[1].(normalize.ToolCallPart); !ok {
		t.Fatalf("Tool Call 未保留: %#v", parsed.Parts[1])
	}

	failed := &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<html>private upstream detail</html>"))}
	_, err = value.ParseResponse(context.Background(), routing.RoutePlan{ProviderID: "provider_1"}, failed)
	var gateway *adapter.GatewayError
	if !errors.As(err, &gateway) || gateway.Code != "upstream_auth_failed" || gateway.SafeMessage == "<html>private upstream detail</html>" {
		t.Fatalf("上游错误未安全映射: %#v", err)
	}
}

func TestAdapterDiscoverModelsUsesCredentialAndParsesModelList(t *testing.T) {
	value, err := openaiadapter.New("local-openai-compatible")
	if err != nil {
		t.Fatalf("创建 Adapter 失败: %v", err)
	}
	client := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "http://127.0.0.1:11434/v1/models" || request.Header.Get("X-API-Key") != "token" {
			t.Fatalf("模型发现请求错误: %s %v", request.URL, request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"model-a"},{"id":"model-b","owned_by":"local"}]}`))}, nil
	})
	result, err := value.DiscoverModels(context.Background(), client, adapter.ProviderRuntime{ID: "p", Slug: "local", BaseURL: mustURL(t, "http://127.0.0.1:11434"), AuthType: provider.AuthTypeAPIKey, AdapterConfig: json.RawMessage(`{"auth_header_mode":"x_api_key"}`)}, adapter.Credential{AuthType: provider.AuthTypeAPIKey, Secret: credential.SecretValue{Bytes: []byte("token")}})
	if err != nil || len(result) != 2 || result[0].UpstreamModelID != "model-a" {
		t.Fatalf("模型发现错误: %+v err=%v", result, err)
	}
	if !result[0].Capabilities.Streaming || !result[0].Capabilities.Tools || result[0].CapabilitySource != "adapter_openai_chat_default" {
		t.Fatalf("OpenAI Chat 默认能力声明错误: %+v", result[0])
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (value roundTripperFunc) Do(request *http.Request) (*http.Response, error) {
	return value(request)
}
func mustURL(t *testing.T, raw string) url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return *parsed
}
