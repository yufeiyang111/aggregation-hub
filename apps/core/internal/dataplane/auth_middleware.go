package dataplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"aggregationhub.local/core/internal/security"
)

const (
	authorizationHeader = "Authorization"
	apiKeyHeader        = "X-API-Key"
	requestIDHeader     = "X-Request-ID"
	maxCredentialLength = 512
)

type localKeyVerifier interface {
	Verify(ctx context.Context, plaintext string) (security.LocalKeyRecord, bool, error)
}

type localKeyIDContextKey struct{}

func LocalKeyIDFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(localKeyIDContextKey{}).(string)
	return value, ok && value != ""
}

// RequireLocalKey 为除 /health 外的 Data Plane 路由建立统一认证边界，不记录客户端提供的密钥。
func RequireLocalKey(verifier localKeyVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requestID, err := newRequestID()
			if err != nil {
				http.Error(writer, "internal server error", http.StatusInternalServerError)
				return
			}
			writer.Header().Set(requestIDHeader, requestID)
			credential, status, code := extractCredential(request)
			if status != 0 {
				writeAuthError(writer, status, code, requestID)
				return
			}
			record, valid, err := verifier.Verify(request.Context(), credential)
			if err != nil {
				writeAuthError(writer, http.StatusInternalServerError, "authentication_unavailable", requestID)
				return
			}
			if !valid {
				writeAuthError(writer, http.StatusUnauthorized, "unauthorized", requestID)
				return
			}
			next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), localKeyIDContextKey{}, record.ID)))
		})
	}
}

func extractCredential(request *http.Request) (string, int, string) {
	authorization := request.Header.Values(authorizationHeader)
	apiKey := request.Header.Values(apiKeyHeader)
	if len(authorization) > 1 || len(apiKey) > 1 {
		return "", http.StatusUnauthorized, "unauthorized"
	}
	if len(authorization) == 1 {
		parts := strings.Split(authorization[0], " ")
		if len(parts) != 2 || parts[0] != "Bearer" || !validCredentialLength(parts[1]) {
			return "", http.StatusUnauthorized, "unauthorized"
		}
		if len(apiKey) == 1 {
			if !validCredentialLength(apiKey[0]) {
				return "", http.StatusUnauthorized, "unauthorized"
			}
			if parts[1] != apiKey[0] {
				return "", http.StatusConflict, "conflicting_credentials"
			}
		}
		return parts[1], 0, ""
	}
	if len(apiKey) == 1 {
		if !validCredentialLength(apiKey[0]) {
			return "", http.StatusUnauthorized, "unauthorized"
		}
		return apiKey[0], 0, ""
	}
	return "", http.StatusUnauthorized, "unauthorized"
}
func validCredentialLength(value string) bool {
	return value != "" && len(value) <= maxCredentialLength && strings.TrimSpace(value) == value
}
func writeAuthError(writer http.ResponseWriter, status int, code, requestID string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"code": code, "message": "本地访问密钥无效或缺失"}, "request_id": requestID})
}
func newRequestID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", errors.New("生成请求标识失败")
	}
	return "req_" + hex.EncodeToString(bytes[:]), nil
}
