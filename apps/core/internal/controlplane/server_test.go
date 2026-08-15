package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"aggregationhub.local/core/internal/controlplane"
)

const testManagementToken = "0123456789abcdef0123456789abcdef"

func TestControlPlaneRequiresFixedManagementTokenAndDoesNotSetCORS(t *testing.T) {
	server, err := controlplane.NewServer(controlplane.Options{ManagementToken: testManagementToken, Runtime: func() controlplane.RuntimeStatus {
		return controlplane.RuntimeStatus{State: "running", DataPlaneURL: "http://127.0.0.1:18443", StartedAt: "2026-08-14T00:00:00Z", Version: "0.1.0-rc.3"}
	}, Shutdown: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatalf("创建 Control Plane 失败: %v", err)
	}
	for _, token := range []string{"", "wrong"} {
		request := httptest.NewRequest(http.MethodGet, "/internal/v1/runtime", nil)
		if token != "" {
			request.Header.Set(controlplane.ManagementTokenHeader, token)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("令牌 %q 状态=%d", token, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/runtime", nil)
	request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("Runtime 响应错误: status=%d cors=%q", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}
	var runtime controlplane.RuntimeStatus
	if err := json.Unmarshal(response.Body.Bytes(), &runtime); err != nil || runtime.State != "running" {
		t.Fatalf("Runtime 响应解析错误: %+v %v", runtime, err)
	}
	options := httptest.NewRequest(http.MethodOptions, "/internal/v1/runtime", nil)
	options.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	optionsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(optionsResponse, options)
	if optionsResponse.Code != http.StatusNotFound && optionsResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("OPTIONS 不应被广泛允许: %d", optionsResponse.Code)
	}
}

func TestControlPlaneShutdownRequiresPostAndRunsOnce(t *testing.T) {
	var shutdownCalls atomic.Int32
	server, err := controlplane.NewServer(controlplane.Options{ManagementToken: testManagementToken, Runtime: func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} }, Shutdown: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("Shutdown 缺少截止时间")
		}
		shutdownCalls.Add(1)
		return nil
	}})
	if err != nil {
		t.Fatalf("创建 Control Plane 失败: %v", err)
	}
	get := httptest.NewRequest(http.MethodGet, "/internal/v1/runtime/shutdown", nil)
	get.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusMethodNotAllowed && getResponse.Code != http.StatusNotFound {
		t.Fatalf("GET shutdown 状态=%d", getResponse.Code)
	}
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/internal/v1/runtime/shutdown", nil)
		request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("POST shutdown 状态=%d", response.Code)
		}
	}
	if shutdownCalls.Load() != 1 {
		t.Fatalf("Shutdown 调用次数=%d", shutdownCalls.Load())
	}
}
