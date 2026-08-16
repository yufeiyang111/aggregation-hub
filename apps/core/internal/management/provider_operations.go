package management

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/id"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
)

var ErrOperationsDependency = errors.New("Provider 操作依赖无效")

type providerReader interface {
	FindByID(context.Context, string) (provider.Provider, error)
}
type modelReconciler interface {
	ReconcileSyncedModels(context.Context, string, string, []provider.SyncedModel, time.Time) error
}
type credentialReader interface {
	Get(context.Context, credential.Ref) (credential.SecretValue, error)
}
type adapterFactory interface {
	Create(string) (adapter.Adapter, error)
}

// HealthRecorder 记录经过去敏与分类的 Provider 健康检查结果。
type HealthRecorder interface {
	Record(context.Context, provider.HealthCheck) error
}

// HealthStatusWriter 只允许由健康检查更新可推导的 Provider 运行状态。
type HealthStatusWriter interface {
	SetHealthStatus(context.Context, string, provider.ProviderStatus, time.Time) error
}

// HealthRecordingOptions 为显式测试接入可选的健康记录依赖。
type HealthRecordingOptions struct {
	Recorder     HealthRecorder
	StatusWriter HealthStatusWriter
}

type clientFactory interface {
	ForProvider(routing.RoutePlan) (adapter.UpstreamClient, error)
}
type ProviderOperations struct {
	providers   providerReader
	models      modelReconciler
	credentials credentialReader
	adapters    adapterFactory
	clients     clientFactory
	health      HealthRecorder
	status      HealthStatusWriter
	now         func() time.Time
}
type SyncResult struct {
	Discovered int `json:"discovered"`
}

func NewProviderOperations(providers providerReader, models modelReconciler, credentials credentialReader, adapters adapterFactory, clients clientFactory) (*ProviderOperations, error) {
	if providers == nil || models == nil || credentials == nil || adapters == nil || clients == nil {
		return nil, ErrOperationsDependency
	}
	return &ProviderOperations{providers: providers, models: models, credentials: credentials, adapters: adapters, clients: clients, now: time.Now}, nil
}

// AttachHealthRecording 仅为用户显式测试附加健康记录，不改变现有同步模型调用。
// 应在服务开始接收请求前调用，避免在并发请求期间变更依赖。
func (value *ProviderOperations) AttachHealthRecording(options HealthRecordingOptions) error {
	if value == nil || options.Recorder == nil || options.StatusWriter == nil {
		return ErrOperationsDependency
	}
	value.health = options.Recorder
	value.status = options.StatusWriter
	return nil
}

func (value *ProviderOperations) SyncModels(ctx context.Context, id string) (SyncResult, error) {
	providerValue, adapterValue, runtime, credentialValue, client, err := value.prepare(ctx, id)
	if err != nil {
		return SyncResult{}, err
	}
	defer clear(credentialValue)
	models, err := adapterValue.DiscoverModels(ctx, client, runtime, credentialValue)
	if err != nil {
		return SyncResult{}, err
	}
	if err := value.models.ReconcileSyncedModels(ctx, providerValue.ID, providerValue.Slug, models, value.now().UTC()); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Discovered: len(models)}, nil
}

func (value *ProviderOperations) Test(ctx context.Context, id string) adapter.CapabilityTestResult {
	startedAt := value.now().UTC()
	providerValue, err := value.providers.FindByID(ctx, id)
	if err != nil {
		return unavailableTestResult(err)
	}
	adapterValue, runtime, credentialValue, client, err := value.prepareKnownProvider(ctx, providerValue)
	if err != nil {
		result := unavailableTestResult(err)
		value.recordHealth(ctx, providerValue, startedAt, result)
		return result
	}
	defer clear(credentialValue)
	result := adapterValue.Test(ctx, client, runtime, credentialValue, adapter.CapabilityTestModels)
	value.recordHealth(ctx, providerValue, startedAt, result)
	return result
}

func unavailableTestResult(err error) adapter.CapabilityTestResult {
	switch {
	case errors.Is(err, context.Canceled):
		return adapter.CapabilityTestResult{Code: "cancelled", Message: "测试已取消", HTTPStatus: 499}
	case errors.Is(err, context.DeadlineExceeded):
		return adapter.CapabilityTestResult{Code: "timeout", Message: "测试超时", HTTPStatus: http.StatusGatewayTimeout, Retryable: true}
	case errors.Is(err, credential.ErrNotFound), errors.Is(err, credential.ErrUnsupported):
		return adapter.CapabilityTestResult{Code: "credential_unavailable", Message: "服务凭据不可用", HTTPStatus: http.StatusBadGateway}
	default:
		return adapter.CapabilityTestResult{Code: "upstream_unavailable", Message: "服务配置不可用", HTTPStatus: http.StatusBadGateway, Retryable: true}
	}
}

func (value *ProviderOperations) recordHealth(ctx context.Context, providerValue provider.Provider, startedAt time.Time, result adapter.CapabilityTestResult) {
	if value.health == nil || value.status == nil || providerValue.ID == "" {
		return
	}
	checkedAt := value.now().UTC()
	latency := checkedAt.Sub(startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	checkID, err := id.RandomULID(checkedAt)
	if err != nil {
		return
	}
	code := "ok"
	if !result.Success {
		code = result.Code
		if !provider.IsRetainableHealthCode(code) {
			code = "upstream_unavailable"
		}
	}
	checkStatus := provider.HealthCheckSucceeded
	if !result.Success {
		checkStatus = provider.HealthCheckFailed
		if code == "unsupported_feature" || code == "cancelled" {
			checkStatus = provider.HealthCheckSkipped
		}
	}
	if value.health.Record(ctx, provider.HealthCheck{ID: checkID, ProviderID: providerValue.ID, CheckType: provider.HealthCheckModels, Status: checkStatus, LatencyMS: &latency, ErrorCode: code, CheckedAt: checkedAt}) != nil {
		return
	}
	if checkStatus == provider.HealthCheckSkipped {
		return
	}
	_ = value.status.SetHealthStatus(ctx, providerValue.ID, provider.HealthStatusTransition(providerValue.LifecycleStatus, providerValue.Enabled, result.Success, code), checkedAt)
}

func (value *ProviderOperations) prepare(ctx context.Context, id string) (provider.Provider, adapter.Adapter, adapter.ProviderRuntime, adapter.Credential, adapter.UpstreamClient, error) {
	providerValue, err := value.providers.FindByID(ctx, id)
	if err != nil {
		return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	adapterValue, runtime, credentialValue, client, err := value.prepareKnownProvider(ctx, providerValue)
	if err != nil {
		return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	return providerValue, adapterValue, runtime, credentialValue, client, nil
}

func (value *ProviderOperations) prepareKnownProvider(ctx context.Context, providerValue provider.Provider) (adapter.Adapter, adapter.ProviderRuntime, adapter.Credential, adapter.UpstreamClient, error) {
	parsed, err := url.Parse(providerValue.BaseURL)
	if err != nil {
		return nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	adapterValue, err := value.adapters.Create(providerValue.AdapterType)
	if err != nil {
		return nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	credentialValue := adapter.Credential{AuthType: providerValue.AuthType}
	if providerValue.AuthType != provider.AuthTypeNone {
		if providerValue.CredentialRef == nil {
			return nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, credential.ErrNotFound
		}
		secret, err := value.credentials.Get(ctx, *providerValue.CredentialRef)
		if err != nil {
			return nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
		}
		credentialValue.Secret = secret
	}
	route := routing.RoutePlan{ProviderID: providerValue.ID, AdapterType: providerValue.AdapterType, AuthType: providerValue.AuthType, BaseURL: providerValue.BaseURL}
	client, err := value.clients.ForProvider(route)
	if err != nil {
		return nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	return adapterValue, adapter.ProviderRuntime{ID: providerValue.ID, Slug: providerValue.Slug, BaseURL: *parsed, AuthType: providerValue.AuthType, AdapterConfig: append([]byte(nil), providerValue.AdapterConfigJSON...), Timeout: providerValue.Timeout}, credentialValue, client, nil
}

func clear(value adapter.Credential) {
	for index := range value.Secret.Bytes {
		value.Secret.Bytes[index] = 0
	}
}
