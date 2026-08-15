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
	if _, err := provider.ParseCapabilityOverride(json.RawMessage(`null`)); !errors.Is(err, provider.ErrInvalidCapabilityOverride) {
		t.Fatalf("null 覆盖字段错误=%v", err)
	}
	if _, err := provider.ParseCapabilityOverride(json.RawMessage(`[]`)); !errors.Is(err, provider.ErrInvalidCapabilityOverride) {
		t.Fatalf("数组覆盖字段错误=%v", err)
	}
	if _, err := provider.ParseCapabilityOverride(json.RawMessage(`{"supports_tools":null}`)); !errors.Is(err, provider.ErrInvalidCapabilityOverride) {
		t.Fatalf("null 能力值错误=%v", err)
	}
	if _, err := provider.ParseCapabilityOverride(json.RawMessage(`{"supports_tools":true,"supports_tools":false}`)); !errors.Is(err, provider.ErrInvalidCapabilityOverride) {
		t.Fatalf("重复能力字段错误=%v", err)
	}
}

func TestCapabilityOverrideJSONPreservesExplicitFalseAndCanReset(t *testing.T) {
	value := false
	override := provider.CapabilityOverride{Tools: &value}
	raw, err := override.JSON()
	if err != nil || string(raw) != `{"supports_tools":false}` {
		t.Fatalf("编码能力覆盖错误: raw=%s err=%v", raw, err)
	}
	parsed, err := provider.ParseCapabilityOverride(raw)
	if err != nil || parsed.Tools == nil || *parsed.Tools {
		t.Fatalf("解析显式 false 覆盖错误: %+v, %v", parsed, err)
	}
	reset, err := (provider.CapabilityOverride{}).JSON()
	if err != nil || string(reset) != `{}` {
		t.Fatalf("恢复上游声明编码错误: raw=%s err=%v", reset, err)
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
