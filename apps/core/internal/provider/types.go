package provider

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"aggregationhub.local/core/internal/credential"
)

var (
	ErrInvalidProvider       = errors.New("Provider 输入无效")
	ErrProviderNotFound      = errors.New("Provider 不存在")
	ErrDuplicateProvider     = errors.New("Provider slug 已存在")
	ErrStaleResource         = errors.New("资源版本已过期")
	ErrOAuthNotConfigured    = errors.New("OAuth Provider 尚未配置账户")
	ErrInvalidModel          = errors.New("模型输入无效")
	ErrModelNotFound         = errors.New("模型不存在")
	ErrCredentialCleanup     = errors.New("凭据清理失败")
	ErrUnsupportedPagination = errors.New("分页参数无效")
)

type AuthType string

const (
	AuthTypeAPIKey      AuthType = "api_key"
	AuthTypeBearerToken AuthType = "bearer_token"
	AuthTypeOAuth       AuthType = "oauth"
	AuthTypeNone        AuthType = "none"
)

type ProviderStatus string

const (
	ProviderStatusDraft        ProviderStatus = "draft"
	ProviderStatusEnabled      ProviderStatus = "enabled"
	ProviderStatusDegraded     ProviderStatus = "degraded"
	ProviderStatusAuthRequired ProviderStatus = "auth_required"
	ProviderStatusDisabled     ProviderStatus = "disabled"
	ProviderStatusDeleted      ProviderStatus = "deleted"
)

type ModelStatus string

const (
	ModelStatusAvailable       ModelStatus = "available"
	ModelStatusDegraded        ModelStatus = "degraded"
	ModelStatusMissingUpstream ModelStatus = "missing_upstream"
	ModelStatusDisabled        ModelStatus = "disabled"
	ModelStatusDeleted         ModelStatus = "deleted"
)

type ModelSource string

const (
	ModelSourceUpstream       ModelSource = "upstream"
	ModelSourceAdapterDefault ModelSource = "adapter_default"
	ModelSourceManual         ModelSource = "manual"
	ModelSourceOAuth          ModelSource = "oauth"
)

type Capabilities struct {
	Streaming     bool
	Tools         bool
	ParallelTools bool
	Reasoning     bool
	Thinking      bool
	Vision        bool
}

type Provider struct {
	ID                string
	Slug              string
	Name              string
	AdapterType       string
	AuthType          AuthType
	BaseURL           string
	CredentialRef     *credential.Ref
	LifecycleStatus   ProviderStatus
	Enabled           bool
	Timeout           time.Duration
	AdapterConfigJSON json.RawMessage
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type ProviderModel struct {
	ID                     string
	ProviderID             string
	UpstreamModelID        string
	PublicModelID          string
	DisplayName            string
	Source                 ModelSource
	LifecycleStatus        ModelStatus
	Enabled                bool
	Capabilities           Capabilities
	ContextWindowTokens    *int64
	MaxOutputTokens        *int64
	CapabilitySource       string
	CapabilityOverrideJSON json.RawMessage
	Version                int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
	DeletedAt              *time.Time
}

type SyncedModel struct {
	UpstreamModelID     string
	DisplayName         string
	Source              ModelSource
	Capabilities        Capabilities
	ContextWindowTokens *int64
	MaxOutputTokens     *int64
	CapabilitySource    string
}

type AuditEvent struct {
	ID         string
	EventType  string
	EntityType string
	EntityID   string
	DetailJSON json.RawMessage
	CreatedAt  time.Time
}

type ProviderPageQuery struct {
	Cursor   string
	PageSize int
}

type ProviderPage struct {
	Items      []Provider
	NextCursor string
}

type ProviderRepository interface {
	Create(context.Context, Provider, AuditEvent) error
	FindByID(context.Context, string) (Provider, error)
	FindBySlug(context.Context, string) (Provider, error)
	Update(context.Context, Provider, int64, AuditEvent) (Provider, error)
	SetEnabled(context.Context, string, int64, bool, AuditEvent) (Provider, error)
	SoftDelete(context.Context, string, int64, AuditEvent) error
	AppendAudit(context.Context, AuditEvent) error
	List(context.Context, ProviderPageQuery) (ProviderPage, error)
}

type ModelPageQuery struct {
	Cursor          string
	PageSize        int
	ProviderID      string
	LifecycleStatus ModelStatus
	Enabled         *bool
	Capability      string
	Search          string
}

type ModelPage struct {
	Items      []ProviderModel
	NextCursor string
}

type ModelRepository interface {
	FindByID(context.Context, string) (ProviderModel, error)
	FindByPublicID(context.Context, string) (ProviderModel, error)
	List(context.Context, ModelPageQuery) (ModelPage, error)
	SetEnabled(context.Context, string, int64, bool, AuditEvent) (ProviderModel, error)
	ReconcileSyncedModels(context.Context, string, string, []SyncedModel, time.Time) error
}

type CreateProviderInput struct {
	Slug              string
	Name              string
	AdapterType       string
	AuthType          AuthType
	BaseURL           string
	Timeout           time.Duration
	AdapterConfigJSON json.RawMessage
	Credential        *credential.SecretValue
}

type UpdateProviderInput struct {
	ExpectedVersion   int64
	Name              string
	BaseURL           string
	Timeout           time.Duration
	AdapterConfigJSON json.RawMessage
	Credential        *credential.SecretValue
}

type CredentialStateDTO struct {
	Configured bool   `json:"configured"`
	MaskedHint string `json:"masked_hint,omitempty"`
}

type AdapterConfigDTO struct {
	WireAPI        string `json:"wire_api"`
	AuthHeaderMode string `json:"auth_header_mode"`
}

// SanitizeAdapterConfig 只暴露当前 UI 需要的 allowlist 配置，避免 WebView 接收原始 Adapter JSON。
func SanitizeAdapterConfig(raw json.RawMessage) AdapterConfigDTO {
	result := AdapterConfigDTO{WireAPI: "chat_completions", AuthHeaderMode: "authorization_bearer"}
	var input struct {
		WireAPI        string `json:"wire_api"`
		AuthHeaderMode string `json:"auth_header_mode"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return result
	}
	if input.WireAPI == "chat_completions" || input.WireAPI == "responses" {
		result.WireAPI = input.WireAPI
	}
	if input.AuthHeaderMode == "authorization_bearer" || input.AuthHeaderMode == "x_api_key" {
		result.AuthHeaderMode = input.AuthHeaderMode
	}
	return result
}

type ProviderDTO struct {
	ID              string             `json:"id"`
	Slug            string             `json:"slug"`
	Name            string             `json:"name"`
	AdapterType     string             `json:"adapter_type"`
	AuthType        AuthType           `json:"auth_type"`
	BaseURL         string             `json:"base_url"`
	LifecycleStatus ProviderStatus     `json:"lifecycle_status"`
	Enabled         bool               `json:"enabled"`
	TimeoutMS       int64              `json:"timeout_ms"`
	AdapterConfig   AdapterConfigDTO   `json:"adapter_config"`
	Version         int64              `json:"version"`
	Credential      CredentialStateDTO `json:"credential"`
}

type ModelCapabilitiesDTO struct {
	Streaming     bool `json:"streaming"`
	Tools         bool `json:"tools"`
	ParallelTools bool `json:"parallel_tools"`
	Reasoning     bool `json:"reasoning"`
	Thinking      bool `json:"thinking"`
	Vision        bool `json:"vision"`
}

type ModelDTO struct {
	ID                     string               `json:"id"`
	ProviderID             string               `json:"provider_id"`
	UpstreamModelID        string               `json:"upstream_model_id"`
	PublicModelID          string               `json:"public_model_id"`
	DisplayName            string               `json:"display_name"`
	Source                 ModelSource          `json:"source"`
	LifecycleStatus        ModelStatus          `json:"lifecycle_status"`
	Enabled                bool                 `json:"enabled"`
	Capabilities           ModelCapabilitiesDTO `json:"capabilities"`
	ContextWindowTokens    *int64               `json:"context_window_tokens,omitempty"`
	MaxOutputTokens        *int64               `json:"max_output_tokens,omitempty"`
	CapabilitySource       string               `json:"capability_source"`
	CapabilityOverrideJSON json.RawMessage      `json:"capability_override_json"`
	Version                int64                `json:"version"`
}

type PublicModel struct {
	ID    string
	Owner string
}

type PublicModelReader interface {
	ListPublic(context.Context) ([]PublicModel, error)
}
