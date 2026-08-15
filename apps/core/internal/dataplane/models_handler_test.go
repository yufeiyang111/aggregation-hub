package dataplane_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"aggregationhub.local/core/internal/dataplane"
	"aggregationhub.local/core/internal/provider"
)

type modelReader struct {
	models []provider.PublicModel
	err    error
}

func (reader modelReader) ListPublic(context.Context) ([]provider.PublicModel, error) {
	return reader.models, reader.err
}

func TestModelsHandlerReturnsOpenAICompatibleList(t *testing.T) {
	handler := dataplane.NewModelsHandler(modelReader{models: []provider.PublicModel{{ID: "bundle/gpt-test", Owner: "bundle"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("模型列表响应头或状态错误: status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	const expected = `{"object":"list","data":[{"id":"bundle/gpt-test","object":"model","owned_by":"bundle","created":0}]}` + "\n"
	if response.Body.String() != expected {
		t.Fatalf("模型列表响应错误: %s", response.Body.String())
	}
}

func TestModelsHandlerReturnsSafeError(t *testing.T) {
	handler := dataplane.NewModelsHandler(modelReader{err: errors.New("database connection with secret-like detail")})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":{"code":"models_unavailable","message":"模型列表暂不可用"}}`+"\n" {
		t.Fatalf("模型列表安全错误响应错误: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModelsHandlerHandlesNilReader(t *testing.T) {
	handler := dataplane.NewModelsHandler(nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("空 Reader 错误状态=%d", response.Code)
	}
}
