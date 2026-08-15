package security

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

var (
	ErrInvalidUpstreamURL = errors.New("上游 URL 不安全")
	ErrBlockedUpstreamIP  = errors.New("上游地址不允许访问")
)

type NetworkScope string

const (
	NetworkScopePublic NetworkScope = "public"
	NetworkScopeLocal  NetworkScope = "local"
)

type NetworkPolicy struct{}

func (NetworkPolicy) ValidateURL(target *url.URL, scope NetworkScope) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil || target.Fragment != "" || (scope == NetworkScopePublic && target.Scheme != "https") {
		return ErrInvalidUpstreamURL
	}
	hostname := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if hostname == "" || (scope == NetworkScopePublic && hostname == "localhost") {
		return ErrInvalidUpstreamURL
	}
	if address, err := netip.ParseAddr(hostname); err == nil {
		return NetworkPolicy{}.ValidateResolvedIP(address, scope)
	}
	return nil
}

func (NetworkPolicy) ValidateResolvedIP(address netip.Addr, scope NetworkScope) error {
	address = address.Unmap()
	if !address.IsValid() || isMetadataAddress(address) || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return ErrBlockedUpstreamIP
	}
	if scope == NetworkScopePublic && (address.IsLoopback() || address.IsPrivate() || isCarrierGradeNAT(address)) {
		return ErrBlockedUpstreamIP
	}
	if scope != NetworkScopePublic && scope != NetworkScopeLocal {
		return ErrInvalidUpstreamURL
	}
	return nil
}

func isCarrierGradeNAT(address netip.Addr) bool {
	return address.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(address)
}

func isMetadataAddress(address netip.Addr) bool {
	for _, metadata := range []netip.Addr{
		netip.MustParseAddr("169.254.169.254"),
		netip.MustParseAddr("100.100.100.200"),
		netip.MustParseAddr("100.96.0.96"),
		netip.MustParseAddr("fd00:ec2::254"),
		netip.MustParseAddr("fe80::a9fe:a9fe"),
	} {
		if address == metadata {
			return true
		}
	}
	return false
}
