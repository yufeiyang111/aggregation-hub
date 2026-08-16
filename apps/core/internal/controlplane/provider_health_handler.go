package controlplane

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"aggregationhub.local/core/internal/provider"
)

// ProviderHealthReader 读取已经脱敏且由固定保留期管理的 Provider 健康记录。
type ProviderHealthReader interface {
	Recent(context.Context, string, int) ([]provider.HealthCheck, error)
}

type providerHealthRecord struct {
	ID        string  `json:"id"`
	CheckType string  `json:"check_type"`
	Status    string  `json:"status"`
	LatencyMS *int64  `json:"latency_ms"`
	ErrorCode *string `json:"error_code"`
	CheckedAt string  `json:"checked_at"`
}

type providerHealthPage struct {
	Data []providerHealthRecord `json:"data"`
}

func (server *Server) registerProviderHealthRoutes(mux *http.ServeMux) {
	mux.Handle("GET /internal/v1/providers/{id}/health", server.requireToken(http.HandlerFunc(server.handleProviderHealth)))
}

func (server *Server) handleProviderHealth(response http.ResponseWriter, request *http.Request) {
	providerID := request.PathValue("id")
	query := request.URL.Query()
	if providerID == "" || len(providerID) > 64 || len(query) > 1 || (query.Has("limit") && query.Get("limit") == "") {
		writeProviderError(response, provider.ErrInvalidProvider)
		return
	}
	for key, values := range query {
		if key != "limit" || len(values) != 1 {
			writeProviderError(response, provider.ErrInvalidProvider)
			return
		}
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProviderError(response, provider.ErrInvalidProvider)
			return
		}
		limit = parsed
	}
	if _, err := server.providerReader.FindByID(request.Context(), providerID); err != nil {
		writeProviderError(response, err)
		return
	}
	records, err := server.providerHealth.Recent(request.Context(), providerID, limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "provider_health_unavailable", "无法读取服务健康记录")
		return
	}
	result := make([]providerHealthRecord, 0, len(records))
	for _, item := range records {
		var errorCode *string
		if item.ErrorCode != "" {
			value := item.ErrorCode
			errorCode = &value
		}
		result = append(result, providerHealthRecord{ID: item.ID, CheckType: string(item.CheckType), Status: string(item.Status), LatencyMS: item.LatencyMS, ErrorCode: errorCode, CheckedAt: item.CheckedAt.UTC().Format(time.RFC3339Nano)})
	}
	writeJSON(response, http.StatusOK, providerHealthPage{Data: result})
}
