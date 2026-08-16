package provider

import "strings"

// HealthStatusTransition 根据安全错误类别推导 Provider 健康状态；不改变用户的启停意图。
func HealthStatusTransition(current ProviderStatus, enabled bool, success bool, code string) ProviderStatus {
	if !enabled || current == ProviderStatusDisabled || current == ProviderStatusDraft || current == ProviderStatusDeleted {
		return current
	}
	if success {
		return ProviderStatusEnabled
	}
	switch strings.TrimSpace(code) {
	case "upstream_auth_failed", "credential_unavailable", "auth_required":
		return ProviderStatusAuthRequired
	case "unsupported_feature":
		return current
	default:
		return ProviderStatusDegraded
	}
}

// IsRetainableHealthCode 只允许有限的安全错误分类进入健康记录。
func IsRetainableHealthCode(code string) bool {
	switch strings.TrimSpace(code) {
	case "ok", "upstream_auth_failed", "credential_unavailable", "upstream_rate_limited", "upstream_unavailable", "upstream_invalid_response", "unsupported_feature", "cancelled", "timeout", "tls_error", "dns_error":
		return true
	default:
		return false
	}
}
