// Package httpapi keeps control and execution credentials on separate handlers.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/mcpserver"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
)

type Authority interface {
	Accounts(context.Context, string) ([]store.Account, error)
	Catalog(context.Context, string) ([]store.CatalogEntry, error)
	Ready(context.Context) error
	Project(context.Context, string) (string, error)
	Admit(context.Context, string, string) (store.Admission, error)
}

// Handlers returns independent listeners. Unimplemented routes fail explicitly;
// authenticating a session never implies that any tool is implemented.
func Handlers(authority Authority, connect *ConnectAPI) (http.Handler, http.Handler) {
	control := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if connect != nil && strings.HasPrefix(r.URL.Path, "/connect/") {
			connect.browser(w, r)
			return
		}
		if len(r.Header.Values("Authorization")) != 0 || len(r.Header.Values("x-api-key")) != 1 {
			failure(w, 401, "CONTROL_AUTH_REQUIRED")
			return
		}
		project, err := authority.Project(r.Context(), r.Header.Get("x-api-key"))
		if err != nil {
			failure(w, 401, "CONTROL_AUTH_REQUIRED")
			return
		}

		if connect != nil {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) == 7 && parts[1] == "api" && parts[2] == "v3.1" && parts[3] == "internal" && parts[5] != "" {
				if parts[4] == "agents" && parts[6] == "policy" && r.Method == http.MethodPut {
					connect.syncPolicy(w, r, project, parts[5])
					return
				}
				if parts[4] == "tasks" && parts[6] == "revoke" && r.Method == http.MethodPost {
					connect.revokeTask(w, r, project, parts[5])
					return
				}
			}
		}
		if connect != nil && r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/v3.1/tool_router/session":
				connect.createSession(w, r, project)
				return
			case "/api/v3.1/connected_accounts/link":
				connect.create(w, r, project)
				return
			case "/api/v3.1/internal/connect/complete":
				connect.complete(w, r, project)
				return
			}
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/api/v3.1/connected_accounts" || strings.HasPrefix(r.URL.Path, "/api/v3.1/connected_accounts/")) {
			accounts(authority, w, r, project)
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/api/v3.1/toolkits" || r.URL.Path == "/api/v3.1/auth_configs" || strings.HasPrefix(r.URL.Path, "/api/v3.1/toolkits/")) {
			catalog(authority, w, r, project)
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
		if connect != nil {
			(&mcpserver.API{DB: connect.DB, Vault: connect.Vault, Runtimes: connect.Runtimes}).Serve(w, r, parts[2], token)
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
