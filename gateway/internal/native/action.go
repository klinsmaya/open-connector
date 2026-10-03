package native

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type Action struct {
	ID          string   `json:"id"`
	Service     string   `json:"service"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Operation   string   `json:"operationType"`
	Input       any      `json:"inputSchema"`
	Output      any      `json:"outputSchema"`
	Scopes      []string `json:"requiredScopes"`
}

func (c *Client) Action(ctx context.Context, token, id string) (Action, error) {
	var out Action
	_, data, err := c.runtime(ctx, http.MethodGet, id, token, "", "", nil)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(data, &out) != nil || out.ID != id {
		return out, ErrUpstream
	}
	return out, nil
}
func (c *Client) Execute(ctx context.Context, token, id, connection, operation string, input any) (string, json.RawMessage, error) {
	return c.runtime(ctx, http.MethodPost, id, token, connection, operation, map[string]any{"input": input})
}
func (c *Client) runtime(ctx context.Context, method, id, token, connection, operation string, input any) (string, json.RawMessage, error) {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return "", nil, ErrUpstream
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/v1/actions/"+url.PathEscape(id), body)
	if err != nil {
		return "", nil, ErrUpstream
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		if connection == "" || operation == "" {
			return "", nil, ErrUpstream
		}
		req.Header.Set("x-oo-connector-app-id", connection)
		req.Header.Set("Idempotency-Key", operation)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", nil, ErrUpstream
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", nil, ErrUpstream
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return "", nil, ErrUpstream
	}
	var wire struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Meta    struct {
			ExecutionID string `json:"executionId"`
		} `json:"meta"`
	}
	if json.Unmarshal(b, &wire) != nil || !wire.Success || len(wire.Data) == 0 {
		return "", nil, ErrUpstream
	}
	return wire.Meta.ExecutionID, wire.Data, nil
}
