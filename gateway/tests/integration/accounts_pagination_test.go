package integration_test

import (
	"context"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/httpapi"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccountPaginationFiltersAndCursorIsolation(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	run := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	run(`INSERT INTO project(id,control_digest,enabled) VALUES('a',$1,true),('b',$2,true)`, credentials.Digest("key-a"), credentials.Digest("key-b"))
	run(`INSERT INTO subject(project_id,id) VALUES('a','one'),('a','two'),('b','one')`)
	for _, p := range []string{"a", "b"} {
		for _, slug := range []string{"example", "other"} {
			run(`INSERT INTO auth_config(project_id,id,toolkit,display_name,runtime_id,auth_type,enabled,approved_actions,capabilities) VALUES($1,$2,$2,$2,'runtime','OAUTH2',true,ARRAY['example.read'],'{"auth_configured":true,"runtime_verified":true}')`, p, slug)
		}
	}
	for _, v := range [][4]string{{"a", "a1", "one", "example"}, {"a", "a2", "two", "example"}, {"a", "a3", "one", "other"}, {"b", "b1", "one", "example"}} {
		run(`INSERT INTO connection(project_id,id,subject_id,auth_config_id,toolkit,runtime_id,native_id,state) VALUES($1,$2,$3,$4,$4,'runtime',$2,'ACTIVE')`, v[0], v[1], v[2], v[3])
	}
	control, _ := httpapi.Handlers(db, nil)
	server := httptest.NewServer(control)
	defer server.Close()
	client := func(key string) *sdk.Client {
		c, e := sdk.NewClient(sdk.Options{APIKey: key, BaseURL: server.URL + "/api/v3.1"})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	a, b := client("key-a"), client("key-b")
	for _, route := range []string{"/tools/execute", "/tools/proxy", "/workbench", "/remote_bash", "/triggers"} {
		req, _ := http.NewRequest("POST", server.URL+"/api/v3.1"+route, nil)
		req.Header.Set("x-api-key", "key-a")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 501 {
			t.Fatalf("unsupported %s: %d", route, res.StatusCode)
		}
	}

	filter := sdk.ListConnectedAccountsRequest{UserIDs: []string{"two", "one"}, ToolkitSlugs: []string{"example"}, AuthConfigIDs: []string{"example"}, Limit: 1}
	first, e := a.ListConnectedAccounts(ctx, filter)
	if e != nil || first.TotalItems != 2 || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("filtered page: %+v %v", first, e)
	}
	filter.Cursor = first.NextCursor
	filter.UserIDs = []string{"one", "two"}
	second, e := a.ListConnectedAccounts(ctx, filter)
	if e != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID || second.NextCursor != "" {
		t.Fatalf("second page: %+v %v", second, e)
	}
	if _, e = b.ListConnectedAccounts(ctx, filter); e == nil {
		t.Fatal("cross-project cursor accepted")
	}
	wrong := filter
	wrong.AuthConfigIDs = []string{"other"}
	if _, e = a.ListConnectedAccounts(ctx, wrong); e == nil {
		t.Fatal("changed auth config cursor accepted")
	}
	duplicate := filter
	duplicate.Cursor = ""
	duplicate.UserIDs = []string{"one", "one"}
	duplicate.AuthConfigIDs = []string{"example", "other"}
	duplicate.ToolkitSlugs = []string{"example"}
	duplicate.ConnectedAccountIDs = []string{"a1", "a3"}
	selected, e := a.ListConnectedAccounts(ctx, duplicate)
	if e != nil || selected.TotalItems != 1 || len(selected.Items) != 1 || selected.Items[0].ID != "a1" {
		t.Fatalf("array intersection: %+v %v", selected, e)
	}
	run(`UPDATE subject SET enabled=false WHERE project_id='a' AND id='two'`)
	if _, e = a.ListConnectedAccounts(ctx, filter); e == nil {
		t.Fatal("policy-changed cursor accepted")
	}
	filter.Cursor = ""
	filter.Statuses = []string{"ACTIVE"}
	active, e := a.ListConnectedAccounts(ctx, filter)
	if e != nil || active.TotalItems != 1 || len(active.Items) != 1 || active.Items[0].ID != "a1" {
		t.Fatalf("disabled subject leaked: %+v %v", active, e)
	}
}
