package security

import (
	"net/url"
	"regexp"
)

const RedactedDiagnosticValue = "[REDACTED]"

type diagnosticSecretPattern struct {
	expression  *regexp.Regexp
	replacement string
}

var diagnosticSecretPatterns = []diagnosticSecretPattern{
	{expression: regexp.MustCompile(`(?i)\bBearer\s+[^\s;,]+`), replacement: "Bearer " + RedactedDiagnosticValue},
	{expression: regexp.MustCompile(`(?i)\b(x-api-key)\s*[:=]\s*[^\s;,]+`), replacement: "${1}: " + RedactedDiagnosticValue},
	{expression: regexp.MustCompile(`(?i)\b(code)\s*[:=]\s*[^\s&;,]+`), replacement: "${1}=" + RedactedDiagnosticValue},
	{expression: regexp.MustCompile(`(?i)\b(prompt|tool_args|tool[_-]?arguments)\s*[:=]\s*[^\r\n;,&]+`), replacement: "${1}=" + RedactedDiagnosticValue},
	{expression: regexp.MustCompile(`(?i)\b(sk-[a-z0-9_-]{20,}|ghp_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,})\b`), replacement: RedactedDiagnosticValue},
}

// RedactURL 仅保留诊断所需的协议、主机和路径，禁止 Query、Fragment 和用户信息进入输出。
func RedactURL(target *url.URL) string {
	if target == nil {
		return ""
	}
	redacted := &url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path, RawPath: target.RawPath}
	return redacted.String()
}

// ContainsDiagnosticSecret 判断文本是否带有已知敏感格式，供结构化日志字段拒绝使用。
func ContainsDiagnosticSecret(value string) bool {
	for _, pattern := range diagnosticSecretPatterns {
		if pattern.expression.MatchString(value) {
			return true
		}
	}
	return false
}

// RedactDiagnosticText 对已知敏感字段做纵深脱敏；字段 allowlist 仍是主防线。
func RedactDiagnosticText(value string) string {
	for _, pattern := range diagnosticSecretPatterns {
		value = pattern.expression.ReplaceAllString(value, pattern.replacement)
	}
	return value
}
