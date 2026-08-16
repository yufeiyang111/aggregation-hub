package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/transport"
)

const maxResponseBytes int64 = 1024 * 1024

var ErrUnsupportedWireAPI = errors.New("当前 Adapter 尚不支持所选 wire API")

type Adapter struct{ kind string }

func New(kind string) (*Adapter, error) {
	if kind != "openai-compatible" && kind != "local-openai-compatible" {
		return nil, adapter.ErrInvalidAdapterType
	}
	return &Adapter{kind: kind}, nil
}

func (value *Adapter) Type() string { return value.kind }
func (*Adapter) Metadata() adapter.Metadata {
	return adapter.Metadata{
		SupportedAuthTypes: []provider.AuthType{provider.AuthTypeAPIKey, provider.AuthTypeBearerToken, provider.AuthTypeOAuth, provider.AuthTypeNone},
		IngressProtocols:   []adapter.IngressProtocol{adapter.IngressOpenAIChat, adapter.IngressOpenAIResponses},
		Capabilities:       provider.Capabilities{Streaming: true, Tools: true},
		ProtectedHeaders:   []string{"Authorization", "X-API-Key"},
		SupportsDiscovery:  true,
	}
}
func (*Adapter) ConfigSchema() json.RawMessage            { return Schema() }
func (*Adapter) ValidateConfig(raw json.RawMessage) error { _, err := ParseConfig(raw); return err }

func (value *Adapter) DiscoverModels(ctx context.Context, client adapter.UpstreamClient, runtime adapter.ProviderRuntime, credential adapter.Credential) ([]adapter.DiscoveredModel, error) {
	if ctx == nil || client == nil {
		return nil, adapterError("invalid_request", "模型发现请求无效", http.StatusBadRequest, false, runtime.ID, nil)
	}
	config, err := ParseConfig(runtime.AdapterConfig)
	if err != nil {
		return nil, adapterError("invalid_provider_config", "服务配置无效", http.StatusBadRequest, false, runtime.ID, err)
	}
	target, err := resolveURL(runtime.BaseURL, config.ModelsPath)
	if err != nil {
		return nil, adapterError("invalid_provider_config", "服务地址无效", http.StatusBadRequest, false, runtime.ID, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, adapterError("invalid_request", "无法创建模型发现请求", http.StatusBadRequest, false, runtime.ID, err)
	}
	if err := applyCredential(request, credential, config.AuthHeaderMode); err != nil {
		return nil, adapterError("credential_unavailable", "服务凭据无效", http.StatusBadRequest, false, runtime.ID, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, adapterError("upstream_unavailable", "无法连接上游服务", http.StatusBadGateway, true, runtime.ID, err)
	}
	if response == nil {
		return nil, adapterError("upstream_invalid_response", "上游服务返回无效响应", http.StatusBadGateway, true, runtime.ID, nil)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, gatewayFromResponse(runtime.ID, response)
	}
	defer response.Body.Close()
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&body); err != nil {
		return nil, adapterError("upstream_invalid_response", "上游模型列表格式无效", http.StatusBadGateway, true, runtime.ID, err)
	}
	models := make([]adapter.DiscoveredModel, 0, len(body.Data))
	seen := make(map[string]struct{}, len(body.Data))
	for _, item := range body.Data {
		if !validModelID(item.ID) {
			return nil, adapterError("upstream_invalid_response", "上游模型标识无效", http.StatusBadGateway, false, runtime.ID, nil)
		}
		if _, exists := seen[item.ID]; exists {
			return nil, adapterError("upstream_invalid_response", "上游模型列表包含重复标识", http.StatusBadGateway, false, runtime.ID, nil)
		}
		seen[item.ID] = struct{}{}
		models = append(models, adapter.DiscoveredModel{UpstreamModelID: item.ID, DisplayName: item.ID, Source: provider.ModelSourceUpstream, Capabilities: value.Metadata().Capabilities, CapabilitySource: "adapter_openai_chat_default"})
	}
	return models, nil
}

func (value *Adapter) BuildRequest(ctx context.Context, route routing.RoutePlan, request normalize.NormalizedRequest, credential adapter.Credential) (*http.Request, error) {
	if ctx == nil || strings.TrimSpace(route.ProviderID) == "" || strings.TrimSpace(route.UpstreamModelID) == "" {
		return nil, adapterError("invalid_request", "代理请求无效", http.StatusBadRequest, false, route.ProviderID, nil)
	}
	config, err := ParseConfig(route.AdapterConfigJSON)
	if err != nil {
		return nil, adapterError("invalid_provider_config", "服务配置无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	if config.WireAPI == WireAPIResponses {
		target, err := resolveRouteURL(route.BaseURL, config.ResponsesPath)
		if err != nil {
			return nil, adapterError("invalid_provider_config", "服务地址无效", http.StatusBadRequest, false, route.ProviderID, err)
		}
		body, err := buildResponsesBody(route.UpstreamModelID, request)
		if err != nil {
			return nil, adapterError("unsupported_feature", "请求包含当前上游不支持的能力", http.StatusBadRequest, false, route.ProviderID, err)
		}
		return buildJSONRequest(ctx, target, body, credential, config.AuthHeaderMode, route.ProviderID)
	}
	target, err := resolveRouteURL(route.BaseURL, config.ChatCompletionsPath)
	if err != nil {
		return nil, adapterError("invalid_provider_config", "服务地址无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	body, err := buildChatBody(route.UpstreamModelID, request)
	if err != nil {
		return nil, adapterError("unsupported_feature", "请求包含当前上游不支持的能力", http.StatusBadRequest, false, route.ProviderID, err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, adapterError("invalid_request", "请求编码失败", http.StatusBadRequest, false, route.ProviderID, err)
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(string(encoded)))
	if err != nil {
		return nil, adapterError("invalid_request", "无法创建上游请求", http.StatusBadRequest, false, route.ProviderID, err)
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "application/json, text/event-stream")
	if err := applyCredential(upstream, credential, config.AuthHeaderMode); err != nil {
		return nil, adapterError("credential_unavailable", "服务凭据无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	return upstream, nil
}

func buildJSONRequest(ctx context.Context, target *url.URL, body any, credential adapter.Credential, authMode AuthHeaderMode, providerID string) (*http.Request, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, adapterError("invalid_request", "请求编码失败", http.StatusBadRequest, false, providerID, err)
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(string(encoded)))
	if err != nil {
		return nil, adapterError("invalid_request", "无法创建上游请求", http.StatusBadRequest, false, providerID, err)
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "application/json")
	if err := applyCredential(upstream, credential, authMode); err != nil {
		return nil, adapterError("credential_unavailable", "服务凭据无效", http.StatusBadRequest, false, providerID, err)
	}
	return upstream, nil
}

func (value *Adapter) ParseResponse(ctx context.Context, route routing.RoutePlan, response *http.Response) (normalize.NormalizedResponse, error) {
	if ctx == nil || response == nil {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游服务返回无效响应", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	config, err := ParseConfig(route.AdapterConfigJSON)
	if err != nil {
		return normalize.NormalizedResponse{}, adapterError("invalid_provider_config", "服务配置无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	if config.WireAPI == WireAPIResponses {
		return value.parseResponsesResponse(route, response)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return normalize.NormalizedResponse{}, gatewayFromResponse(route.ProviderID, response)
	}
	defer response.Body.Close()
	var body chatResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&body); err != nil {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游响应格式无效", http.StatusBadGateway, true, route.ProviderID, err)
	}
	if body.ID == "" || len(body.Choices) == 0 {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游响应缺少结果", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	choice := body.Choices[0]
	parts := make([]normalize.ContentPart, 0, 1+len(choice.Message.ToolCalls))
	if choice.Message.Content != "" {
		parts = append(parts, normalize.TextPart{Text: choice.Message.Content})
	}
	for _, call := range choice.Message.ToolCalls {
		if !validModelID(call.ID) || !validModelID(call.Function.Name) || !json.Valid([]byte(call.Function.Arguments)) {
			return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游 Tool Call 格式无效", http.StatusBadGateway, true, route.ProviderID, nil)
		}
		parts = append(parts, normalize.ToolCallPart{CallID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	usage := normalizeUsage(body.Usage)
	return normalize.NormalizedResponse{ID: body.ID, Model: body.Model, Parts: parts, Usage: usage, FinishReason: mapFinishReason(choice.FinishReason)}, nil
}

func (value *Adapter) Test(ctx context.Context, client adapter.UpstreamClient, runtime adapter.ProviderRuntime, credential adapter.Credential, kind adapter.CapabilityTestKind) adapter.CapabilityTestResult {
	if kind != adapter.CapabilityTestConnection && kind != adapter.CapabilityTestModels {
		return adapter.CapabilityTestResult{Code: "unsupported_feature", Message: "该测试类型尚未支持"}
	}
	_, err := value.DiscoverModels(ctx, client, runtime, credential)
	if err == nil {
		return adapter.CapabilityTestResult{Success: true, Code: "ok", Message: "服务连接正常，已读取模型列表", HTTPStatus: http.StatusOK}
	}
	var gateway *adapter.GatewayError
	if errors.As(err, &gateway) {
		return adapter.CapabilityTestResult{Code: gateway.Code, Message: gateway.SafeMessage, HTTPStatus: gateway.HTTPStatus, Retryable: gateway.Retryable}
	}
	return adapter.CapabilityTestResult{Code: "upstream_unavailable", Message: "无法连接上游服务", HTTPStatus: http.StatusBadGateway, Retryable: true}
}

func resolveRouteURL(raw, path string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	return resolveURL(*parsed, path)
}
func resolveURL(base url.URL, path string) (*url.URL, error) {
	if !base.IsAbs() || base.Host == "" {
		return nil, errors.New("Base URL 无效")
	}
	target := base.ResolveReference(&url.URL{Path: path})
	return target, nil
}
func applyCredential(request *http.Request, credential adapter.Credential, mode AuthHeaderMode) error {
	if request == nil {
		return errors.New("请求为空")
	}
	request.Header.Del("Authorization")
	request.Header.Del("X-API-Key")
	switch credential.AuthType {
	case provider.AuthTypeNone:
		return nil
	case provider.AuthTypeAPIKey:
		if len(credential.Secret.Bytes) == 0 {
			return errors.New("API Key 为空")
		}
		if mode == AuthHeaderXAPIKey {
			request.Header.Set("X-API-Key", string(credential.Secret.Bytes))
		} else {
			request.Header.Set("Authorization", "Bearer "+string(credential.Secret.Bytes))
		}
		return nil
	case provider.AuthTypeBearerToken, provider.AuthTypeOAuth:
		if len(credential.Secret.Bytes) == 0 {
			return errors.New("Bearer Token 为空")
		}
		request.Header.Set("Authorization", "Bearer "+string(credential.Secret.Bytes))
		return nil
	default:
		return errors.New("认证类型无效")
	}
}
func gatewayFromResponse(providerID string, response *http.Response) error {
	summary, _ := transport.ReadErrorSummary(response.Body, response.Header.Get("Content-Type"), 16*1024)
	code, message, status, retry := "upstream_error", "上游服务请求失败", http.StatusBadGateway, false
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		code, message, status = "upstream_auth_failed", "上游服务认证失败", http.StatusBadGateway
	case http.StatusTooManyRequests:
		code, message, status, retry = "upstream_rate_limited", "上游服务限流", http.StatusTooManyRequests, true
	default:
		if response.StatusCode >= 500 {
			code, message, status, retry = "upstream_unavailable", "上游服务暂时不可用", http.StatusBadGateway, true
		}
	}
	return adapterError(code, message, status, retry, providerID, errors.New(summary.ContentType))
}
func adapterError(code, message string, status int, retry bool, providerID string, cause error) *adapter.GatewayError {
	return &adapter.GatewayError{Code: code, SafeMessage: message, HTTPStatus: status, Retryable: retry, ProviderID: providerID, Cause: cause}
}
func validModelID(value string) bool {
	return value != "" && len(value) <= 304 && strings.TrimSpace(value) == value
}
