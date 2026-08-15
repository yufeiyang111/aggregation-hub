package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aggregationhub.local/core/internal/health"
)

func TestHandlerReturnsMinimalHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	health.NewHandler("0.1.0-rc.3").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type=%q", got)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] != "0.1.0-rc.3" || body["data_plane"] != "ready" {
		t.Fatalf("unexpected body: %#v", body)
	}
	if len(body) != 3 {
		t.Fatalf("health leaked fields: %#v", body)
	}
}

func TestHandlerRejectsNonGet(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			rec := httptest.NewRecorder()

			health.NewHandler("0.1.0-rc.3").ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d", rec.Code)
			}
		})
	}
}
