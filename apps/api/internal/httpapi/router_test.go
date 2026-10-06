package httpapi_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"reviewpilot.local/api/internal/httpapi"
)

func TestHealthNoSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://secret-value")
	w := httptest.NewRecorder()
	httpapi.NewRouter().ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "foundation") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "DATABASE_URL") || strings.Contains(w.Body.String(), "secret-value") {
		t.Fatal("config leaked")
	}
}
