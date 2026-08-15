package management

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/credential"
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
type clientFactory interface {
	ForProvider(routing.RoutePlan) (adapter.UpstreamClient, error)
}
type ProviderOperations struct {
	providers   providerReader
	models      modelReconciler
	credentials credentialReader
	adapters    adapterFactory
	clients     clientFactory
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
	_, adapterValue, runtime, credentialValue, client, err := value.prepare(ctx, id)
	if err != nil {
		return adapter.CapabilityTestResult{Code: "provider_unavailable", Message: "服务配置或凭据不可用", HTTPStatus: http.StatusBadGateway}
	}
	defer clear(credentialValue)
	return adapterValue.Test(ctx, client, runtime, credentialValue, adapter.CapabilityTestModels)
}
func (value *ProviderOperations) prepare(ctx context.Context, id string) (provider.Provider, adapter.Adapter, adapter.ProviderRuntime, adapter.Credential, adapter.UpstreamClient, error) {
	providerValue, err := value.providers.FindByID(ctx, id)
	if err != nil {
		return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	parsed, err := url.Parse(providerValue.BaseURL)
	if err != nil {
		return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	adapterValue, err := value.adapters.Create(providerValue.AdapterType)
	if err != nil {
		return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	credentialValue := adapter.Credential{AuthType: providerValue.AuthType}
	if providerValue.AuthType != provider.AuthTypeNone {
		if providerValue.CredentialRef == nil {
			return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, credential.ErrNotFound
		}
		secret, err := value.credentials.Get(ctx, *providerValue.CredentialRef)
		if err != nil {
			return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
		}
		credentialValue.Secret = secret
	}
	route := routing.RoutePlan{ProviderID: providerValue.ID, AdapterType: providerValue.AdapterType, AuthType: providerValue.AuthType, BaseURL: providerValue.BaseURL}
	client, err := value.clients.ForProvider(route)
	if err != nil {
		return provider.Provider{}, nil, adapter.ProviderRuntime{}, adapter.Credential{}, nil, err
	}
	return providerValue, adapterValue, adapter.ProviderRuntime{ID: providerValue.ID, Slug: providerValue.Slug, BaseURL: *parsed, AuthType: providerValue.AuthType, AdapterConfig: append([]byte(nil), providerValue.AdapterConfigJSON...), Timeout: providerValue.Timeout}, credentialValue, client, nil
}
func clear(value adapter.Credential) {
	for index := range value.Secret.Bytes {
		value.Secret.Bytes[index] = 0
	}
}
