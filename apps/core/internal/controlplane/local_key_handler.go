package controlplane

import (
	"context"
	"errors"
	"net/http"
	"time"

	"aggregationhub.local/core/internal/security"
)

type LocalKeyService interface {
	Create(context.Context, string, *time.Time) (string, security.LocalKeyRecord, error)
}

type localKeyCreateRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type localKeyCreateResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	Suffix      string     `json:"suffix"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Key         string     `json:"key"`
	DisplayOnce bool       `json:"display_once"`
}

func (server *Server) registerLocalKeyRoutes(mux *http.ServeMux) {
	mux.Handle("POST /internal/v1/local-keys", server.requireToken(http.HandlerFunc(server.handleCreateLocalKey)))
}

func (server *Server) handleCreateLocalKey(response http.ResponseWriter, request *http.Request) {
	var input localKeyCreateRequest
	if err := decodeJSONBody(request, &input); err != nil {
		writeLocalKeyError(response, err)
		return
	}
	plaintext, record, err := server.localKeyService.Create(request.Context(), input.Name, input.ExpiresAt)
	if err != nil {
		writeLocalKeyError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, localKeyCreateResponse{
		ID:          record.ID,
		Name:        record.Name,
		Prefix:      record.Prefix,
		Suffix:      record.Suffix,
		CreatedAt:   record.CreatedAt,
		ExpiresAt:   record.ExpiresAt,
		Key:         plaintext,
		DisplayOnce: true,
	})
}

func writeLocalKeyError(response http.ResponseWriter, err error) {
	code, status := "internal_error", http.StatusInternalServerError
	switch {
	case errors.Is(err, security.ErrInvalidKeyName), errors.Is(err, security.ErrInvalidKeyExpiration), errors.Is(err, security.ErrInvalidLocalKeyStore):
		code, status = "invalid_request", http.StatusBadRequest
	}
	writeError(response, status, code, "Local Access Key 请求未能完成")
}
