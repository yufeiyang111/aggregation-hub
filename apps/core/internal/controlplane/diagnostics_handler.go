package controlplane

import "net/http"

func (server *Server) registerDiagnosticsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /internal/v1/diagnostics", server.requireToken(http.HandlerFunc(server.handleDiagnostics)))
	mux.Handle("POST /internal/v1/diagnostics/export", server.requireToken(http.HandlerFunc(server.handleDiagnosticsExport)))
}

func (server *Server) handleDiagnostics(response http.ResponseWriter, request *http.Request) {
	summary, err := server.diagnostics.Summary(request.Context())
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "diagnostics_unavailable", "诊断信息暂不可用")
		return
	}
	writeJSON(response, http.StatusOK, summary)
}

func (server *Server) handleDiagnosticsExport(response http.ResponseWriter, request *http.Request) {
	export, err := server.diagnostics.Export(request.Context())
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "diagnostics_export_failed", "诊断包导出失败")
		return
	}
	writeJSON(response, http.StatusCreated, export)
}
