package controlplane

import (
	"context"
	"errors"
	"net/http"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/management"
	"aggregationhub.local/core/internal/provider"
)

type ProviderOperations interface {
	Test(context.Context, string) adapter.CapabilityTestResult
	SyncModels(context.Context, string) (management.SyncResult, error)
}

func (server *Server) registerProviderOperationRoutes(mux *http.ServeMux) {
	mux.Handle("POST /internal/v1/providers/{id}/test", server.requireToken(http.HandlerFunc(server.handleTestProvider)))
	mux.Handle("POST /internal/v1/providers/{id}/sync-models", server.requireToken(http.HandlerFunc(server.handleSyncProviderModels)))
}

func (server *Server) handleTestProvider(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, server.providerOperations.Test(request.Context(), request.PathValue("id")))
}

func (server *Server) handleSyncProviderModels(response http.ResponseWriter, request *http.Request) {
	result, err := server.providerOperations.SyncModels(request.Context(), request.PathValue("id"))
	if err != nil {
		if errors.Is(err, provider.ErrProviderNotFound) {
			writeError(response, http.StatusNotFound, "provider_not_found", "服务不存在")
			return
		}
		writeError(response, http.StatusBadGateway, "provider_sync_failed", "无法同步上游模型")
		return
	}
	writeJSON(response, http.StatusOK, result)
}
