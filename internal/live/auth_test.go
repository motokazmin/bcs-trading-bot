package live

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateHTTPListen(t *testing.T) {
	if err := ValidateHTTPListen("127.0.0.1:8091", ""); err != nil {
		t.Fatalf("localhost without token: %v", err)
	}
	if err := ValidateHTTPListen("0.0.0.0:8091", ""); err == nil {
		t.Fatal("expected error for public bind without token")
	}
	if err := ValidateHTTPListen("0.0.0.0:8091", "secret"); err != nil {
		t.Fatalf("public with token: %v", err)
	}
	if err := ValidateHTTPListen(":8091", ""); err == nil {
		t.Fatal("expected error for :port without token")
	}
	if err := ValidateHTTPListen("", ""); err != nil {
		t.Fatalf("empty listen: %v", err)
	}
}

func TestWithAuth(t *testing.T) {
	s := &Server{token: "secret"}
	h := s.withAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/summary?access_token=secret", nil)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query token: status %d", rec.Code)
	}
}
