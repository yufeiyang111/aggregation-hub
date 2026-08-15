package anthropic

import (
	"bytes"
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

var ErrInvalidConfig = errors.New("Anthropic Compatible Adapter 配置无效")

type AuthHeaderMode string

const (
	AuthHeaderXAPIKey             AuthHeaderMode = "x_api_key"
	AuthHeaderAuthorizationBearer AuthHeaderMode = "authorization_bearer"
)

type Config struct {
	MessagesPath   string         `json:"messages_path"`
	APIVersion     string         `json:"anthropic_version"`
	AuthHeaderMode AuthHeaderMode `json:"auth_header_mode"`
}

func DefaultConfig() Config {
	return Config{MessagesPath: "/v1/messages", APIVersion: "2023-06-01", AuthHeaderMode: AuthHeaderXAPIKey}
}
func ParseConfig(raw json.RawMessage) (Config, error) {
	config := DefaultConfig()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		return config, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, ErrInvalidConfig
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, ErrInvalidConfig
	}
	if !validPath(config.MessagesPath) || config.APIVersion != "2023-06-01" || (config.AuthHeaderMode != AuthHeaderXAPIKey && config.AuthHeaderMode != AuthHeaderAuthorizationBearer) {
		return Config{}, ErrInvalidConfig
	}
	return config, nil
}
func validPath(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.Contains(value, "\\") && !parsed.IsAbs() && parsed.Host == "" && parsed.RawQuery == "" && parsed.Fragment == "" && !strings.Contains(parsed.Path, "..")
}
func Schema() json.RawMessage {
	return json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"properties":{"messages_path":{"type":"string","default":"/v1/messages"},"anthropic_version":{"const":"2023-06-01","default":"2023-06-01"},"auth_header_mode":{"enum":["x_api_key","authorization_bearer"],"default":"x_api_key"}}}`)
}

type Adapter struct{ kind string }

func New(kind string) (*Adapter, error) {
	if kind != "anthropic-compatible" {
		return nil, adapter.ErrInvalidAdapterType
	}
	return &Adapter{kind: kind}, nil
}
func (value *Adapter) Type() string { return value.kind }
func (*Adapter) Metadata() adapter.Metadata {
	return adapter.Metadata{SupportedAuthTypes: []provider.AuthType{provider.AuthTypeAPIKey, provider.AuthTypeBearerToken, provider.AuthTypeOAuth}, IngressProtocols: []adapter.IngressProtocol{adapter.IngressAnthropic, adapter.IngressOpenAIChat}, Capabilities: provider.Capabilities{Streaming: true, Tools: true}, ProtectedHeaders: []string{"Authorization", "X-API-Key", "Anthropic-Version", "Anthropic-Beta"}, SupportsDiscovery: false}
}
func (*Adapter) ConfigSchema() json.RawMessage            { return Schema() }
func (*Adapter) ValidateConfig(raw json.RawMessage) error { _, err := ParseConfig(raw); return err }
func (value *Adapter) DiscoverModels(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential) ([]adapter.DiscoveredModel, error) {
	return nil, adapterError("unsupported_feature", "该 Anthropic 兼容服务不支持自动模型发现", http.StatusBadRequest, false, "", nil)
}

func (value *Adapter) BuildRequest(ctx context.Context, route routing.RoutePlan, request normalize.NormalizedRequest, credential adapter.Credential) (*http.Request, error) {
	if ctx == nil || route.ProviderID == "" || route.UpstreamModelID == "" {
		return nil, adapterError("invalid_request", "代理请求无效", http.StatusBadRequest, false, route.ProviderID, nil)
	}
	config, err := ParseConfig(route.AdapterConfigJSON)
	if err != nil {
		return nil, adapterError("invalid_provider_config", "服务配置无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	body, err := buildBody(route.UpstreamModelID, request)
	if err != nil {
		return nil, adapterError("unsupported_feature", "请求包含当前上游不支持的能力", http.StatusBadRequest, false, route.ProviderID, err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, adapterError("invalid_request", "请求编码失败", http.StatusBadRequest, false, route.ProviderID, err)
	}
	target, err := resolveURL(route.BaseURL, config.MessagesPath)
	if err != nil {
		return nil, adapterError("invalid_provider_config", "服务地址无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, adapterError("invalid_request", "无法创建上游请求", http.StatusBadRequest, false, route.ProviderID, err)
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "application/json")
	upstream.Header.Set("Anthropic-Version", config.APIVersion)
	if err := applyCredential(upstream, credential, config.AuthHeaderMode); err != nil {
		return nil, adapterError("credential_unavailable", "服务凭据无效", http.StatusBadRequest, false, route.ProviderID, err)
	}
	return upstream, nil
}
func (value *Adapter) ParseResponse(ctx context.Context, route routing.RoutePlan, response *http.Response) (normalize.NormalizedResponse, error) {
	if ctx == nil || response == nil {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游服务返回无效响应", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return normalize.NormalizedResponse{}, gatewayFromResponse(route.ProviderID, response)
	}
	defer response.Body.Close()
	var body messagesResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&body); err != nil {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游响应格式无效", http.StatusBadGateway, true, route.ProviderID, err)
	}
	if body.ID == "" || body.Model == "" || len(body.Content) == 0 {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游响应缺少结果", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	parts := make([]normalize.ContentPart, 0, len(body.Content))
	for _, block := range body.Content {
		switch block.Type {
		case "text":
			if block.Text == "" {
				return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游文本块无效", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			parts = append(parts, normalize.TextPart{Text: block.Text})
		case "tool_use":
			if block.ID == "" || block.Name == "" || !validJSONObject(block.Input) {
				return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游 Tool Call 无效", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			parts = append(parts, normalize.ToolCallPart{CallID: block.ID, Name: block.Name, Arguments: string(block.Input)})
		default:
			return normalize.NormalizedResponse{}, adapterError("unsupported_feature", "上游响应包含当前未支持的内容块", http.StatusBadGateway, false, route.ProviderID, nil)
		}
	}
	return normalize.NormalizedResponse{ID: body.ID, Model: body.Model, Parts: parts, Usage: normalizeUsage(body.Usage), FinishReason: mapFinish(body.StopReason)}, nil
}
func (value *Adapter) ParseStream(ctx context.Context, route routing.RoutePlan, response *http.Response, emitter normalize.StreamEmitter) error {
	return parseAnthropicStream(ctx, route, response, emitter)
}
func (value *Adapter) Test(ctx context.Context, client adapter.UpstreamClient, runtime adapter.ProviderRuntime, credential adapter.Credential, kind adapter.CapabilityTestKind) adapter.CapabilityTestResult {
	return adapter.CapabilityTestResult{Code: "unsupported_feature", Message: "该测试类型尚未支持"}
}
func Register(registry *adapter.Registry) error {
	return registry.Register(func() adapter.Adapter { value, _ := New("anthropic-compatible"); return value })
}

type messagesRequest struct {
	Model         string      `json:"model"`
	MaxTokens     *int64      `json:"max_tokens"`
	Stream        bool        `json:"stream,omitempty"`
	System        []textBlock `json:"system,omitempty"`
	Messages      []message   `json:"messages"`
	Tools         []tool      `json:"tools,omitempty"`
	ToolChoice    any         `json:"tool_choice,omitempty"`
	Temperature   *float64    `json:"temperature,omitempty"`
	StopSequences []string    `json:"stop_sequences,omitempty"`
}
type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type message struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func buildBody(model string, input normalize.NormalizedRequest) (messagesRequest, error) {
	if input.MaxOutputTokens == nil || *input.MaxOutputTokens < 1 {
		return messagesRequest{}, errors.New("Anthropic 请求必须包含 max_tokens")
	}
	if input.ParallelToolCalls {
		return messagesRequest{}, errors.New("并行 Tool 尚未支持")
	}
	result := messagesRequest{Model: model, MaxTokens: input.MaxOutputTokens, Stream: input.Stream, Temperature: input.Temperature, StopSequences: input.StopSequences}
	for _, part := range input.System {
		result.System = append(result.System, textBlock{Type: "text", Text: part.Text})
	}
	for _, source := range input.Messages {
		mapped := message{Role: string(source.Role)}
		if source.Role == normalize.RoleTool {
			mapped.Role = "user"
		}
		for _, part := range source.Parts {
			switch typed := part.(type) {
			case normalize.TextPart:
				if source.Role == normalize.RoleTool {
					return messagesRequest{}, errors.New("Tool 消息不能包含文本")
				}
				mapped.Content = append(mapped.Content, textBlock{Type: "text", Text: typed.Text})
			case normalize.ToolCallPart:
				if source.Role != normalize.RoleAssistant {
					return messagesRequest{}, errors.New("Tool Call 角色无效")
				}
				mapped.Content = append(mapped.Content, struct {
					Type  string          `json:"type"`
					ID    string          `json:"id"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				}{"tool_use", typed.CallID, typed.Name, json.RawMessage(typed.Arguments)})
			case normalize.ToolResultPart:
				if source.Role != normalize.RoleTool {
					return messagesRequest{}, errors.New("Tool Result 角色无效")
				}
				mapped.Content = append(mapped.Content, struct {
					Type      string `json:"type"`
					ToolUseID string `json:"tool_use_id"`
					Content   string `json:"content"`
					IsError   bool   `json:"is_error,omitempty"`
				}{"tool_result", typed.CallID, typed.Content, typed.IsError})
			default:
				return messagesRequest{}, errors.New("内容类型未支持")
			}
		}
		result.Messages = append(result.Messages, mapped)
	}
	for _, item := range input.Tools {
		result.Tools = append(result.Tools, tool{Name: item.Name, Description: item.Description, InputSchema: item.InputSchema})
	}
	switch input.ToolChoice.Mode {
	case "", normalize.ToolChoiceAuto:
	case normalize.ToolChoiceRequired:
		result.ToolChoice = map[string]string{"type": "any"}
	case normalize.ToolChoiceNamed:
		result.ToolChoice = map[string]string{"type": "tool", "name": input.ToolChoice.Name}
	default:
		return messagesRequest{}, errors.New("Tool Choice 未支持")
	}
	return result, nil
}

type messagesResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	} `json:"usage"`
}

func normalizeUsage(value struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
}) *normalize.Usage {
	if value.InputTokens == nil && value.OutputTokens == nil {
		return nil
	}
	return &normalize.Usage{InputTokens: value.InputTokens, OutputTokens: value.OutputTokens, Source: normalize.UsageSourceUpstreamReported}
}
func mapFinish(value string) normalize.FinishReason {
	switch value {
	case "end_turn":
		return normalize.FinishReasonStop
	case "max_tokens":
		return normalize.FinishReasonLength
	case "tool_use":
		return normalize.FinishReasonToolCalls
	default:
		return normalize.FinishReasonError
	}
}
func resolveURL(baseURL, path string) (*url.URL, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	relative, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(relative), nil
}
func applyCredential(request *http.Request, credential adapter.Credential, mode AuthHeaderMode) error {
	if len(credential.Secret.Bytes) == 0 {
		return errors.New("凭据为空")
	}
	if credential.AuthType == provider.AuthTypeAPIKey && mode == AuthHeaderXAPIKey {
		request.Header.Set("X-API-Key", string(credential.Secret.Bytes))
		return nil
	}
	if credential.AuthType == provider.AuthTypeAPIKey || credential.AuthType == provider.AuthTypeBearerToken || credential.AuthType == provider.AuthTypeOAuth {
		request.Header.Set("Authorization", "Bearer "+string(credential.Secret.Bytes))
		return nil
	}
	return errors.New("认证类型无效")
}
func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 1 && trimmed[0] == '{' && json.Valid(trimmed)
}
func gatewayFromResponse(providerID string, response *http.Response) error {
	summary, _ := transport.ReadErrorSummary(response.Body, response.Header.Get("Content-Type"), 16*1024)
	code, msg, status, retry := "upstream_error", "上游服务请求失败", http.StatusBadGateway, false
	switch response.StatusCode {
	case 401, 403:
		code, msg = "upstream_auth_failed", "上游服务认证失败"
	case 429:
		code, msg, status, retry = "upstream_rate_limited", "上游服务限流", http.StatusTooManyRequests, true
	default:
		if response.StatusCode >= 500 {
			code, msg, retry = "upstream_unavailable", "上游服务暂时不可用", true
		}
	}
	return adapterError(code, msg, status, retry, providerID, errors.New(summary.ContentType))
}
func adapterError(code, msg string, status int, retry bool, providerID string, cause error) *adapter.GatewayError {
	return &adapter.GatewayError{Code: code, SafeMessage: msg, HTTPStatus: status, Retryable: retry, ProviderID: providerID, Cause: cause}
}
