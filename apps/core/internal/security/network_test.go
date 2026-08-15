package security_test

import (
	"errors"
	"net/netip"
	"net/url"
	"testing"

	"aggregationhub.local/core/internal/security"
)

func TestNetworkPolicyRejectsDangerousURLsAndAddresses(t *testing.T) {
	policy := security.NetworkPolicy{}
	for _, raw := range []string{
		"file:///etc/passwd", "gopher://example.test", "ftp://example.test", "https://user:pass@example.test", "https://example.test/#fragment", "http://example.test", "https://localhost/v1", "https://169.254.169.254/latest/meta-data",
	} {
		target, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("构造 URL 失败: %v", err)
		}
		if err := policy.ValidateURL(target, security.NetworkScopePublic); !errors.Is(err, security.ErrInvalidUpstreamURL) && !errors.Is(err, security.ErrBlockedUpstreamIP) {
			t.Fatalf("Public URL %q 错误=%v", raw, err)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:11434/v1", "http://10.0.0.8:8080/v1"} {
		target, _ := url.Parse(raw)
		if err := policy.ValidateURL(target, security.NetworkScopeLocal); err != nil {
			t.Fatalf("Local URL %q 不应被拒绝: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://169.254.169.254/latest/meta-data", "http://[fd00:ec2::254]/"} {
		target, _ := url.Parse(raw)
		if err := policy.ValidateURL(target, security.NetworkScopeLocal); !errors.Is(err, security.ErrBlockedUpstreamIP) {
			t.Fatalf("Local metadata URL %q 错误=%v", raw, err)
		}
	}
}

func TestNetworkPolicyClassifiesResolvedAddresses(t *testing.T) {
	policy := security.NetworkPolicy{}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "100.64.0.1", "169.254.169.254", "fd00:ec2::254", "::1"} {
		if err := policy.ValidateResolvedIP(netip.MustParseAddr(raw), security.NetworkScopePublic); !errors.Is(err, security.ErrBlockedUpstreamIP) {
			t.Fatalf("Public 地址 %s 错误=%v", raw, err)
		}
	}
	if err := policy.ValidateResolvedIP(netip.MustParseAddr("8.8.8.8"), security.NetworkScopePublic); err != nil {
		t.Fatalf("公共地址不应被拒绝: %v", err)
	}
	if err := policy.ValidateResolvedIP(netip.MustParseAddr("127.0.0.1"), security.NetworkScopeLocal); err != nil {
		t.Fatalf("Local 回环地址不应被拒绝: %v", err)
	}
	if err := policy.ValidateResolvedIP(netip.MustParseAddr("fe80::1"), security.NetworkScopeLocal); !errors.Is(err, security.ErrBlockedUpstreamIP) {
		t.Fatalf("Local 链路本地地址错误=%v", err)
	}
}
