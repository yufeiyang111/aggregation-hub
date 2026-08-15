package management_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/management"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
)

func TestOperationsSyncsDiscoveredModelsAsDisabled(t *testing.T) {
	ref := credential.Ref("providers/p/auth/test")
	store := credential.NewMemoryStore()
	_ = store.Put(context.Background(), ref, credential.SecretValue{Bytes: []byte("test")})
	registry := adapter.NewRegistry()
	_ = registry.Register(func() adapter.Adapter { return fakeAdapter{} })
	base, _ := url.Parse("https://provider.example")
	models := &fakeModels{}
	service, err := management.NewProviderOperations(&fakeProviders{value: provider.Provider{ID: "p", Slug: "bundle", AdapterType: "fake", AuthType: provider.AuthTypeAPIKey, BaseURL: base.String(), CredentialRef: &ref, Timeout: time.Second}}, models, store, registry, fakeClientFactory{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SyncModels(context.Background(), "p")
	if err != nil || result.Discovered != 1 || models.slug != "bundle" {
		t.Fatalf("同步错误 result=%+v err=%v models=%+v", result, err, models)
	}
}

type fakeProviders struct{ value provider.Provider }

func (value *fakeProviders) FindByID(context.Context, string) (provider.Provider, error) {
	return value.value, nil
}

type fakeModels struct {
	slug  string
	items []provider.SyncedModel
}

func (value *fakeModels) ReconcileSyncedModels(_ context.Context, _ string, slug string, items []provider.SyncedModel, _ time.Time) error {
	value.slug = slug
	value.items = items
	return nil
}

type fakeClientFactory struct{}

func (fakeClientFactory) ForProvider(routing.RoutePlan) (adapter.UpstreamClient, error) {
	return fakeClient{}, nil
}

type fakeClient struct{}

func (fakeClient) Do(*http.Request) (*http.Response, error) { return nil, nil }

type fakeAdapter struct{}

func (fakeAdapter) Type() string                         { return "fake" }
func (fakeAdapter) Metadata() adapter.Metadata           { return adapter.Metadata{} }
func (fakeAdapter) ConfigSchema() json.RawMessage        { return json.RawMessage(`{}`) }
func (fakeAdapter) ValidateConfig(json.RawMessage) error { return nil }
func (fakeAdapter) DiscoverModels(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential) ([]adapter.DiscoveredModel, error) {
	return []adapter.DiscoveredModel{{UpstreamModelID: "model-a", DisplayName: "Model A", Source: provider.ModelSourceUpstream}}, nil
}
func (fakeAdapter) BuildRequest(context.Context, routing.RoutePlan, normalize.NormalizedRequest, adapter.Credential) (*http.Request, error) {
	return nil, nil
}
func (fakeAdapter) ParseResponse(context.Context, routing.RoutePlan, *http.Response) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (fakeAdapter) ParseStream(context.Context, routing.RoutePlan, *http.Response, normalize.StreamEmitter) error {
	return nil
}
func (fakeAdapter) Test(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential, adapter.CapabilityTestKind) adapter.CapabilityTestResult {
	return adapter.CapabilityTestResult{}
}
