package openai_test

import (
	"testing"

	"aggregationhub.local/core/internal/adapter"
	openaiadapter "aggregationhub.local/core/internal/adapter/openai"
)

func TestRegisterAddsPublicAndLocalAdapterTypes(t *testing.T) {
	registry := adapter.NewRegistry()
	if err := openaiadapter.Register(registry); err != nil {
		t.Fatalf("注册内置 Adapter 失败: %v", err)
	}
	for _, kind := range []string{"openai-compatible", "local-openai-compatible"} {
		value, err := registry.Create(kind)
		if err != nil || value.Type() != kind {
			t.Fatalf("未注册 %s: value=%v err=%v", kind, value, err)
		}
	}
}
