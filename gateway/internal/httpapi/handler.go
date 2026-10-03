// Package httpapi keeps control and execution credentials on separate handlers.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
)

type Authority interface {
	Ready(context.Context) error
	Project(context.Context, string) (string, error)
	Admit(context.Context, string, string) (store.Admission, error)
}

// Handlers returns independent listeners. Unimplemented routes fail explicitly;
// authenticating a session never implies that any tool is implemented.
func Handlers(authority Authority) (http.Handler, http.Handler) {
	control := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Authorization")) != 0 || len(r.Header.Values("x-api-key")) != 1 {
			failure(w, 401, "CONTROL_AUTH_REQUIRED")
			return
		}
		if _, err := authority.Project(r.Context(), r.Header.Get("x-api-key")); err != nil {
			failure(w, 401, "CONTROL_AUTH_REQUIRED")
			return
		}
		failure(w, 501, "UNSUPPORTED_CAPABILITY")
	})
	execution := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("x-api-key")) != 0 || len(r.Header.Values("Authorization")) != 1 {
			failure(w, 401, "SESSION_AUTH_REQUIRED")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.ContainsAny(token, " \t\r\n,") || token == "" {
			failure(w, 401, "SESSION_AUTH_REQUIRED")
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 4 || parts[1] != "sessions" || parts[2] == "" || parts[3] != "mcp" {
			failure(w, 404, "NOT_FOUND")
			return
		}
		if _, err := authority.Admit(r.Context(), parts[2], token); err != nil {
			failure(w, 401, "SESSION_AUTH_REQUIRED")
			return
		}
		failure(w, 501, "UNSUPPORTED_CAPABILITY")
	})
	return bounded(health(authority, control)), bounded(health(authority, execution))
}

func bounded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func failure(w http.ResponseWriter, status int, slug string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": http.StatusText(status), "status": status, "code": status, "slug": slug, "request_id": "req_" + credentials.NewToken()}})
}

func health(authority Authority, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			if err := authority.Ready(r.Context()); err != nil {
				failure(w, 503, "NOT_READY")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
