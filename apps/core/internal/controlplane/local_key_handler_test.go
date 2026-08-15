package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/controlplane"
	"aggregationhub.local/core/internal/security"
)

type fakeLocalKeyService struct {
	name      string
	expiresAt *time.Time
	calls     int
	err       error
}

func (service *fakeLocalKeyService) Create(_ context.Context, name string, expiresAt *time.Time) (string, security.LocalKeyRecord, error) {
	service.calls++
	service.name, service.expiresAt = name, expiresAt
	if service.err != nil {
		return "", security.LocalKeyRecord{}, service.err
	}
	createdAt := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return "ah_local_test_only", security.LocalKeyRecord{ID: "key-1", Name: name, Prefix: "ah_local_test", Suffix: "_only", CreatedAt: createdAt, ExpiresAt: expiresAt}, nil
}

func newLocalKeyServer(t *testing.T, service *fakeLocalKeyService) *controlplane.Server {
	t.Helper()
	server, err := controlplane.NewServer(controlplane.Options{
		ManagementToken: testManagementToken,
		Runtime:         func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} },
		Shutdown:        func(context.Context) error { return nil },
		LocalKeyService: service,
	})
	if err != nil {
		t.Fatalf("创建 Local Key Control Plane 失败: %v", err)
	}
	return server
}

func TestLocalKeyCreationIsManagementProtectedAndDisplayOnce(t *testing.T) {
	service := &fakeLocalKeyService{}
	server := newLocalKeyServer(t, service)

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, providerRequest(http.MethodPost, "/internal/v1/local-keys", `{"name":"Claude Code"}`, false))
	if unauthorized.Code != http.StatusUnauthorized || service.calls != 0 {
		t.Fatalf("未鉴权 Local Key 创建 status=%d calls=%d", unauthorized.Code, service.calls)
	}

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodPost, "/internal/v1/local-keys", `{"name":"Claude Code"}`, true))
	if response.Code != http.StatusCreated || service.calls != 1 || service.name != "Claude Code" {
		t.Fatalf("创建 Local Key 失败 status=%d calls=%d name=%q", response.Code, service.calls, service.name)
	}
	var body struct {
		Key         string `json:"key"`
		DisplayOnce bool   `json:"display_once"`
		TokenHash   string `json:"token_hash"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Key != "ah_local_test_only" || !body.DisplayOnce || body.TokenHash != "" {
		t.Fatalf("Local Key 单次响应错误: %+v %v", body, err)
	}
}

func TestLocalKeyCreationUsesSafeErrors(t *testing.T) {
	service := &fakeLocalKeyService{err: security.ErrInvalidKeyName}
	server := newLocalKeyServer(t, service)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodPost, "/internal/v1/local-keys", `{"name":""}`, true))
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "test_only") {
		t.Fatalf("无效 Local Key 请求错误: %d %s", response.Code, response.Body.String())
	}

	service = &fakeLocalKeyService{err: errors.New("private storage failure")}
	server = newLocalKeyServer(t, service)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodPost, "/internal/v1/local-keys", `{"name":"Claude Code"}`, true))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private storage failure") {
		t.Fatalf("内部 Local Key 错误泄露: %d %s", response.Code, response.Body.String())
	}
}
