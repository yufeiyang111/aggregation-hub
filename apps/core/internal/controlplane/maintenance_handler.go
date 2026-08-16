package controlplane

import (
	"context"
	"errors"
	"net/http"

	"aggregationhub.local/core/internal/maintenance"
	"aggregationhub.local/core/internal/storage"
)

type MaintenanceService interface {
	Settings(context.Context) (maintenance.RuntimeSettings, error)
	UpdateSettings(context.Context, maintenance.UpdateRuntimeSettingsInput) (maintenance.UpdateRuntimeSettingsResult, error)
	PruneRequests(context.Context) (storage.RetentionResult, error)
	CreateBackup(context.Context) (storage.BackupRecord, error)
	ListBackups(context.Context) ([]storage.BackupRecord, error)
	ScheduleRestore(context.Context, string) (storage.RestoreSchedule, error)
}

func (server *Server) registerMaintenanceRoutes(mux *http.ServeMux) {
	mux.Handle("GET /internal/v1/settings", server.requireToken(http.HandlerFunc(server.handleSettings)))
	mux.Handle("PATCH /internal/v1/settings", server.requireToken(http.HandlerFunc(server.handleUpdateSettings)))
	mux.Handle("POST /internal/v1/maintenance/prune", server.requireToken(http.HandlerFunc(server.handlePruneRequests)))
	mux.Handle("GET /internal/v1/maintenance/backups", server.requireToken(http.HandlerFunc(server.handleListBackups)))
	mux.Handle("POST /internal/v1/maintenance/backups", server.requireToken(http.HandlerFunc(server.handleCreateBackup)))
	mux.Handle("POST /internal/v1/maintenance/restore", server.requireToken(http.HandlerFunc(server.handleScheduleRestore)))
}

func (server *Server) handleSettings(response http.ResponseWriter, request *http.Request) {
	value, err := server.maintenance.Settings(request.Context())
	if err != nil {
		writeMaintenanceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleUpdateSettings(response http.ResponseWriter, request *http.Request) {
	var input maintenance.UpdateRuntimeSettingsInput
	if err := decodeJSONBody(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_settings", "设置参数无效")
		return
	}
	value, err := server.maintenance.UpdateSettings(request.Context(), input)
	if err != nil {
		writeMaintenanceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handlePruneRequests(response http.ResponseWriter, request *http.Request) {
	value, err := server.maintenance.PruneRequests(request.Context())
	if err != nil {
		writeMaintenanceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (server *Server) handleListBackups(response http.ResponseWriter, request *http.Request) {
	value, err := server.maintenance.ListBackups(request.Context())
	if err != nil {
		writeMaintenanceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"data": value})
}

func (server *Server) handleCreateBackup(response http.ResponseWriter, request *http.Request) {
	value, err := server.maintenance.CreateBackup(request.Context())
	if err != nil {
		writeMaintenanceError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, value)
}

func (server *Server) handleScheduleRestore(response http.ResponseWriter, request *http.Request) {
	var input struct {
		BackupID string `json:"backup_id"`
	}
	if err := decodeJSONBody(request, &input); err != nil || input.BackupID == "" || len(input.BackupID) > 100 {
		writeError(response, http.StatusBadRequest, "invalid_restore", "恢复备份无效")
		return
	}
	value, err := server.maintenance.ScheduleRestore(request.Context(), input.BackupID)
	if err != nil {
		writeMaintenanceError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, value)
}

func writeMaintenanceError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, maintenance.ErrInvalidRuntimeSettings):
		writeError(response, http.StatusBadRequest, "invalid_settings", "设置参数无效")
	case errors.Is(err, storage.ErrStaleSettings):
		writeError(response, http.StatusConflict, "settings_conflict", "设置已被更新，请刷新后重试")
	case errors.Is(err, storage.ErrInvalidBackupID):
		writeError(response, http.StatusBadRequest, "invalid_restore", "恢复备份无效")
	default:
		writeError(response, http.StatusServiceUnavailable, "maintenance_unavailable", "维护操作暂不可用")
	}
}
