package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/controlplane"
)

type fakeDiagnosticsService struct {
	summary controlplane.DiagnosticsSummary
	export  controlplane.DiagnosticsExport
	err     error
}

func (service fakeDiagnosticsService) Summary(context.Context) (controlplane.DiagnosticsSummary, error) {
	return service.summary, service.err
}

func (service fakeDiagnosticsService) Export(context.Context) (controlplane.DiagnosticsExport, error) {
	return service.export, service.err
}

func TestDiagnosticsEndpointsRequireTokenAndReturnSafeMetadata(t *testing.T) {
	generatedAt := time.Date(2026, 8, 16, 16, 30, 0, 0, time.UTC)
	server, err := controlplane.NewServer(controlplane.Options{
		ManagementToken: testManagementToken,
		Runtime:         func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} },
		Shutdown:        func(context.Context) error { return nil },
		Diagnostics: fakeDiagnosticsService{
			summary: controlplane.DiagnosticsSummary{FormatVersion: "diagnostics/v1", RecentErrorCount: 2, ExportAvailable: true},
			export:  controlplane.DiagnosticsExport{FileName: "aggregation-hub-diagnostics-20260816T163000Z.zip", SizeBytes: 512, GeneratedAt: generatedAt, FormatVersion: "diagnostics/v1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/internal/v1/diagnostics", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("未鉴权 diagnostics 状态=%d", unauthorizedResponse.Code)
	}

	summaryRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/diagnostics", nil)
	summaryRequest.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	summaryResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(summaryResponse, summaryRequest)
	if summaryResponse.Code != http.StatusOK {
		t.Fatalf("diagnostics summary 状态=%d body=%s", summaryResponse.Code, summaryResponse.Body.String())
	}
	var summary controlplane.DiagnosticsSummary
	if err := json.Unmarshal(summaryResponse.Body.Bytes(), &summary); err != nil || summary.RecentErrorCount != 2 || !summary.ExportAvailable {
		t.Fatalf("diagnostics summary=%+v err=%v", summary, err)
	}

	exportRequest := httptest.NewRequest(http.MethodPost, "/internal/v1/diagnostics/export", nil)
	exportRequest.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	exportResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusCreated {
		t.Fatalf("diagnostics export 状态=%d body=%s", exportResponse.Code, exportResponse.Body.String())
	}
	var export controlplane.DiagnosticsExport
	if err := json.Unmarshal(exportResponse.Body.Bytes(), &export); err != nil || export.FileName != "aggregation-hub-diagnostics-20260816T163000Z.zip" || export.SizeBytes != 512 {
		t.Fatalf("diagnostics export=%+v err=%v", export, err)
	}
}

func TestDiagnosticsExportDoesNotExposeInternalFailure(t *testing.T) {
	server, err := controlplane.NewServer(controlplane.Options{
		ManagementToken: testManagementToken,
		Runtime:         func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} },
		Shutdown:        func(context.Context) error { return nil },
		Diagnostics:     fakeDiagnosticsService{err: errors.New("diagnostic-export-secret")},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/diagnostics/export", nil)
	request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("导出失败状态=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.String() == "" || json.Valid(response.Body.Bytes()) == false || strings.Contains(response.Body.String(), "diagnostic-export-secret") {
		t.Fatalf("导出失败泄漏内部错误: %s", response.Body.String())
	}
}
