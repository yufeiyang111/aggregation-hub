package dataplane_test

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"aggregationhub.local/core/internal/config"
	"aggregationhub.local/core/internal/dataplane"
)

func TestNewServerUsesLoopbackAndTimeouts(t *testing.T) {
	cfg := config.Runtime{
		Version:    "0.1.0-rc.5",
		ListenPort: 18443,
	}

	server := dataplane.NewServer(cfg, http.NewServeMux())

	if server.Addr != config.LoopbackHost+":18443" {
		t.Fatalf("addr=%q", server.Addr)
	}
	if server.ReadHeaderTimeout <= 0 {
		t.Fatalf("read header timeout=%s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout <= 0 {
		t.Fatalf("idle timeout=%s", server.IdleTimeout)
	}
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("read header timeout=%s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != 90*time.Second {
		t.Fatalf("idle timeout=%s", server.IdleTimeout)
	}
}

func TestRuntimeExposesNoHostOverrideAndServerAlwaysUsesLoopback(t *testing.T) {
	// Runtime 的字段集合是编译期边界的回归护栏，不允许重新引入 Host 配置入口。
	typeOfRuntime := reflect.TypeOf(config.Runtime{})
	if typeOfRuntime.NumField() != 2 {
		t.Fatalf("runtime field count=%d, want 2", typeOfRuntime.NumField())
	}

	for index := 0; index < typeOfRuntime.NumField(); index++ {
		field := typeOfRuntime.Field(index)
		if field.Name == "ListenHost" || field.Name == "Host" {
			t.Fatalf("runtime exposes host override field %q", field.Name)
		}
	}

	cfg := config.Runtime{Version: "0.1.0-rc.5", ListenPort: 19443}
	server := dataplane.NewServer(cfg, http.NewServeMux())
	if server.Addr != config.LoopbackHost+":19443" {
		t.Fatalf("server addr=%q, want loopback port 19443", server.Addr)
	}
}
