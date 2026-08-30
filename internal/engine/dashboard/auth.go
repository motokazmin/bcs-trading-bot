package dashboard

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ValidateHTTPListen проверяет, что публичный bind невозможен без токена.
func ValidateHTTPListen(listen, token string) error {
	if listen == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		// допускаем ":8091" — это bind на все интерфейсы
		if strings.HasPrefix(listen, ":") {
			host = ""
		} else {
			return fmt.Errorf("некорректный -http-listen %q: %w", listen, err)
		}
	}
	if isLoopbackHost(host) {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("ADMIN_TOKEN обязателен при -http-listen %q (не localhost)", listen)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "127.0.0.1" || h == "localhost" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func extractBearerOrQueryToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return strings.TrimSpace(r.URL.Query().Get("access_token"))
}

func tokenMatch(got, want string) bool {
	if want == "" {
		return true
	}
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) authorize(r *http.Request) bool {
	return tokenMatch(extractBearerOrQueryToken(r), s.token)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="bcs-admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
