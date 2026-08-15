package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"aggregationhub.local/core/internal/provider"
)

type ModelService interface {
	Enable(context.Context, string, int64) (provider.ModelDTO, error)
	Disable(context.Context, string, int64) (provider.ModelDTO, error)
	UpdateCapabilities(context.Context, string, provider.UpdateModelCapabilitiesInput) (provider.ModelDTO, error)
	UpdateLimits(context.Context, string, provider.UpdateModelLimitsInput) (provider.ModelDTO, error)
	CreateManual(context.Context, provider.CreateManualModelInput) (provider.ModelDTO, error)
	DeleteManual(context.Context, string, int64) error
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
	mux.Handle("POST /internal/v1/providers/{providerId}/models", server.requireToken(http.HandlerFunc(server.handleCreateManualModel)))
	mux.Handle("PATCH /internal/v1/models/{id}", server.requireToken(http.HandlerFunc(server.handleUpdateModelCapabilities)))
	mux.Handle("PATCH /internal/v1/models/{id}/limits", server.requireToken(http.HandlerFunc(server.handleUpdateModelLimits)))
	mux.Handle("DELETE /internal/v1/models/{id}", server.requireToken(http.HandlerFunc(server.handleDeleteManualModel)))
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

type manualModelRequest struct {
	UpstreamModelID     string          `json:"upstream_model_id"`
	DisplayName         string          `json:"display_name"`
	Capabilities        json.RawMessage `json:"capabilities"`
	ContextWindowTokens *int64          `json:"context_window_tokens"`
	MaxOutputTokens     *int64          `json:"max_output_tokens"`
}

func (server *Server) handleCreateManualModel(response http.ResponseWriter, request *http.Request) {
	var input manualModelRequest
	if err := decodeJSONBody(request, &input); err != nil {
		writeModelError(response, err)
		return
	}
	capabilities, err := decodeManualCapabilities(input.Capabilities)
	if err != nil {
		writeModelError(response, provider.ErrInvalidModel)
		return
	}
	value, err := server.modelService.CreateManual(request.Context(), provider.CreateManualModelInput{ProviderID: request.PathValue("providerId"), UpstreamModelID: input.UpstreamModelID, DisplayName: input.DisplayName, Capabilities: capabilities, ContextWindowTokens: input.ContextWindowTokens, MaxOutputTokens: input.MaxOutputTokens})
	if err != nil {
		writeModelError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, value)
}

func decodeManualCapabilities(raw json.RawMessage) (provider.Capabilities, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return provider.Capabilities{}, provider.ErrInvalidModel
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return provider.Capabilities{}, provider.ErrInvalidModel
	}
	values := make(map[string]bool, 6)
	known := map[string]struct{}{"streaming": {}, "tools": {}, "parallel_tools": {}, "reasoning": {}, "thinking": {}, "vision": {}}
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return provider.Capabilities{}, provider.ErrInvalidModel
		}
		if _, ok := known[key]; !ok {
			return provider.Capabilities{}, provider.ErrInvalidModel
		}
		if _, duplicate := values[key]; duplicate {
			return provider.Capabilities{}, provider.ErrInvalidModel
		}
		var encoded json.RawMessage
		if err := decoder.Decode(&encoded); err != nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
			return provider.Capabilities{}, provider.ErrInvalidModel
		}
		var value bool
		if err := json.Unmarshal(encoded, &value); err != nil {
			return provider.Capabilities{}, provider.ErrInvalidModel
		}
		values[key] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(values) != len(known) {
		return provider.Capabilities{}, provider.ErrInvalidModel
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return provider.Capabilities{}, provider.ErrInvalidModel
	}
	return provider.Capabilities{Streaming: values["streaming"], Tools: values["tools"], ParallelTools: values["parallel_tools"], Reasoning: values["reasoning"], Thinking: values["thinking"], Vision: values["vision"]}, nil
}

func (server *Server) handleUpdateModelCapabilities(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Version            int64                        `json:"version"`
		CapabilityOverride *provider.CapabilityOverride `json:"capability_override"`
	}
	if err := decodeJSONBody(request, &input); err != nil || input.Version < 1 || input.CapabilityOverride == nil {
		writeModelError(response, provider.ErrInvalidModel)
		return
	}
	value, err := server.modelService.UpdateCapabilities(request.Context(), request.PathValue("id"), provider.UpdateModelCapabilitiesInput{ExpectedVersion: input.Version, CapabilityOverride: *input.CapabilityOverride})
	if err != nil {
		writeModelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleUpdateModelLimits(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Version       int64                        `json:"version"`
		LimitOverride *provider.ModelLimitOverride `json:"limit_override"`
	}
	if err := decodeJSONBody(request, &input); err != nil || input.Version < 1 || input.LimitOverride == nil {
		writeModelError(response, provider.ErrInvalidModel)
		return
	}
	value, err := server.modelService.UpdateLimits(request.Context(), request.PathValue("id"), provider.UpdateModelLimitsInput{ExpectedVersion: input.Version, LimitOverride: *input.LimitOverride})
	if err != nil {
		writeModelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleDeleteManualModel(response http.ResponseWriter, request *http.Request) {
	version, err := decodeModelVersionRequest(request)
	if err != nil {
		writeModelError(response, err)
		return
	}
	if err := server.modelService.DeleteManual(request.Context(), request.PathValue("id"), version); err != nil {
		writeModelError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
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
	override, err := provider.ParseCapabilityOverride(value.CapabilityOverrideJSON)
	if err != nil {
		override = provider.CapabilityOverride{}
	}
	effective, err := provider.EffectiveCapabilities(value.Capabilities, value.CapabilityOverrideJSON)
	if err != nil {
		effective = value.Capabilities
	}
	effectiveContextWindow, effectiveMaxOutput := provider.EffectiveModelLimits(value.ContextWindowTokens, value.MaxOutputTokens, value.ContextWindowOverrideTokens, value.MaxOutputOverrideTokens)
	return provider.ModelDTO{
		ID:                  value.ID,
		ProviderID:          value.ProviderID,
		UpstreamModelID:     value.UpstreamModelID,
		PublicModelID:       value.PublicModelID,
		DisplayName:         value.DisplayName,
		Source:              value.Source,
		LifecycleStatus:     value.LifecycleStatus,
		Enabled:             value.Enabled,
		Capabilities:        provider.ModelCapabilitiesDTO{Streaming: effective.Streaming, Tools: effective.Tools, ParallelTools: effective.ParallelTools, Reasoning: effective.Reasoning, Thinking: effective.Thinking, Vision: effective.Vision},
		ContextWindowTokens: cloneModelInt64(effectiveContextWindow),
		MaxOutputTokens:     cloneModelInt64(effectiveMaxOutput),
		CapabilitySource:    value.CapabilitySource,
		CapabilityOverride:  override,
		LimitOverride:       provider.ModelLimitOverride{ContextWindowTokens: cloneModelInt64(value.ContextWindowOverrideTokens), MaxOutputTokens: cloneModelInt64(value.MaxOutputOverrideTokens)},
		Version:             value.Version,
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
	case errors.Is(err, provider.ErrInvalidModel), errors.Is(err, provider.ErrInvalidCapabilityOverride), errors.Is(err, provider.ErrInvalidModelLimitOverride), errors.Is(err, provider.ErrUnsupportedPagination):
		code, status = "invalid_request", http.StatusBadRequest
	case errors.Is(err, provider.ErrModelNotFound):
		code, status = "model_not_found", http.StatusNotFound
	case errors.Is(err, provider.ErrStaleResource):
		code, status = "stale_resource", http.StatusConflict
	}
	writeError(response, status, code, "模型请求未能完成")
}
