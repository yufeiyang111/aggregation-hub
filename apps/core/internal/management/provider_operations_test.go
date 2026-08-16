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

type testAdapter struct {
	result adapter.CapabilityTestResult
}

func (value testAdapter) Type() string                         { return "test" }
func (value testAdapter) Metadata() adapter.Metadata           { return adapter.Metadata{} }
func (value testAdapter) ConfigSchema() json.RawMessage        { return json.RawMessage(`{}`) }
func (value testAdapter) ValidateConfig(json.RawMessage) error { return nil }
func (value testAdapter) DiscoverModels(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential) ([]adapter.DiscoveredModel, error) {
	return nil, nil
}
func (value testAdapter) BuildRequest(context.Context, routing.RoutePlan, normalize.NormalizedRequest, adapter.Credential) (*http.Request, error) {
	return nil, nil
}
func (value testAdapter) ParseResponse(context.Context, routing.RoutePlan, *http.Response) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (value testAdapter) ParseStream(context.Context, routing.RoutePlan, *http.Response, normalize.StreamEmitter) error {
	return nil
}
func (value testAdapter) Test(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential, adapter.CapabilityTestKind) adapter.CapabilityTestResult {
	return value.result
}

type fakeHealthRecorder struct {
	records []provider.HealthCheck
	err     error
}

func (value *fakeHealthRecorder) Record(_ context.Context, item provider.HealthCheck) error {
	value.records = append(value.records, item)
	return value.err
}

type healthStatusUpdate struct {
	id     string
	status provider.ProviderStatus
}

type fakeHealthStatusWriter struct {
	updates []healthStatusUpdate
	err     error
}

func (value *fakeHealthStatusWriter) SetHealthStatus(_ context.Context, id string, status provider.ProviderStatus, _ time.Time) error {
	value.updates = append(value.updates, healthStatusUpdate{id: id, status: status})
	return value.err
}

func TestOperationsRecordExplicitProviderTestAndClassifyAuthenticationFailure(t *testing.T) {
	ref := credential.Ref("providers/p/auth/test")
	store := credential.NewMemoryStore()
	if err := store.Put(context.Background(), ref, credential.SecretValue{Bytes: []byte("test")}); err != nil {
		t.Fatal(err)
	}
	registry := adapter.NewRegistry()
	if err := registry.Register(func() adapter.Adapter {
		return testAdapter{result: adapter.CapabilityTestResult{Code: "upstream_auth_failed", Message: "上游服务认证失败", HTTPStatus: http.StatusBadGateway}}
	}); err != nil {
		t.Fatal(err)
	}
	providerValue := provider.Provider{ID: "p", Slug: "bundle", AdapterType: "test", AuthType: provider.AuthTypeAPIKey, BaseURL: "https://provider.example", CredentialRef: &ref, LifecycleStatus: provider.ProviderStatusEnabled, Enabled: true, Timeout: time.Second}
	service, err := management.NewProviderOperations(&fakeProviders{value: providerValue}, &fakeModels{}, store, registry, fakeClientFactory{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &fakeHealthRecorder{}
	statusWriter := &fakeHealthStatusWriter{}
	if err := service.AttachHealthRecording(management.HealthRecordingOptions{Recorder: recorder, StatusWriter: statusWriter}); err != nil {
		t.Fatal(err)
	}

	result := service.Test(context.Background(), "p")
	if result.Success || result.Code != "upstream_auth_failed" {
		t.Fatalf("测试结果错误: %+v", result)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("健康记录数量=%d", len(recorder.records))
	}
	record := recorder.records[0]
	if record.ProviderID != "p" || record.CheckType != provider.HealthCheckModels || record.Status != provider.HealthCheckFailed || record.ErrorCode != "upstream_auth_failed" || record.LatencyMS == nil || *record.LatencyMS < 0 {
		t.Fatalf("健康记录错误: %+v", record)
	}
	if len(statusWriter.updates) != 1 || statusWriter.updates[0] != (healthStatusUpdate{id: "p", status: provider.ProviderStatusAuthRequired}) {
		t.Fatalf("健康状态更新错误: %+v", statusWriter.updates)
	}
}

func TestOperationsRecordUnsupportedTestWithoutChangingProviderStatus(t *testing.T) {
	registry := adapter.NewRegistry()
	if err := registry.Register(func() adapter.Adapter {
		return testAdapter{result: adapter.CapabilityTestResult{Code: "unsupported_feature", Message: "该测试类型尚未支持"}}
	}); err != nil {
		t.Fatal(err)
	}
	providerValue := provider.Provider{ID: "p", Slug: "bundle", AdapterType: "test", AuthType: provider.AuthTypeNone, BaseURL: "https://provider.example", LifecycleStatus: provider.ProviderStatusEnabled, Enabled: true, Timeout: time.Second}
	service, err := management.NewProviderOperations(&fakeProviders{value: providerValue}, &fakeModels{}, credential.NewMemoryStore(), registry, fakeClientFactory{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &fakeHealthRecorder{}
	statusWriter := &fakeHealthStatusWriter{}
	if err := service.AttachHealthRecording(management.HealthRecordingOptions{Recorder: recorder, StatusWriter: statusWriter}); err != nil {
		t.Fatal(err)
	}

	result := service.Test(context.Background(), "p")
	if result.Code != "unsupported_feature" || len(recorder.records) != 1 || recorder.records[0].Status != provider.HealthCheckSkipped || len(statusWriter.updates) != 0 {
		t.Fatalf("未支持能力记录错误 result=%+v records=%+v updates=%+v", result, recorder.records, statusWriter.updates)
	}
}

func TestOperationsRecordCredentialUnavailableWithoutLeakingStoreError(t *testing.T) {
	ref := credential.Ref("providers/p/auth/test")
	registry := adapter.NewRegistry()
	if err := registry.Register(func() adapter.Adapter { return testAdapter{} }); err != nil {
		t.Fatal(err)
	}
	providerValue := provider.Provider{ID: "p", Slug: "bundle", AdapterType: "test", AuthType: provider.AuthTypeAPIKey, BaseURL: "https://provider.example", CredentialRef: &ref, LifecycleStatus: provider.ProviderStatusEnabled, Enabled: true, Timeout: time.Second}
	service, err := management.NewProviderOperations(&fakeProviders{value: providerValue}, &fakeModels{}, credential.NewMemoryStore(), registry, fakeClientFactory{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &fakeHealthRecorder{}
	statusWriter := &fakeHealthStatusWriter{}
	if err := service.AttachHealthRecording(management.HealthRecordingOptions{Recorder: recorder, StatusWriter: statusWriter}); err != nil {
		t.Fatal(err)
	}

	result := service.Test(context.Background(), "p")
	if result.Success || result.Code != "credential_unavailable" || result.Message != "服务凭据不可用" {
		t.Fatalf("凭据错误未安全映射: %+v", result)
	}
	if len(recorder.records) != 1 || recorder.records[0].ErrorCode != "credential_unavailable" || len(statusWriter.updates) != 1 || statusWriter.updates[0].status != provider.ProviderStatusAuthRequired {
		t.Fatalf("凭据健康记录错误 records=%+v updates=%+v", recorder.records, statusWriter.updates)
	}
}

func TestOperationsRecordCancelledTestWithoutChangingProviderStatus(t *testing.T) {
	registry := adapter.NewRegistry()
	if err := registry.Register(func() adapter.Adapter {
		return testAdapter{result: adapter.CapabilityTestResult{Code: "cancelled", Message: "测试已取消", HTTPStatus: 499}}
	}); err != nil {
		t.Fatal(err)
	}
	providerValue := provider.Provider{ID: "p", Slug: "bundle", AdapterType: "test", AuthType: provider.AuthTypeNone, BaseURL: "https://provider.example", LifecycleStatus: provider.ProviderStatusEnabled, Enabled: true, Timeout: time.Second}
	service, err := management.NewProviderOperations(&fakeProviders{value: providerValue}, &fakeModels{}, credential.NewMemoryStore(), registry, fakeClientFactory{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &fakeHealthRecorder{}
	statusWriter := &fakeHealthStatusWriter{}
	if err := service.AttachHealthRecording(management.HealthRecordingOptions{Recorder: recorder, StatusWriter: statusWriter}); err != nil {
		t.Fatal(err)
	}

	result := service.Test(context.Background(), "p")
	if result.Code != "cancelled" || len(recorder.records) != 1 || recorder.records[0].Status != provider.HealthCheckSkipped || len(statusWriter.updates) != 0 {
		t.Fatalf("取消测试记录错误 result=%+v records=%+v updates=%+v", result, recorder.records, statusWriter.updates)
	}
}
