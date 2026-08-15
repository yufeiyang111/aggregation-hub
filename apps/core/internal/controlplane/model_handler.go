package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"aggregationhub.local/core/internal/provider"
)

type ModelService interface {
	Enable(context.Context, string, int64) (provider.ModelDTO, error)
	Disable(context.Context, string, int64) (provider.ModelDTO, error)
}

type ModelReader interface {
	List(context.Context, provider.ModelPageQuery) (provider.ModelPage, error)
}

type modelPageResponse struct {
	Data       []provider.ModelDTO `json:"data"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (server *Server) registerModelRoutes(mux *http.ServeMux) {
	mux.Handle("GET /internal/v1/models", server.requireToken(http.HandlerFunc(server.handleListModels)))
	mux.Handle("POST /internal/v1/models/{id}/enable", server.requireToken(http.HandlerFunc(server.handleEnableModel)))
	mux.Handle("POST /internal/v1/models/{id}/disable", server.requireToken(http.HandlerFunc(server.handleDisableModel)))
}

func (server *Server) handleListModels(response http.ResponseWriter, request *http.Request) {
	query, err := parseModelPageQuery(request)
	if err != nil {
		writeModelError(response, err)
		return
	}
	page, err := server.modelReader.List(request.Context(), query)
	if err != nil {
		writeModelError(response, err)
		return
	}
	items := make([]provider.ModelDTO, 0, len(page.Items))
	for _, value := range page.Items {
		items = append(items, safeModelDTO(value))
	}
	writeJSON(response, http.StatusOK, modelPageResponse{Data: items, NextCursor: page.NextCursor})
}

func (server *Server) handleEnableModel(response http.ResponseWriter, request *http.Request) {
	server.handleSetModelEnabled(response, request, true)
}

func (server *Server) handleDisableModel(response http.ResponseWriter, request *http.Request) {
	server.handleSetModelEnabled(response, request, false)
}

func (server *Server) handleSetModelEnabled(response http.ResponseWriter, request *http.Request, enabled bool) {
	version, err := decodeModelVersionRequest(request)
	if err != nil {
		writeModelError(response, err)
		return
	}
	var value provider.ModelDTO
	if enabled {
		value, err = server.modelService.Enable(request.Context(), request.PathValue("id"), version)
	} else {
		value, err = server.modelService.Disable(request.Context(), request.PathValue("id"), version)
	}
	if err != nil {
		writeModelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func parseModelPageQuery(request *http.Request) (provider.ModelPageQuery, error) {
	values := request.URL.Query()
	for key, entries := range values {
		if _, supported := map[string]struct{}{"cursor": {}, "page_size": {}, "provider_id": {}, "lifecycle_status": {}, "enabled": {}, "capability": {}, "search": {}}[key]; !supported || len(entries) != 1 {
			return provider.ModelPageQuery{}, provider.ErrInvalidModel
		}
	}
	pageSize, err := parsePageSize(values.Get("page_size"))
	if err != nil {
		return provider.ModelPageQuery{}, err
	}
	query := provider.ModelPageQuery{Cursor: values.Get("cursor"), PageSize: pageSize, ProviderID: values.Get("provider_id"), LifecycleStatus: provider.ModelStatus(values.Get("lifecycle_status")), Capability: values.Get("capability"), Search: values.Get("search")}
	if rawEnabled := values.Get("enabled"); rawEnabled != "" {
		if rawEnabled != "true" && rawEnabled != "false" {
			return provider.ModelPageQuery{}, provider.ErrInvalidModel
		}
		parsed, err := strconv.ParseBool(rawEnabled)
		if err != nil {
			return provider.ModelPageQuery{}, provider.ErrInvalidModel
		}
		query.Enabled = &parsed
	}
	if len(query.Cursor) > 64 || len(query.ProviderID) > 64 || len(query.Search) > 128 || len(query.LifecycleStatus) > 32 || len(query.Capability) > 32 {
		return provider.ModelPageQuery{}, provider.ErrInvalidModel
	}
	if query.LifecycleStatus != "" && query.LifecycleStatus != provider.ModelStatusAvailable && query.LifecycleStatus != provider.ModelStatusDegraded && query.LifecycleStatus != provider.ModelStatusMissingUpstream && query.LifecycleStatus != provider.ModelStatusDisabled {
		return provider.ModelPageQuery{}, provider.ErrInvalidModel
	}
	if query.Capability != "" && query.Capability != "streaming" && query.Capability != "tools" && query.Capability != "parallel_tools" && query.Capability != "reasoning" && query.Capability != "thinking" && query.Capability != "vision" {
		return provider.ModelPageQuery{}, provider.ErrInvalidModel
	}
	return query, nil
}

func decodeModelVersionRequest(request *http.Request) (int64, error) {
	var value struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSONBody(request, &value); err != nil || value.Version < 1 {
		return 0, provider.ErrInvalidModel
	}
	return value.Version, nil
}

func safeModelDTO(value provider.ProviderModel) provider.ModelDTO {
	return provider.ModelDTO{
		ID:                     value.ID,
		ProviderID:             value.ProviderID,
		UpstreamModelID:        value.UpstreamModelID,
		PublicModelID:          value.PublicModelID,
		DisplayName:            value.DisplayName,
		Source:                 value.Source,
		LifecycleStatus:        value.LifecycleStatus,
		Enabled:                value.Enabled,
		Capabilities:           provider.ModelCapabilitiesDTO{Streaming: value.Capabilities.Streaming, Tools: value.Capabilities.Tools, ParallelTools: value.Capabilities.ParallelTools, Reasoning: value.Capabilities.Reasoning, Thinking: value.Capabilities.Thinking, Vision: value.Capabilities.Vision},
		ContextWindowTokens:    cloneModelInt64(value.ContextWindowTokens),
		MaxOutputTokens:        cloneModelInt64(value.MaxOutputTokens),
		CapabilitySource:       value.CapabilitySource,
		CapabilityOverrideJSON: append([]byte(nil), value.CapabilityOverrideJSON...),
		Version:                value.Version,
	}
}

func cloneModelInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func writeModelError(response http.ResponseWriter, err error) {
	code, status := "internal_error", http.StatusInternalServerError
	switch {
	case errors.Is(err, provider.ErrInvalidModel), errors.Is(err, provider.ErrUnsupportedPagination):
		code, status = "invalid_request", http.StatusBadRequest
	case errors.Is(err, provider.ErrModelNotFound):
		code, status = "model_not_found", http.StatusNotFound
	case errors.Is(err, provider.ErrStaleResource):
		code, status = "stale_resource", http.StatusConflict
	}
	writeError(response, status, code, "模型请求未能完成")
}
