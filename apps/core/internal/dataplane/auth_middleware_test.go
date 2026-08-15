package dataplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aggregationhub.local/core/internal/dataplane"
	"aggregationhub.local/core/internal/security"
)

type fakeVerifier struct {
	validToken string
	calls      int
	err        error
}

func (verifier *fakeVerifier) Verify(_ context.Context, token string) (security.LocalKeyRecord, bool, error) {
	verifier.calls++
	if verifier.err != nil {
		return security.LocalKeyRecord{}, false, verifier.err
	}
	if token != verifier.validToken {
		return security.LocalKeyRecord{}, false, nil
	}
	return security.LocalKeyRecord{ID: "key-1", Status: security.LocalKeyStatusActive, CreatedAt: time.Now().UTC()}, true, nil
}

func TestRouterBypassesAuthenticationForHealth(t *testing.T) {
	verifier := &fakeVerifier{validToken: "ah_local_valid"}
	router := dataplane.NewRouter(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }),
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) }),
		verifier,
	)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK || verifier.calls != 0 {
		t.Fatalf("/health 必须绕过鉴权 status=%d calls=%d", response.Code, verifier.calls)
	}
}

func TestRequireLocalKeyRejectsMissingInvalidAndConflictingHeaders(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{name: "缺失", headers: map[string]string{}, wantStatus: http.StatusUnauthorized},
		{name: "非法 bearer", headers: map[string]string{"Authorization": "Bearer ah_local_invalid"}, wantStatus: http.StatusUnauthorized},
		{name: "相同凭据", headers: map[string]string{"Authorization": "Bearer ah_local_valid", "X-API-Key": "ah_local_valid"}, wantStatus: http.StatusNoContent},
		{name: "冲突", headers: map[string]string{"Authorization": "Bearer ah_local_valid", "X-API-Key": "ah_local_other"}, wantStatus: http.StatusConflict},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &fakeVerifier{validToken: "ah_local_valid"}
			handler := dataplane.RequireLocalKey(verifier)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d，期望 %d", response.Code, test.wantStatus)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("鉴权失败响应必须包含 request ID")
			}
			if response.Code != http.StatusNoContent && (response.Body.String() == "" || contains(response.Body.String(), "ah_local_")) {
				t.Fatalf("错误响应不得为空或泄露 Local Key: %s", response.Body.String())
			}
		})
	}
}

func TestRequireLocalKeyAcceptsEitherHeaderAndStoresKeyIDInContext(t *testing.T) {
	for _, header := range []struct{ name, value string }{
		{name: "Authorization", value: "Bearer ah_local_valid"},
		{name: "X-API-Key", value: "ah_local_valid"},
	} {
		t.Run(header.name, func(t *testing.T) {
			verifier := &fakeVerifier{validToken: "ah_local_valid"}
			handler := dataplane.RequireLocalKey(verifier)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				keyID, ok := dataplane.LocalKeyIDFromContext(request.Context())
				if !ok || keyID != "key-1" {
					t.Fatalf("认证后的上下文缺少 key ID: %q %t", keyID, ok)
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			request.Header.Set(header.name, header.value)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || verifier.calls != 1 {
				t.Fatalf("合法请求 status=%d calls=%d", response.Code, verifier.calls)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("成功响应必须包含 request ID")
			}
		})
	}
}

func TestAuthenticationErrorHasSafeShape(t *testing.T) {
	verifier := &fakeVerifier{validToken: "ah_local_valid"}
	handler := dataplane.RequireLocalKey(verifier)(http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("鉴权错误不是 JSON: %v", err)
	}
	if body.Error.Code != "unauthorized" || body.RequestID == "" {
		t.Fatalf("鉴权错误体不符合安全形状: %+v", body)
	}
}

func contains(value string, fragment string) bool {
	return len(fragment) > 0 && len(value) >= len(fragment) && (value == fragment || len(value) > len(fragment) && (contains(value[1:], fragment) || value[:len(fragment)] == fragment))
}
