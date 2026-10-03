package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klinsmaya/open-connector/gateway/internal/store"
)

type authority struct{}

func (authority) Catalog(context.Context, string) ([]store.CatalogEntry, error) { return nil, nil }

func (authority) Ready(context.Context) error { return nil }

func (authority) Project(_ context.Context, t string) (string, error) {
	if t == "control-fixture" {
		return "p", nil
	}
	return "", store.ErrDenied
}
func (authority) Admit(_ context.Context, id, t string) (store.Admission, error) {
	if id == "s" && t == "session-fixture" {
		return store.Admission{ProjectID: "p"}, nil
	}
	return store.Admission{}, store.ErrDenied
}

func TestCredentialClassesCannotCrossListeners(t *testing.T) {
	control, data := Handlers(authority{})
	for _, tc := range []struct {
		name        string
		handler     http.Handler
		key, bearer string
		status      int
	}{
		{"control key", control, "control-fixture", "", 501},
		{"session cannot control", control, "", "session-fixture", 401},
		{"session as key", control, "session-fixture", "", 401},
		{"control cannot execute", data, "control-fixture", "", 401},
		{"control as bearer", data, "", "control-fixture", 401},
		{"mixed credentials", data, "control-fixture", "session-fixture", 401},
		{"valid session unsupported", data, "", "session-fixture", 501},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/sessions/s/mcp", nil)
			if tc.key != "" {
				req.Header.Set("x-api-key", tc.key)
			}
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			b, _ := io.ReadAll(w.Result().Body)
			for _, s := range []string{"control-fixture", "session-fixture"} {
				if strings.Contains(string(b), s) {
					t.Fatal("credential leaked")
				}
			}
		})
	}
}
