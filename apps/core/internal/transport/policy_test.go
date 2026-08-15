package transport_test

import (
	"testing"

	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/security"
	"aggregationhub.local/core/internal/transport"
)

func TestFactoryRejectsPublicHTTPBaseURL(t *testing.T) {
	factory := transport.NewFactory(security.NetworkPolicy{}, transport.Options{})
	if _, err := factory.ForProvider(localRoute("local-http", "http://127.0.0.1:11434/v1")); err != nil {
		t.Fatalf("Local HTTP Base URL 不应被拒绝: %v", err)
	}
	publicRoute := routing.RoutePlan{ProviderID: "public-http", AdapterType: "openai-compatible", BaseURL: "http://example.test/v1"}
	if _, err := factory.ForProvider(publicRoute); err == nil {
		t.Fatal("Public HTTP Base URL 不应被接受")
	}
}
