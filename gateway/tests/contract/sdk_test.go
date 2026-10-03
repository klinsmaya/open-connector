package contract_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	sdk "github.com/multica-ai/multica/server/pkg/composio"
)

// These tests freeze real client behavior against explicit fixtures. They do
// not claim gateway admission, provider connectivity or lifecycle acceptance.
func client(t *testing.T, handler http.HandlerFunc) *sdk.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := sdk.NewClient(sdk.Options{APIKey: "fixture-control-key", BaseURL: server.URL + "/api/v3.1"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		t.Error(err)
	}
}

func TestRealSDKCatalogAndAuthConfigWireContract(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "fixture-control-key" {
			t.Error("missing control authentication")
		}
		if r.Method != http.MethodGet {
			t.Error("unexpected method")
		}
		switch r.URL.Path {
		case "/api/v3.1/toolkits":
			if r.URL.Query().Get("cursor") != "page-1" || r.URL.Query().Get("limit") != "1" {
				t.Error("pagination not serialized")
			}
			fixture(t, w, "toolkits")
		case "/api/v3.1/auth_configs":
			q := r.URL.Query()
			if !reflect.DeepEqual(q["toolkit_slug"], []string{"github,slack"}) {
				t.Errorf("auth config slug format: %v", q)
			}
			if q.Get("is_composio_managed") != "false" {
				t.Error("custom auth config filter lost")
			}
			fixture(t, w, "auth-configs")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	kits, err := c.ListToolkits(context.Background(), sdk.ListToolkitsRequest{Limit: 1, Cursor: "page-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(kits.Items) != 1 || kits.Items[0].Slug != "github" || kits.NextCursor != "fixture-page-2" || kits.TotalItems != 2 {
		t.Fatalf("unexpected toolkit response: %+v", kits)
	}
	custom := false
	auth, err := c.ListAuthConfigs(context.Background(), sdk.ListAuthConfigsRequest{ToolkitSlugs: []string{"github", "slack"}, IsComposioManaged: &custom})
	if err != nil {
		t.Fatal(err)
	}
	if len(auth.Items) != 1 || auth.Items[0].ID != "ac_fixture" || auth.Items[0].Status != "ENABLED" || auth.Items[0].IsComposioManaged {
		t.Fatalf("unexpected config: %+v", auth)
	}
}

func TestRealSDKRepeatedAccountFiltersAndOwnershipFields(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3.1/connected_accounts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		for key, want := range map[string][]string{"user_ids": {"owner-a", "owner-b"}, "toolkit_slugs": {"github", "slack"}, "auth_config_ids": {"ac_fixture", "ac_other"}, "connected_account_ids": {"ca_fixture", "ca_other"}, "statuses": {"ACTIVE", "EXPIRED"}} {
			if !reflect.DeepEqual(q[key], want) {
				t.Errorf("%s: got %v, want %v", key, q[key], want)
			}
		}
		fixture(t, w, "accounts")
	})
	out, err := c.ListConnectedAccounts(context.Background(), sdk.ListConnectedAccountsRequest{UserIDs: []string{"owner-a", "owner-b"}, ToolkitSlugs: []string{"github", "slack"}, AuthConfigIDs: []string{"ac_fixture", "ac_other"}, ConnectedAccountIDs: []string{"ca_fixture", "ca_other"}, Statuses: []string{"ACTIVE", "EXPIRED"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatal("missing account")
	}
	a := out.Items[0]
	if a.UserID != "owner-a" || a.AuthConfig.ID != "ac_fixture" || a.AuthConfigID != a.AuthConfig.ID || a.Toolkit.Slug != "github" || a.Status != "ACTIVE" {
		t.Fatalf("ownership fields lost: %+v", a)
	}
}

func TestRealSDKUnsupportedCannotMasqueradeAsDeleteSuccess(t *testing.T) {
	for _, status := range []int{404, 501} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := client(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"Not supported","code":501,"slug":"UNSUPPORTED_CAPABILITY","status":501,"request_id":"req_fixture"}}`))
			})
			err := c.DeleteConnectedAccount(context.Background(), "ca_fixture")
			if status == 404 {
				if err != nil {
					t.Fatal("SDK no longer treats delete 404 as idempotent")
				}
				return
			}
			var apiErr *sdk.APIError
			if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 501 || apiErr.Slug != "UNSUPPORTED_CAPABILITY" || apiErr.RequestID != "req_fixture" {
				t.Fatalf("unsupported error contract: %v", err)
			}
			if err = c.RevokeConnection(context.Background(), "ca_fixture"); err == nil {
				t.Fatal("unsupported revoke was reported successful")
			}
		})
	}
}

func TestRealSDKRejectsMalformedSuccessJSON(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":`))
	})
	if _, err := c.ListToolkits(context.Background(), sdk.ListToolkitsRequest{}); err == nil {
		t.Fatal("malformed success body accepted")
	}
}
