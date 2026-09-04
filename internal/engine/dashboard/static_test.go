package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticUIEmbedded(t *testing.T) {
	hub := NewHub()
	srv, err := NewServer(hub, Options{Listen: "127.0.0.1:8091"})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	for _, path := range []string{"/", "/open", "/day", "/strategy", "/trades", "/export", "/static/style.css", "/static/app.js", "/static/vendor/lightweight-charts.standalone.production.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("summary without reader: status %d, want 503", rec.Code)
	}
}
