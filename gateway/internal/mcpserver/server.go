// Package mcpserver implements the constrained native-tools-v1 profile with
// the official MCP SDK. No arbitrary native MCP tool or selector is forwarded.
package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type API struct {
	DB       *store.Store
	Vault    *credentials.Vault
	Runtimes map[string]*native.Client
}

var operationID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

type arguments struct {
	Query      string         `json:"query,omitempty"`
	Service    string         `json:"service,omitempty"`
	Limit      int            `json:"limit,omitempty"`
	Action     string         `json:"actionId,omitempty"`
	Connection string         `json:"connectionName,omitempty"`
	Operation  string         `json:"operationId,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

func (a *API) Serve(w http.ResponseWriter, r *http.Request, session, bearer string) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mcp.NewServer(&mcp.Implementation{Name: "open-connector-gateway", Version: "multica-core-v1"}, &mcp.ServerOptions{Logger: logger})
	for _, name := range []string{"list_apps", "list_connections", "search_actions", "get_action_guide", "execute_action"} {
		properties := map[string]any{}
		required := []string{}
		switch name {
		case "list_apps":
			properties["query"] = map[string]string{"type": "string"}
		case "list_connections":
			properties["service"] = map[string]string{"type": "string"}
		case "search_actions":
			properties["query"] = map[string]string{"type": "string"}
			properties["service"] = map[string]string{"type": "string"}
			properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 50}
		default:
			properties["actionId"] = map[string]string{"type": "string"}
			properties["connectionName"] = map[string]string{"type": "string", "description": "Exact compatibility connection ID from list_connections; no native names or defaults."}
			required = []string{"actionId", "connectionName"}
			if name == "execute_action" {
				properties["input"] = map[string]string{"type": "object"}
				properties["operationId"] = map[string]string{"type": "string", "description": "Unique ID for this business intent. Reuse only for transport retries."}
				required = append(required, "operationId")
			}
		}
		tool := name
		server.AddTool(&mcp.Tool{Name: name, Description: "Read-only gateway " + name + " using only this session's exact grants.", InputSchema: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var in arguments
			decoder := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			decoder.DisallowUnknownFields()
			decoder.UseNumber()
			if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
				return toolFailure("INVALID_INPUT"), nil
			}
			// The low-level SDK API does not validate schemas; enforce each tool's
			// allowed fields independently rather than trusting advertised metadata.
			var fields map[string]json.RawMessage
			if json.Unmarshal(req.Params.Arguments, &fields) != nil {
				return toolFailure("INVALID_INPUT"), nil
			}
			for field := range fields {
				if _, ok := properties[field]; !ok {
					return toolFailure("INVALID_INPUT"), nil
				}
			}
			for _, field := range required {
				if _, ok := fields[field]; !ok {
					return toolFailure("INVALID_INPUT"), nil
				}
			}
			return a.call(ctx, tool, session, bearer, in), nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: logger, MaxRequestBodyBytes: 1 << 20})
	http.NewCrossOriginProtection().Handler(handler).ServeHTTP(w, r)
}
func toolFailure(code string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}
}
func toolResult(value any) *mcp.CallToolResult {
	b, err := json.Marshal(value)
	if err != nil {
		return toolFailure("RESULT_UNAVAILABLE")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func (a *API) call(ctx context.Context, name, session, bearer string, in arguments) *mcp.CallToolResult {
	admission, err := a.DB.Admit(ctx, session, bearer)
	if err != nil {
		return toolFailure("SESSION_DENIED")
	}
	grants, err := a.DB.Grants(ctx, admission)
	if err != nil || len(grants) == 0 {
		return toolFailure("POLICY_UNAVAILABLE")
	}
	runtime, sealed, err := a.DB.SessionMaterial(ctx, admission)
	if err != nil {
		return toolFailure("SESSION_UNAVAILABLE")
	}
	token, err := a.Vault.Open(sealed, store.SessionIdentity(admission.ProjectID, session))
	if err != nil {
		return toolFailure("SESSION_UNAVAILABLE")
	}
	upstream := a.Runtimes[runtime]
	if upstream == nil {
		return toolFailure("RUNTIME_UNAVAILABLE")
	}
	if name == "list_apps" || name == "list_connections" {
		out := make([]map[string]string, 0)
		seen := map[string]bool{}
		for _, g := range grants {
			if in.Service != "" && in.Service != g.Toolkit {
				continue
			}
			if in.Query != "" && !strings.Contains(strings.ToLower(g.Toolkit), strings.ToLower(in.Query)) {
				continue
			}
			key := g.Connection
			if name == "list_apps" {
				key = g.Toolkit
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			row := map[string]string{"service": g.Toolkit}
			if name == "list_connections" {
				row["connectionName"] = g.Connection
			}
			out = append(out, row)
		}
		return toolResult(out)
	}
	if name == "search_actions" {
		limit := in.Limit
		if limit == 0 {
			limit = 10
		}
		if limit < 1 || limit > 50 {
			return toolFailure("INVALID_INPUT")
		}
		out := make([]any, 0)
		seen := map[string]bool{}
		for _, g := range grants {
			if seen[g.Action] || (in.Service != "" && in.Service != g.Toolkit) {
				continue
			}
			seen[g.Action] = true
			action, e := upstream.Action(ctx, string(token), g.Action)
			if e != nil {
				return toolFailure("CATALOG_UNAVAILABLE")
			}
			if action.Operation != "read" {
				continue
			}
			if in.Query != "" && !strings.Contains(strings.ToLower(action.ID+" "+action.Description), strings.ToLower(in.Query)) {
				continue
			}
			out = append(out, action)
			if len(out) == limit {
				break
			}
		}
		return toolResult(out)
	}
	var selected *store.Grant
	for _, g := range grants {
		if g.Action == in.Action && g.Connection == in.Connection {
			copy := g
			selected = &copy
			break
		}
	}
	if selected == nil {
		return toolFailure("ACTION_OR_CONNECTION_DENIED")
	}
	action, err := upstream.Action(ctx, string(token), in.Action)
	if err != nil {
		return toolFailure("CATALOG_UNAVAILABLE")
	}
	if action.Operation != "read" {
		return toolFailure("READ_ONLY_PROFILE")
	}
	if name == "get_action_guide" {
		return toolResult(map[string]any{"action": action, "connectionName": selected.Connection, "operationId": "Supply a unique ID for each intent; retries reuse it."})
	}
	if !operationID.MatchString(in.Operation) {
		return toolFailure("INVALID_OPERATION_ID")
	}
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	canonical, _ := json.Marshal([]any{admission.ProjectID, admission.SubjectID, admission.AgentID, admission.ActorID, admission.TaskID, selected.Connection, selected.Generation, in.Action, in.Input})
	fingerprint := sha256.Sum256(canonical)
	if _, err = a.DB.Admit(ctx, session, bearer); err != nil {
		return toolFailure("SESSION_DENIED")
	}
	reserved, err := a.DB.ReserveExecution(ctx, admission, in.Operation, *selected, fingerprint[:])
	if err != nil {
		return toolFailure("OPERATION_CONFLICT_OR_UNAVAILABLE")
	}
	if !reserved.Dispatch {
		if reserved.State == "SUCCEEDED" {
			return toolResult(json.RawMessage(reserved.Result))
		}
		return toolFailure("OPERATION_" + reserved.State)
	}
	nativeOperation := fmt.Sprintf("gw-%x", sha256.Sum256([]byte(admission.ProjectID+"/"+in.Operation)))
	nativeID, data, err := upstream.Execute(ctx, string(token), in.Action, selected.NativeID, nativeOperation, in.Input)
	state := "SUCCEEDED"
	if err != nil {
		state = "UNKNOWN"
		data = nil
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if a.DB.FinishExecution(finish, admission.ProjectID, in.Operation, nativeID, state, data) != nil {
		return toolFailure("OPERATION_OUTCOME_UNKNOWN")
	}
	if state == "UNKNOWN" {
		return toolFailure("OPERATION_OUTCOME_UNKNOWN")
	}
	return toolResult(json.RawMessage(data))
}
