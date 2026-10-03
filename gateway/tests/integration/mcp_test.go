package integration_test

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type bearerTransport string

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// This uses the actual MCP SDK over HTTP, through the gateway and native OC.
func exerciseMCP(t *testing.T, ctx context.Context, endpoint, bearer, connection string) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearerTransport(bearer)}}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-06-18"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != "2025-06-18" {
		t.Fatal("unexpected negotiated protocol")
	}
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 5 {
		t.Fatalf("tools=%+v error=%v", list, err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"execute_action", "get_action_guide", "list_apps", "list_connections", "search_actions"}) {
		t.Fatalf("tool surface changed: %v", names)
	}
	for _, name := range []string{"COMPOSIO_REMOTE_WORKBENCH", "COMPOSIO_REMOTE_BASH_TOOL", "COMPOSIO_MULTI_EXECUTE_TOOL", "proxy", "remote_bash", "workbench"} {
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if e == nil && !result.IsError {
			t.Fatalf("unsupported tool accepted: %s", name)
		}
	}
	call := func(name string, args map[string]any, rejected bool) string {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != rejected {
			t.Fatalf("%s: %+v", name, result)
		}
		data, _ := json.Marshal(result.Content)
		return string(data)
	}
	connections := call("list_connections", map[string]any{}, false)
	if !strings.Contains(connections, connection) {
		t.Fatal("missing compatibility connection")
	}
	call("list_connections", map[string]any{"connectionName": "native"}, true)
	apps := call("list_apps", map[string]any{}, false)
	if !strings.Contains(apps, "example") || strings.Contains(apps, "ungranted") || strings.Contains(connections, "other-account") {
		t.Fatal("discovery leaked ungranted resource")
	}
	actions := call("search_actions", map[string]any{"query": "fixture"}, false)
	if !strings.Contains(actions, "example.read") || strings.Contains(actions, "example.hidden") {
		t.Fatal("search view differs from exact action grants")
	}
	call("get_action_guide", map[string]any{"actionId": "example.hidden", "connectionName": connection}, true)
	call("get_action_guide", map[string]any{"actionId": "example.read", "connectionName": "other-account"}, true)
	call("get_action_guide", map[string]any{"actionId": "example.read", "connectionName": connection}, false)
	call("get_action_guide", map[string]any{"actionId": "example.read", "connectionName": "native-alias"}, true)
	args := map[string]any{"actionId": "example.read", "connectionName": connection, "operationId": "intent_0001", "input": map[string]any{"value": "first"}}
	first := call("execute_action", args, false)
	if !strings.Contains(first, `\"executions\":1`) {
		t.Fatalf("first execution %s", first)
	}
	replay := call("execute_action", args, false)
	if replay != first {
		t.Fatalf("unstable replay %s vs %s", first, replay)
	}
	args["operationId"] = "intent_same_args"
	same := call("execute_action", args, false)
	if !strings.Contains(same, `\"executions\":2`) {
		t.Fatalf("distinct intent reused result %s", same)
	}
	args["operationId"] = "intent_0001"
	args["input"] = map[string]any{"value": "different"}
	call("execute_action", args, true)
	args["operationId"] = "intent_0002"
	second := call("execute_action", args, false)
	if !strings.Contains(second, `\"executions\":3`) {
		t.Fatalf("second execution %s", second)
	}
	args["operationId"] = "intent_lost_response"
	args["input"] = map[string]any{"value": "__lose_response__"}
	call("execute_action", args, true)
	uncertain := call("execute_action", args, true)
	if !strings.Contains(uncertain, "OPERATION_UNKNOWN") {
		t.Fatalf("unknown redispatched %s", uncertain)
	}
	args["operationId"] = "intent_after_loss"
	args["input"] = map[string]any{"value": "after loss"}
	after := call("execute_action", args, false)
	if !strings.Contains(after, `\"executions\":5`) {
		t.Fatalf("lost response caused extra execution %s", after)
	}
	args["actionId"] = "example.write"
	call("execute_action", args, true)
}
