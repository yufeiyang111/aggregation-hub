package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aggregationhub.local/core/internal/observability"
)

// RequestReader 是 Control Plane 需要的只读请求观测边界。
type RequestReader interface {
	List(context.Context, observability.RequestListQuery) (observability.RequestPage, error)
	Get(context.Context, string) (observability.RequestMetadata, error)
}

// UsageReader 是 Control Plane 需要的只读日用量边界。
type UsageReader interface {
	Summary(context.Context, observability.UsageQuery) (observability.UsageSummary, error)
	TimeSeries(context.Context, observability.UsageQuery) (observability.UsageTimeSeries, error)
}

func (server *Server) registerObservabilityRoutes(mux *http.ServeMux) {
	mux.Handle("GET /internal/v1/requests", server.requireToken(http.HandlerFunc(server.handleListRequests)))
	mux.Handle("GET /internal/v1/requests/{id}", server.requireToken(http.HandlerFunc(server.handleGetRequest)))
	mux.Handle("GET /internal/v1/usage/summary", server.requireToken(http.HandlerFunc(server.handleUsageSummary)))
	mux.Handle("GET /internal/v1/usage/timeseries", server.requireToken(http.HandlerFunc(server.handleUsageTimeSeries)))
}

func (server *Server) handleListRequests(response http.ResponseWriter, request *http.Request) {
	query, err := parseRequestListQuery(request)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	page, err := server.requestReader.List(request.Context(), query)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (server *Server) handleGetRequest(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if id == "" || len(id) > 64 {
		writeObservabilityError(response, observability.ErrRequestNotFound)
		return
	}
	value, err := server.requestReader.Get(request.Context(), id)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleUsageSummary(response http.ResponseWriter, request *http.Request) {
	query, err := parseUsageQuery(request, time.Now)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	value, err := server.usageReader.Summary(request.Context(), query)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleUsageTimeSeries(response http.ResponseWriter, request *http.Request) {
	query, err := parseUsageQuery(request, time.Now)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	value, err := server.usageReader.TimeSeries(request.Context(), query)
	if err != nil {
		writeObservabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func parseRequestListQuery(request *http.Request) (observability.RequestListQuery, error) {
	values := request.URL.Query()
	if !onlyObservationParameters(values, "page_size", "cursor", "status", "provider_slug", "public_model_id", "source_protocol", "from_utc", "to_utc") {
		return observability.RequestListQuery{}, observability.ErrInvalidRequestQuery
	}
	pageSize, err := parseObservationPageSize(values.Get("page_size"))
	if err != nil {
		return observability.RequestListQuery{}, err
	}
	fromUTC, err := parseUTC(values.Get("from_utc"))
	if err != nil {
		return observability.RequestListQuery{}, err
	}
	toUTC, err := parseUTC(values.Get("to_utc"))
	if err != nil {
		return observability.RequestListQuery{}, err
	}
	query := observability.RequestListQuery{PageSize: pageSize, Cursor: values.Get("cursor"), Status: observability.RequestStatus(values.Get("status")), ProviderSlug: values.Get("provider_slug"), PublicModelID: values.Get("public_model_id"), SourceProtocol: observability.SourceProtocol(values.Get("source_protocol")), FromUTC: fromUTC, ToUTC: toUTC}
	if err := observability.ValidateRequestListQuery(query); err != nil {
		return observability.RequestListQuery{}, err
	}
	return query, nil
}

func parseUsageQuery(request *http.Request, clock func() time.Time) (observability.UsageQuery, error) {
	values := request.URL.Query()
	if !onlyObservationParameters(values, "provider_slug", "public_model_id", "from_utc", "to_utc") {
		return observability.UsageQuery{}, observability.ErrInvalidRequestQuery
	}
	fromUTC, err := parseUTC(values.Get("from_utc"))
	if err != nil {
		return observability.UsageQuery{}, err
	}
	toUTC, err := parseUTC(values.Get("to_utc"))
	if err != nil {
		return observability.UsageQuery{}, err
	}
	now := clock().UTC()
	if toUTC == nil {
		value := now
		toUTC = &value
	}
	if fromUTC == nil {
		value := toUTC.Add(-6 * 24 * time.Hour)
		fromUTC = &value
	}
	query := observability.UsageQuery{ProviderSlug: values.Get("provider_slug"), PublicModelID: values.Get("public_model_id"), FromUTC: *fromUTC, ToUTC: *toUTC}
	if err := observability.ValidateUsageQuery(query); err != nil {
		return observability.UsageQuery{}, err
	}
	return query, nil
}

func onlyObservationParameters(values map[string][]string, allowed ...string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, entries := range values {
		if _, ok := allowedSet[key]; !ok || len(entries) != 1 {
			return false
		}
	}
	return true
}

func parseObservationPageSize(raw string) (int, error) {
	if raw == "" {
		return 25, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 100 {
		return 0, observability.ErrInvalidRequestQuery
	}
	return value, nil
}

func parseUTC(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > len(time.RFC3339Nano) || !strings.HasSuffix(raw, "Z") {
		return nil, observability.ErrInvalidRequestQuery
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, observability.ErrInvalidRequestQuery
	}
	result := value.UTC()
	return &result, nil
}

func writeObservabilityError(response http.ResponseWriter, err error) {
	if errors.Is(err, observability.ErrRequestNotFound) {
		writeError(response, http.StatusNotFound, "request_not_found", "请求记录不存在")
		return
	}
	if errors.Is(err, observability.ErrInvalidRequestQuery) {
		writeError(response, http.StatusBadRequest, "invalid_request", "观测查询参数无效")
		return
	}
	writeError(response, http.StatusServiceUnavailable, "observability_unavailable", "观测数据暂不可用")
}
