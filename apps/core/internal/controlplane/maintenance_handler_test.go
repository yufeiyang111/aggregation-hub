package controlplane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/controlplane"
	"aggregationhub.local/core/internal/maintenance"
	"aggregationhub.local/core/internal/storage"
)

type fakeMaintenanceService struct {
	settings      maintenance.RuntimeSettings
	updateInput   maintenance.UpdateRuntimeSettingsInput
	updateResult  maintenance.UpdateRuntimeSettingsResult
	backups       []storage.BackupRecord
	pruneResult   storage.RetentionResult
	restoreID     string
	err           error
	settingsCalls int
}

func (service *fakeMaintenanceService) Settings(context.Context) (maintenance.RuntimeSettings, error) {
	service.settingsCalls++
	return service.settings, service.err
}
func (service *fakeMaintenanceService) UpdateSettings(_ context.Context, input maintenance.UpdateRuntimeSettingsInput) (maintenance.UpdateRuntimeSettingsResult, error) {
	service.updateInput = input
	return service.updateResult, service.err
}
func (service *fakeMaintenanceService) PruneRequests(context.Context) (storage.RetentionResult, error) {
	return service.pruneResult, service.err
}
func (service *fakeMaintenanceService) CreateBackup(context.Context) (storage.BackupRecord, error) {
	if len(service.backups) == 0 {
		return storage.BackupRecord{}, service.err
	}
	return service.backups[0], service.err
}
func (service *fakeMaintenanceService) ListBackups(context.Context) ([]storage.BackupRecord, error) {
	return service.backups, service.err
}
func (service *fakeMaintenanceService) ScheduleRestore(_ context.Context, identifier string) (storage.RestoreSchedule, error) {
	service.restoreID = identifier
	return storage.RestoreSchedule{SafetyBackup: storage.BackupRecord{ID: "backup-safety"}, RestartRequired: true}, service.err
}

func newMaintenanceServer(t *testing.T, service *fakeMaintenanceService) *controlplane.Server {
	t.Helper()
	server, err := controlplane.NewServer(controlplane.Options{
		ManagementToken: testManagementToken,
		Runtime:         func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} },
		Shutdown:        func(context.Context) error { return nil },
		Maintenance:     service,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func maintenanceRequest(method string, target string, body string, token bool) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token {
		request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	}
	return request
}

func TestMaintenanceRoutesRequireTokenAndUseSafeContracts(t *testing.T) {
	service := &fakeMaintenanceService{
		settings:     maintenance.RuntimeSettings{ListenPort: 18443, RequestTimeoutMS: 60000, RequestRetentionDays: 30, Version: 2},
		backups:      []storage.BackupRecord{{ID: "backup-20260816t120000.000z-abc", CreatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), SizeBytes: 512}},
		pruneResult:  storage.RetentionResult{DeletedRequests: 4, Batches: 1},
		updateResult: maintenance.UpdateRuntimeSettingsResult{Settings: maintenance.RuntimeSettings{ListenPort: 19443, RequestTimeoutMS: 60000, RequestRetentionDays: 30, Version: 3}, RestartRequired: true},
	}
	server := newMaintenanceServer(t, service)
	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, maintenanceRequest(http.MethodGet, "/internal/v1/settings", "", false))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("未授权设置请求 status=%d", unauthorized.Code)
	}

	settingsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(settingsResponse, maintenanceRequest(http.MethodGet, "/internal/v1/settings", "", true))
	if settingsResponse.Code != http.StatusOK || strings.Contains(settingsResponse.Body.String(), "127.0.0.1") {
		t.Fatalf("设置响应不安全: %d %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	updateResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateResponse, maintenanceRequest(http.MethodPatch, "/internal/v1/settings", `{"listen_port":19443,"request_timeout_ms":60000,"request_retention_days":30,"version":2}`, true))
	if updateResponse.Code != http.StatusOK || service.updateInput.ListenPort != 19443 || !strings.Contains(updateResponse.Body.String(), `"restart_required":true`) {
		t.Fatalf("更新设置响应错误: status=%d input=%+v body=%s", updateResponse.Code, service.updateInput, updateResponse.Body.String())
	}

	backupsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(backupsResponse, maintenanceRequest(http.MethodGet, "/internal/v1/maintenance/backups", "", true))
	if backupsResponse.Code != http.StatusOK || strings.Contains(backupsResponse.Body.String(), "\\") {
		t.Fatalf("备份列表响应错误: %d %s", backupsResponse.Code, backupsResponse.Body.String())
	}
	pruneResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(pruneResponse, maintenanceRequest(http.MethodPost, "/internal/v1/maintenance/prune", "", true))
	if pruneResponse.Code != http.StatusOK || !strings.Contains(pruneResponse.Body.String(), `"deleted_requests":4`) {
		t.Fatalf("清理响应错误: %d %s", pruneResponse.Code, pruneResponse.Body.String())
	}
	restoreResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(restoreResponse, maintenanceRequest(http.MethodPost, "/internal/v1/maintenance/restore", `{"backup_id":"backup-20260816t120000.000z-abc"}`, true))
	if restoreResponse.Code != http.StatusAccepted || service.restoreID == "" || !strings.Contains(restoreResponse.Body.String(), `"restart_required":true`) {
		t.Fatalf("恢复响应错误: %d id=%s body=%s", restoreResponse.Code, service.restoreID, restoreResponse.Body.String())
	}
}

func TestMaintenanceRoutesRejectInvalidInputAndReportConflict(t *testing.T) {
	service := &fakeMaintenanceService{err: storage.ErrStaleSettings}
	server := newMaintenanceServer(t, service)
	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, maintenanceRequest(http.MethodPatch, "/internal/v1/settings", `{"listen_port":18443,"unknown":true}`, true))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("未知字段 status=%d", invalid.Code)
	}
	conflict := httptest.NewRecorder()
	server.Handler().ServeHTTP(conflict, maintenanceRequest(http.MethodPatch, "/internal/v1/settings", `{"listen_port":18443,"request_timeout_ms":60000,"request_retention_days":30,"version":0}`, true))
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "settings_conflict") {
		t.Fatalf("设置冲突响应=%d %s", conflict.Code, conflict.Body.String())
	}
	service.err = storage.ErrInvalidBackupID
	invalidRestore := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidRestore, maintenanceRequest(http.MethodPost, "/internal/v1/maintenance/restore", `{"backup_id":"../outside"}`, true))
	if invalidRestore.Code != http.StatusBadRequest || !strings.Contains(invalidRestore.Body.String(), "invalid_restore") {
		t.Fatalf("非法恢复标识响应=%d %s", invalidRestore.Code, invalidRestore.Body.String())
	}
	if service.restoreID != "../outside" {
		t.Fatal("恢复标识未传入维护服务")
	}
}
