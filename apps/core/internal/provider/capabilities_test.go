package provider_test

import (
	"encoding/json"
	"errors"
	"testing"

	"aggregationhub.local/core/internal/provider"
)

func TestEffectiveCapabilitiesAppliesOnlyKnownBooleanOverrides(t *testing.T) {
	base := provider.Capabilities{Streaming: true, Tools: false}
	effective, err := provider.EffectiveCapabilities(base, json.RawMessage(`{"supports_tools":true,"supports_streaming":false}`))
	if err != nil {
		t.Fatalf("应用能力覆盖失败: %v", err)
	}
	if effective.Streaming || !effective.Tools {
		t.Fatalf("能力覆盖结果错误: %+v", effective)
	}
	if _, err := provider.EffectiveCapabilities(base, json.RawMessage(`{"unknown":true}`)); !errors.Is(err, provider.ErrInvalidCapabilityOverride) {
		t.Fatalf("未知覆盖字段错误=%v", err)
	}
	if _, err := provider.EffectiveCapabilities(base, json.RawMessage(`{"supports_tools":"yes"}`)); !errors.Is(err, provider.ErrInvalidCapabilityOverride) {
		t.Fatalf("非布尔覆盖字段错误=%v", err)
	}
}

func TestCapabilitiesValidateReportsFirstMissingFeature(t *testing.T) {
	err := (provider.Capabilities{Streaming: true}).Validate(provider.RequiredCapabilities{Streaming: true, Tools: true})
	var unsupported *provider.UnsupportedCapabilityError
	if !errors.As(err, &unsupported) || unsupported.Feature != "tools" {
		t.Fatalf("缺失能力错误=%v", err)
	}
	if err := (provider.Capabilities{Streaming: true, Tools: true}).Validate(provider.RequiredCapabilities{Streaming: true, Tools: true}); err != nil {
		t.Fatalf("已支持能力不应报错: %v", err)
	}
}
