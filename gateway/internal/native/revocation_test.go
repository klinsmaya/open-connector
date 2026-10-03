package native

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeRevocationRequiresExplicitConfirmedOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unsupported", 501, `{"errorCode":"revocation_unsupported"}`, ErrUnsupported},
		{"unknown", 409, `{"errorCode":"revocation_unknown"}`, ErrBlocked},
		{"missing", 404, `{}`, ErrNotFound},
		{"network failure", 503, `{"secret":"must-not-escape"}`, ErrUpstream},
		{"false success", 200, `{"success":true,"data":{"connectionId":"ca","state":"UNKNOWN"}}`, ErrUpstream},
		{"confirmed", 200, `{"success":true,"data":{"connectionId":"ca","state":"REVOKED"}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/connections/by-id/ca/revoke" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-admin" {
					t.Error("unsafe native revoke request")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := New(server.URL, "fixture-admin", true)
			if err != nil {
				t.Fatal(err)
			}
			if err = client.RevokeConnection(context.Background(), "ca"); !errors.Is(err, tc.want) {
				t.Fatalf("outcome %v", err)
			}
		})
	}
}
func TestNativeDowngradeDoesNotSatisfySecurityCapabilities(t *testing.T) {
	for _, body := range []string{`{}`, `{"success":true,"data":{"profile":"multica-core-v1","securityRevision":1,"trustedSubjectRequests":true,"nonDestructiveRevocation":true}}`, `{"success":true,"data":{"profile":"multica-core-v1","securityRevision":2,"trustedSubjectRequests":false,"nonDestructiveRevocation":true}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		client, _ := New(server.URL, "fixture-admin", true)
		if client.CheckCompatibility(context.Background()) == nil {
			t.Error("unsafe native downgrade accepted")
		}
		server.Close()
	}
}
