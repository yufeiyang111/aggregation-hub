package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/provider"
)

const maxProviderRequestBytes int64 = 64 * 1024

type ProviderService interface {
	Create(context.Context, provider.CreateProviderInput) (provider.ProviderDTO, error)
	Update(context.Context, string, provider.UpdateProviderInput) (provider.ProviderDTO, error)
	Enable(context.Context, string, int64) (provider.ProviderDTO, error)
	Disable(context.Context, string, int64) (provider.ProviderDTO, error)
	Delete(context.Context, string, int64) error
}

type ProviderReader interface {
	FindByID(context.Context, string) (provider.Provider, error)
	List(context.Context, provider.ProviderPageQuery) (provider.ProviderPage, error)
}

type providerRequest struct {
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	AdapterType   string            `json:"adapter_type"`
	AuthType      provider.AuthType `json:"auth_type"`
	BaseURL       string            `json:"base_url"`
	TimeoutMS     int64             `json:"timeout_ms"`
	AdapterConfig json.RawMessage   `json:"adapter_config"`
	Credential    *string           `json:"credential"`
	Version       int64             `json:"version"`
}

type providerPageResponse struct {
	Data       []provider.ProviderDTO `json:"data"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

func (server *Server) registerProviderRoutes(mux *http.ServeMux) {
	mux.Handle("GET /internal/v1/providers", server.requireToken(http.HandlerFunc(server.handleListProviders)))
	mux.Handle("POST /internal/v1/providers", server.requireToken(http.HandlerFunc(server.handleCreateProvider)))
	mux.Handle("GET /internal/v1/providers/{id}", server.requireToken(http.HandlerFunc(server.handleGetProvider)))
	mux.Handle("PATCH /internal/v1/providers/{id}", server.requireToken(http.HandlerFunc(server.handleUpdateProvider)))
	mux.Handle("DELETE /internal/v1/providers/{id}", server.requireToken(http.HandlerFunc(server.handleDeleteProvider)))
	mux.Handle("POST /internal/v1/providers/{id}/enable", server.requireToken(http.HandlerFunc(server.handleEnableProvider)))
	mux.Handle("POST /internal/v1/providers/{id}/disable", server.requireToken(http.HandlerFunc(server.handleDisableProvider)))
}

func (server *Server) handleListProviders(response http.ResponseWriter, request *http.Request) {
	pageSize, err := parsePageSize(request.URL.Query().Get("page_size"))
	if err != nil {
		writeProviderError(response, err)
		return
	}
	page, err := server.providerReader.List(request.Context(), provider.ProviderPageQuery{Cursor: request.URL.Query().Get("cursor"), PageSize: pageSize})
	if err != nil {
		writeProviderError(response, err)
		return
	}
	items := make([]provider.ProviderDTO, 0, len(page.Items))
	for _, value := range page.Items {
		items = append(items, safeProviderDTO(value))
	}
	writeJSON(response, http.StatusOK, providerPageResponse{Data: items, NextCursor: page.NextCursor})
}

func (server *Server) handleCreateProvider(response http.ResponseWriter, request *http.Request) {
	input, err := decodeProviderRequest(request)
	if err != nil {
		writeProviderError(response, err)
		return
	}
	value, err := server.providerService.Create(request.Context(), input.createInput())
	if err != nil {
		writeProviderError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, value)
}

func (server *Server) handleGetProvider(response http.ResponseWriter, request *http.Request) {
	value, err := server.providerReader.FindByID(request.Context(), request.PathValue("id"))
	if err != nil {
		writeProviderError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, safeProviderDTO(value))
}

func (server *Server) handleUpdateProvider(response http.ResponseWriter, request *http.Request) {
	input, err := decodeProviderRequest(request)
	if err != nil {
		writeProviderError(response, err)
		return
	}
	value, err := server.providerService.Update(request.Context(), request.PathValue("id"), input.updateInput())
	if err != nil {
		writeProviderError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleDeleteProvider(response http.ResponseWriter, request *http.Request) {
	version, err := decodeVersionRequest(request)
	if err != nil {
		writeProviderError(response, err)
		return
	}
	if err := server.providerService.Delete(request.Context(), request.PathValue("id"), version); err != nil {
		writeProviderError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleEnableProvider(response http.ResponseWriter, request *http.Request) {
	server.handleSetProviderEnabled(response, request, true)
}

func (server *Server) handleDisableProvider(response http.ResponseWriter, request *http.Request) {
	server.handleSetProviderEnabled(response, request, false)
}

func (server *Server) handleSetProviderEnabled(response http.ResponseWriter, request *http.Request, enabled bool) {
	version, err := decodeVersionRequest(request)
	if err != nil {
		writeProviderError(response, err)
		return
	}
	var value provider.ProviderDTO
	if enabled {
		value, err = server.providerService.Enable(request.Context(), request.PathValue("id"), version)
	} else {
		value, err = server.providerService.Disable(request.Context(), request.PathValue("id"), version)
	}
	if err != nil {
		writeProviderError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func decodeProviderRequest(request *http.Request) (providerRequest, error) {
	var value providerRequest
	if err := decodeJSONBody(request, &value); err != nil {
		return providerRequest{}, err
	}
	if value.TimeoutMS < 1000 || value.TimeoutMS > int64(time.Hour/time.Millisecond) || value.Version < 0 || len(value.CredentialValue()) > 5120 {
		return providerRequest{}, provider.ErrInvalidProvider
	}
	return value, nil
}

func decodeVersionRequest(request *http.Request) (int64, error) {
	var value struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSONBody(request, &value); err != nil {
		return 0, err
	}
	if value.Version < 1 {
		return 0, provider.ErrInvalidProvider
	}
	return value.Version, nil
}

func decodeJSONBody(request *http.Request, target any) error {
	if request.Body == nil || request.ContentLength > maxProviderRequestBytes {
		return provider.ErrInvalidProvider
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, maxProviderRequestBytes+1))
	if err != nil || len(body) > int(maxProviderRequestBytes) {
		return provider.ErrInvalidProvider
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return provider.ErrInvalidProvider
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return provider.ErrInvalidProvider
	}
	return nil
}

func (value providerRequest) CredentialValue() string {
	if value.Credential == nil {
		return ""
	}
	return *value.Credential
}

func (value providerRequest) createInput() provider.CreateProviderInput {
	return provider.CreateProviderInput{
		Slug:              value.Slug,
		Name:              value.Name,
		AdapterType:       value.AdapterType,
		AuthType:          value.AuthType,
		BaseURL:           value.BaseURL,
		Timeout:           time.Duration(value.TimeoutMS) * time.Millisecond,
		AdapterConfigJSON: append(json.RawMessage(nil), value.AdapterConfig...),
		Credential:        secretValue(value.Credential),
	}
}

func (value providerRequest) updateInput() provider.UpdateProviderInput {
	return provider.UpdateProviderInput{
		ExpectedVersion:   value.Version,
		Name:              value.Name,
		BaseURL:           value.BaseURL,
		Timeout:           time.Duration(value.TimeoutMS) * time.Millisecond,
		AdapterConfigJSON: append(json.RawMessage(nil), value.AdapterConfig...),
		Credential:        secretValue(value.Credential),
	}
}

func secretValue(raw *string) *credential.SecretValue {
	if raw == nil {
		return nil
	}
	return &credential.SecretValue{Bytes: []byte(*raw)}
}

func safeProviderDTO(value provider.Provider) provider.ProviderDTO {
	return provider.ProviderDTO{
		ID:              value.ID,
		Slug:            value.Slug,
		Name:            value.Name,
		AdapterType:     value.AdapterType,
		AuthType:        value.AuthType,
		BaseURL:         value.BaseURL,
		LifecycleStatus: value.LifecycleStatus,
		Enabled:         value.Enabled,
		TimeoutMS:       value.Timeout.Milliseconds(),
		Version:         value.Version,
		Credential:      provider.CredentialStateDTO{Configured: value.CredentialRef != nil},
	}
}

func parsePageSize(raw string) (int, error) {
	if raw == "" {
		return 50, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 200 {
		return 0, provider.ErrUnsupportedPagination
	}
	return value, nil
}

func writeProviderError(response http.ResponseWriter, err error) {
	code, status := "internal_error", http.StatusInternalServerError
	switch {
	case errors.Is(err, provider.ErrInvalidProvider), errors.Is(err, provider.ErrUnsupportedPagination):
		code, status = "invalid_request", http.StatusBadRequest
	case errors.Is(err, provider.ErrOAuthNotConfigured):
		code, status = "oauth_not_configured", http.StatusConflict
	case errors.Is(err, provider.ErrProviderNotFound):
		code, status = "provider_not_found", http.StatusNotFound
	case errors.Is(err, provider.ErrDuplicateProvider), errors.Is(err, provider.ErrStaleResource):
		code, status = "stale_resource", http.StatusConflict
	case errors.Is(err, provider.ErrCredentialCleanup):
		code, status = "credential_cleanup_failed", http.StatusInternalServerError
	}
	writeError(response, status, code, "Provider 请求未能完成")
}
