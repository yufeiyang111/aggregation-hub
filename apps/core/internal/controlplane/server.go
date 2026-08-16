package controlplane

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ManagementTokenHeader = "X-Aggregation-Hub-Management-Token"

type RuntimeStatus struct {
	State        string  `json:"state"`
	DataPlaneURL string  `json:"data_plane_url"`
	StartedAt    string  `json:"started_at"`
	Version      string  `json:"version"`
	LastError    *string `json:"last_error"`
}

type RuntimeSource func() RuntimeStatus

type Server struct {
	managementToken    []byte
	runtime            RuntimeSource
	shutdown           func(context.Context) error
	providerService    ProviderService
	providerReader     ProviderReader
	localKeyService    LocalKeyService
	providerOperations ProviderOperations
	providerHealth     ProviderHealthReader
	modelService       ModelService
	modelReader        ModelReader
	diagnostics        DiagnosticsService
	requestReader      RequestReader
	usageReader        UsageReader
	maintenance        MaintenanceService
	shutdownOnce       sync.Once
	shutdownError      error
}

type Options struct {
	ManagementToken    string
	Runtime            RuntimeSource
	Shutdown           func(context.Context) error
	ProviderService    ProviderService
	ProviderReader     ProviderReader
	LocalKeyService    LocalKeyService
	ProviderOperations ProviderOperations
	ProviderHealth     ProviderHealthReader
	ModelService       ModelService
	ModelReader        ModelReader
	Diagnostics        DiagnosticsService
	RequestReader      RequestReader
	UsageReader        UsageReader
	Maintenance        MaintenanceService
}

func NewServer(options Options) (*Server, error) {
	if len(options.ManagementToken) < 32 || options.Runtime == nil || options.Shutdown == nil {
		return nil, errors.New("Control Plane 依赖无效")
	}
	return &Server{managementToken: []byte(options.ManagementToken), runtime: options.Runtime, shutdown: options.Shutdown, providerService: options.ProviderService, providerReader: options.ProviderReader, localKeyService: options.LocalKeyService, providerOperations: options.ProviderOperations, providerHealth: options.ProviderHealth, modelService: options.ModelService, modelReader: options.ModelReader, diagnostics: options.Diagnostics, requestReader: options.RequestReader, usageReader: options.UsageReader, maintenance: options.Maintenance}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /internal/v1/runtime", server.requireToken(http.HandlerFunc(server.handleRuntime)))
	mux.Handle("POST /internal/v1/runtime/shutdown", server.requireToken(http.HandlerFunc(server.handleShutdown)))
	if server.providerService != nil && server.providerReader != nil {
		server.registerProviderRoutes(mux)
		if server.providerOperations != nil {
			server.registerProviderOperationRoutes(mux)
		}
		if server.providerHealth != nil {
			server.registerProviderHealthRoutes(mux)
		}
	}
	if server.localKeyService != nil {
		server.registerLocalKeyRoutes(mux)
	}
	if server.modelService != nil && server.modelReader != nil {
		server.registerModelRoutes(mux)
	}
	if server.requestReader != nil && server.usageReader != nil {
		server.registerObservabilityRoutes(mux)
	}
	if server.diagnostics != nil {
		server.registerDiagnosticsRoutes(mux)
	}
	if server.maintenance != nil {
		server.registerMaintenanceRoutes(mux)
	}
	return mux
}

func (server *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := request.Header.Get(ManagementTokenHeader)
		if token == "" || subtle.ConstantTimeCompare([]byte(token), server.managementToken) != 1 {
			writeError(response, http.StatusUnauthorized, "invalid_management_token", "管理令牌无效")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (server *Server) handleRuntime(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, server.runtime())
}

func (server *Server) handleShutdown(response http.ResponseWriter, request *http.Request) {
	server.shutdownOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.shutdownError = server.shutdown(ctx)
	})
	if server.shutdownError != nil {
		writeError(response, http.StatusInternalServerError, "shutdown_failed", "Core 无法正常停止")
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
func writeError(response http.ResponseWriter, status int, code string, message string) {
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": strings.TrimSpace(message)}})
}
