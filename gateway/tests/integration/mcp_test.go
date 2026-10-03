package integration_test

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
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
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearerTransport(bearer)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 5 {
		t.Fatalf("tools=%+v error=%v", list, err)
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
	call("search_actions", map[string]any{"query": "fixture"}, false)
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
	args["input"] = map[string]any{"value": "different"}
	call("execute_action", args, true)
	args["operationId"] = "intent_0002"
	second := call("execute_action", args, false)
	if !strings.Contains(second, `\"executions\":2`) {
		t.Fatalf("second execution %s", second)
	}
	args["actionId"] = "example.write"
	call("execute_action", args, true)
}
