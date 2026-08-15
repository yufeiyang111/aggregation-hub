package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/transport"
)

var (
	ErrInvalidAdapterType   = errors.New("Adapter 类型无效")
	ErrDuplicateAdapterType = errors.New("Adapter 类型已注册")
	ErrAdapterNotFound      = errors.New("Adapter 类型不存在")
)

// UpstreamClient 由受网络策略保护的 Transport 实现，Adapter 不得自行创建绕过策略的客户端。
type UpstreamClient = transport.UpstreamClient

type IngressProtocol string

const (
	IngressOpenAIChat      IngressProtocol = "openai_chat_completions"
	IngressAnthropic       IngressProtocol = "anthropic_messages"
	IngressOpenAIResponses IngressProtocol = "openai_responses"
)

// Metadata 说明 Adapter 的非秘密能力与受保护 Header，供控制面、路由和 UI 读取。
type Metadata struct {
	SupportedAuthTypes []provider.AuthType
	IngressProtocols   []IngressProtocol
	Capabilities       provider.Capabilities
	ProtectedHeaders   []string
	SupportsDiscovery  bool
}

// ProviderRuntime 只包含已验证的非秘密 Provider 配置。
type ProviderRuntime struct {
	ID            string
	Slug          string
	BaseURL       url.URL
	AuthType      provider.AuthType
	AdapterConfig json.RawMessage
	Timeout       time.Duration
}

// Credential 是短生命周期的内存副本，Adapter 不直接访问 CredentialStore。
type Credential struct {
	AuthType provider.AuthType
	Secret   credential.SecretValue
}

type DiscoveredModel = provider.SyncedModel

type CapabilityTestKind string

const (
	CapabilityTestConnection CapabilityTestKind = "connection"
	CapabilityTestModels     CapabilityTestKind = "models"
	CapabilityTestChat       CapabilityTestKind = "chat"
)

// CapabilityTestResult 仅包含可安全展示的测试结果，禁止携带原始上游错误体或秘密。
type CapabilityTestResult struct {
	Success    bool   `json:"success"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status"`
	Retryable  bool   `json:"retryable"`
}

// GatewayError 保留内部原因，但对入口层只暴露结构化、安全错误信息。
type GatewayError struct {
	Code         string
	SafeMessage  string
	HTTPStatus   int
	Retryable    bool
	ProviderID   string
	UpstreamCode string
	Cause        error
}

func (value *GatewayError) Error() string {
	if value == nil {
		return "Gateway 错误"
	}
	if value.Code == "" {
		return "Gateway 错误"
	}
	return fmt.Sprintf("Gateway 错误: %s", value.Code)
}
func (value *GatewayError) Unwrap() error {
	if value == nil {
		return nil
	}
	return value.Cause
}

// Adapter 是 Provider 协议差异的唯一扩展边界。
type Adapter interface {
	Type() string
	Metadata() Metadata
	ConfigSchema() json.RawMessage
	ValidateConfig(json.RawMessage) error
	DiscoverModels(context.Context, UpstreamClient, ProviderRuntime, Credential) ([]DiscoveredModel, error)
	BuildRequest(context.Context, routing.RoutePlan, normalize.NormalizedRequest, Credential) (*http.Request, error)
	ParseResponse(context.Context, routing.RoutePlan, *http.Response) (normalize.NormalizedResponse, error)
	ParseStream(context.Context, routing.RoutePlan, *http.Response, normalize.StreamEmitter) error
	Test(context.Context, UpstreamClient, ProviderRuntime, Credential, CapabilityTestKind) CapabilityTestResult
}
