package adapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
)

type fakeAdapter struct{ kind string }

func (value *fakeAdapter) Type() string                         { return value.kind }
func (value *fakeAdapter) Metadata() adapter.Metadata           { return adapter.Metadata{} }
func (value *fakeAdapter) ConfigSchema() json.RawMessage        { return json.RawMessage(`{}`) }
func (value *fakeAdapter) ValidateConfig(json.RawMessage) error { return nil }
func (value *fakeAdapter) DiscoverModels(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential) ([]adapter.DiscoveredModel, error) {
	return nil, nil
}
func (value *fakeAdapter) BuildRequest(context.Context, routing.RoutePlan, normalize.NormalizedRequest, adapter.Credential) (*http.Request, error) {
	return nil, nil
}
func (value *fakeAdapter) ParseResponse(context.Context, routing.RoutePlan, *http.Response) (normalize.NormalizedResponse, error) {
	return normalize.NormalizedResponse{}, nil
}
func (value *fakeAdapter) ParseStream(context.Context, routing.RoutePlan, *http.Response, normalize.StreamEmitter) error {
	return nil
}
func (value *fakeAdapter) Test(context.Context, adapter.UpstreamClient, adapter.ProviderRuntime, adapter.Credential, adapter.CapabilityTestKind) adapter.CapabilityTestResult {
	return adapter.CapabilityTestResult{}
}

func TestRegistryRejectsDuplicateAndCreatesIndependentAdapters(t *testing.T) {
	registry := adapter.NewRegistry()
	factory := func() adapter.Adapter { return &fakeAdapter{kind: "example"} }
	if err := registry.Register(factory); err != nil {
		t.Fatalf("注册 Adapter 失败: %v", err)
	}
	if err := registry.Register(factory); !errors.Is(err, adapter.ErrDuplicateAdapterType) {
		t.Fatalf("重复类型未被拒绝: %v", err)
	}
	first, err := registry.Create("example")
	if err != nil {
		t.Fatalf("创建 Adapter 失败: %v", err)
	}
	second, err := registry.Create("example")
	if err != nil {
		t.Fatalf("再次创建 Adapter 失败: %v", err)
	}
	if first == second {
		t.Fatal("每次 Create 必须得到独立 Adapter 实例")
	}
}

func TestRegistryRejectsUnknownType(t *testing.T) {
	_, err := adapter.NewRegistry().Create("missing")
	if !errors.Is(err, adapter.ErrAdapterNotFound) {
		t.Fatalf("未知类型错误=%v", err)
	}
}

func TestCapabilityTestResultUsesStableSnakeCaseJSON(t *testing.T) {
	encoded, err := json.Marshal(adapter.CapabilityTestResult{Success: true, Code: "ok", Message: "safe", HTTPStatus: 200, Retryable: false})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, field := range []string{`"success"`, `"code"`, `"message"`, `"http_status"`, `"retryable"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("缺少 JSON 字段 %s: %s", field, text)
		}
	}
}
