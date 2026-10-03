package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/httpapi"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
)

func TestRealSDKGatewayPostgresCatalogAndCursorIsolation(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO project(id,control_digest,enabled) VALUES('a',$1,true),('b',$2,true)`, credentials.Digest("key-a"), credentials.Digest("key-b"))
	for _, e := range []struct {
		project, id, slug   string
		enabled, configured bool
	}{
		{"a", "ac_github", "github", true, true}, {"a", "ac_slack", "slack", true, true},
		{"a", "ac_disabled", "notion", false, true}, {"a", "ac_unconfigured", "gmail", true, false},
		{"b", "ac_other", "other-provider", true, true},
	} {
		exec(`INSERT INTO auth_config(project_id,id,toolkit,display_name,runtime_id,auth_type,enabled,approved_actions,capabilities) VALUES($1,$2,$3,$3,'runtime','OAUTH2',$4,ARRAY['github.get_current_user'],jsonb_build_object('auth_configured',$5::boolean,'runtime_verified',true))`, e.project, e.id, e.slug, e.enabled, e.configured)
	}
	control, _ := httpapi.Handlers(db, nil)
	server := httptest.NewServer(control)
	t.Cleanup(server.Close)
	newClient := func(key string) *sdk.Client {
		c, err := sdk.NewClient(sdk.Options{APIKey: key, BaseURL: server.URL + "/api/v3.1"})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := newClient("key-a"), newClient("key-b")
	first, err := a.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 1, SortBy: "usage"})
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalItems != 2 || len(first.Items) != 1 || first.Items[0].Slug != "github" || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	second, err := a.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 1, SortBy: "usage", Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Slug != "slack" || second.NextCursor != "" {
		t.Fatalf("second page: %+v", second)
	}
	for name, call := range map[string]func() error{
		"cross project": func() error {
			_, e := b.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 1, SortBy: "usage", Cursor: first.NextCursor})
			return e
		},
		"changed filters": func() error {
			_, e := a.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 2, SortBy: "usage", Cursor: first.NextCursor})
			return e
		},
		"modified cursor": func() error {
			_, e := a.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 1, SortBy: "usage", Cursor: first.NextCursor + "x"})
			return e
		},
	} {
		t.Run(name, func(t *testing.T) {
			var api *sdk.APIError
			if e := call(); !errors.As(e, &api) || api.HTTPStatus != 400 {
				t.Fatalf("cursor accepted: %v", e)
			}
		})
	}

	exec(`INSERT INTO connection(project_id,id,subject_id,auth_config_id,toolkit,runtime_id,native_id,state) VALUES('a','slack-ca','owner','ac_slack','slack','runtime','native-slack','ACTIVE')`)
	exec(`INSERT INTO execution(project_id,id,session_id,connection_id,action_id,fingerprint,state) VALUES('a','op','s','slack-ca','slack.read',$1,'SUCCEEDED')`, credentials.Digest("fixture-input"))
	_, changedErr := a.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 1, SortBy: "usage", Cursor: first.NextCursor})
	var changedAPI *sdk.APIError
	if !errors.As(changedErr, &changedAPI) || changedAPI.Slug != "INVALID_CURSOR" {
		t.Fatalf("usage drift silently changed page: %v", changedErr)
	}
	ranked, rankErr := a.ListToolkits(ctx, sdk.ListToolkitsRequest{Limit: 1, SortBy: "usage"})
	if rankErr != nil || len(ranked.Items) != 1 || ranked.Items[0].Slug != "slack" {
		t.Fatalf("actual usage ordering: %+v %v", ranked, rankErr)
	}
	configs, err := a.ListAuthConfigs(ctx, sdk.ListAuthConfigsRequest{ToolkitSlugs: []string{"github", "gmail"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(configs.Items) != 1 || configs.Items[0].ID != "ac_github" || configs.Items[0].IsComposioManaged || configs.Items[0].Status != "ENABLED" {
		t.Fatalf("config admission: %+v", configs)
	}
	disabled, err := a.ListAuthConfigs(ctx, sdk.ListAuthConfigsRequest{ShowDisabled: true, ToolkitSlugs: []string{"notion"}})
	if err != nil || len(disabled.Items) != 1 || disabled.Items[0].Status != "DISABLED" {
		t.Fatalf("disabled config: %+v %v", disabled, err)
	}
	for _, query := range []string{"toolkit_slug=github&toolkit_slug=slack", "unknown_permission=all", "show_disabled=maybe", "toolkit_slug=%zz"} {
		req, _ := http.NewRequest("GET", server.URL+"/api/v3.1/auth_configs?"+query, nil)
		req.Header.Set("x-api-key", "key-a")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("bad query accepted %q: %d", query, res.StatusCode)
		}
	}
	for _, key := range []string{"key-a", "key-b"} {
		c := newClient(key)
		err := c.RevokeConnection(ctx, "not-implemented")
		var api *sdk.APIError
		if !errors.As(err, &api) || api.HTTPStatus != 501 {
			t.Fatalf("unsupported revoke: %v", err)
		}
	}
	exec(`UPDATE auth_config SET approved_actions=ARRAY[NULL]::text[] WHERE project_id='a' AND id='ac_github'`)
	after, err := a.ListToolkits(ctx, sdk.ListToolkitsRequest{})
	if err != nil || len(after.Items) != 1 || after.Items[0].Slug != "slack" {
		t.Fatalf("NULL action admission: %+v %v", after, err)
	}
	// Public representations must contain neither native identifiers nor keys.
	if strings.Contains(first.NextCursor, "key-a") {
		t.Fatal("cursor leaked control key")
	}
}
