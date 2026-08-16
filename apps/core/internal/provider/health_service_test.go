package provider

import "testing"

func TestHealthStatusTransitionPreservesManualStateAndClassifiesFailures(t *testing.T) {
	cases := []struct {
		name             string
		current          ProviderStatus
		enabled, success bool
		code             string
		want             ProviderStatus
	}{
		{"认证失败", ProviderStatusEnabled, true, false, "upstream_auth_failed", ProviderStatusAuthRequired},
		{"可重试失败", ProviderStatusEnabled, true, false, "upstream_unavailable", ProviderStatusDegraded},
		{"未支持能力不降级", ProviderStatusEnabled, true, false, "unsupported_feature", ProviderStatusEnabled},
		{"用户取消不降级", ProviderStatusEnabled, true, false, "cancelled", ProviderStatusEnabled},
		{"成功恢复", ProviderStatusDegraded, true, true, "ok", ProviderStatusEnabled},
		{"停用保持停用", ProviderStatusDisabled, false, false, "upstream_unavailable", ProviderStatusDisabled},
	}
	for _, item := range cases {
		if got := HealthStatusTransition(item.current, item.enabled, item.success, item.code); got != item.want {
			t.Fatalf("%s: got=%s want=%s", item.name, got, item.want)
		}
	}
}
func TestIsRetainableHealthCodeRejectsUnsafeText(t *testing.T) {
	if !IsRetainableHealthCode("upstream_rate_limited") || IsRetainableHealthCode("provider secret from body") {
		t.Fatal("健康错误代码 allowlist 错误")
	}
}
