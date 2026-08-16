package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/gateway"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
)

func TestServiceRoutesReadsCredentialAndParsesResponse(t *testing.T) {
	registry := adapter.NewRegistry()
	if err := registry.Register(func() adapter.Adapter { return &fakeAdapter{} }); err != nil {
		t.Fatal(err)
	}
	store := credential.NewMemoryStore()
	ref := credential.Ref("providers/p/auth/test")
	if err := store.Put(context.Background(), ref, credential.SecretValue{Bytes: []byte("test-secret")}); err != nil {
		t.Fatal(err)
	}
	service, err := gateway.New(&fakeRouter{plan: routing.RoutePlan{ProviderID: "p", AdapterType: "fake", BaseURL: "https://provider.example", UpstreamModelID: "model", AuthType: provider.AuthTypeAPIKey, CredentialRef: &ref, Timeout: time.Second}}, store, registry, &fakeClientFactory{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Complete(context.Background(), normalize.NormalizedRequest{Model: "provider/model", Messages: []normalize.Message{{Role: normalize.RoleUser, Parts: []normalize.ContentPart{normalize.TextPart{Text: "hi"}}}}})
	if err != nil || result.ID != "answer" {
		t.Fatalf("网关结果错误: %+v err=%v", result, err)
	}
}

type fakeRouter struct{ plan routing.RoutePlan }

func (value *fakeRouter) Resolve(context.Context, string, provider.RequiredCapabilities) (routing.RoutePlan, error) {
	return value.plan, nil
}

type fakeClientFactory struct{}

func (*fakeClientFactory) ForProvider(routing.RoutePlan) (adapter.UpstreamClient, error) {
	return fakeClient{}, nil
}

type fakeClient struct{}

func (fakeClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(nil)}, nil
}

type fakeAdapter struct{}

func (*fakeAdapter) Type() string                         { return "fake" }
func (*fakeAdapter) Metadata() adapter.Metadata           { return adapter.Metadata{} }
func (*fakeAdapter) ConfigSchema() json.RawMessage        { return json.RawMessage(`{}`) }
func (*fakeAdapter) ValidateConfig(json.RawMessage) error { return nil }
func (*fakeAdapter) DiscoverModels(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential) ([]adapter.DiscoveredModel, error) {
	return nil, nil
}
func (*fakeAdapter) BuildRequest(ctx context.Context, _ routing.RoutePlan, _ normalize.NormalizedRequest, credential adapter.Credential) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.example", nil)
}
func (*fakeAdapter) ParseResponse(context.Context, routing.RoutePlan, *http.Response) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{ID: "answer"}, nil
}
func (*fakeAdapter) ParseStream(context.Context, routing.RoutePlan, *http.Response, normalize.StreamEmitter) error {
	return nil
}
func (*fakeAdapter) Test(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential, adapter.CapabilityTestKind) adapter.CapabilityTestResult {
	return adapter.CapabilityTestResult{}
}

func TestServiceAppliesConfiguredRequestTimeout(t *testing.T) {
	registry := adapter.NewRegistry()
	if err := registry.Register(func() adapter.Adapter { return &fakeAdapter{} }); err != nil {
		t.Fatal(err)
	}
	store := credential.NewMemoryStore()
	factory := deadlineClientFactory{deadline: make(chan time.Time, 1)}
	service, err := gateway.NewWithOptions(
		&fakeRouter{plan: routing.RoutePlan{ProviderID: "p", AdapterType: "fake", BaseURL: "https://provider.example", UpstreamModelID: "model", AuthType: provider.AuthTypeNone, Timeout: time.Second}},
		store,
		registry,
		factory,
		gateway.Options{RequestTimeout: time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), normalize.NormalizedRequest{Model: "provider/model", Messages: []normalize.Message{{Role: normalize.RoleUser, Parts: []normalize.ContentPart{normalize.TextPart{Text: "hi"}}}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case deadline := <-factory.deadline:
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > time.Second+100*time.Millisecond {
			t.Fatalf("请求超时截止时间异常，剩余=%s", remaining)
		}
	case <-time.After(time.Second):
		t.Fatal("上游请求未收到全局超时上下文")
	}
}

type deadlineClientFactory struct{ deadline chan time.Time }

func (factory deadlineClientFactory) ForProvider(routing.RoutePlan) (adapter.UpstreamClient, error) {
	return deadlineClient{deadline: factory.deadline}, nil
}

type deadlineClient struct{ deadline chan time.Time }

func (client deadlineClient) Do(request *http.Request) (*http.Response, error) {
	deadline, ok := request.Context().Deadline()
	if !ok {
		return nil, context.DeadlineExceeded
	}
	client.deadline <- deadline
	return &http.Response{StatusCode: 200, Body: io.NopCloser(nil)}, nil
}
