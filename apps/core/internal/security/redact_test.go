package security_test

import (
	"net/url"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/security"
)

func TestRedactURLRemovesUserInfoQueryAndFragment(t *testing.T) {
	target, err := url.Parse("https://user:diagnostic-url-secret@provider.example/v1/messages?api_key=diagnostic-query-secret#diagnostic-fragment-secret")
	if err != nil {
		t.Fatalf("构造测试 URL 失败: %v", err)
	}
	if actual := security.RedactURL(target); actual != "https://provider.example/v1/messages" {
		t.Fatalf("脱敏 URL=%q", actual)
	}
}

func TestRedactDiagnosticTextRemovesKnownSecretSentinels(t *testing.T) {
	input := "Authorization: Bearer diagnostic-bearer-secret; x-api-key: diagnostic-api-key-secret; code=diagnostic-oauth-code-secret; prompt=diagnostic-prompt-secret; tool_args=diagnostic-tool-args-secret"
	actual := security.RedactDiagnosticText(input)
	for _, secret := range []string{"diagnostic-bearer-secret", "diagnostic-api-key-secret", "diagnostic-oauth-code-secret", "diagnostic-prompt-secret", "diagnostic-tool-args-secret"} {
		if strings.Contains(actual, secret) {
			t.Fatalf("诊断文本泄漏 Sentinel %q: %q", secret, actual)
		}
	}
	if !strings.Contains(actual, "[REDACTED]") {
		t.Fatalf("诊断文本未使用统一脱敏占位符: %q", actual)
	}
}
