package native

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type TokenRecord struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Actions     []string `json:"allowedActions"`
	Connections []string `json:"allowedConnections"`
	Proxies     []string `json:"allowedProxies"`
	Triggers    []string `json:"allowedTriggers"`
	Blocked     []string `json:"blockedActions"`
}
type Token struct {
	Token  string      `json:"token"`
	Record TokenRecord `json:"record"`
}

func Exact(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if strings.TrimSpace(v) != v || v == "" || strings.ContainsAny(v, "*\r\n") || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func sameSet(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Mint never allows the empty native connection policy (which means unrestricted).
func (c *Client) Mint(ctx context.Context, name string, connections, actions []string) (Token, error) {
	var out Token
	if name == "" || !Exact(connections) || !Exact(actions) {
		return out, ErrUpstream
	}
	input := map[string]any{"name": name, "allowedConnections": connections, "allowedActions": actions, "blockedActions": []string{}, "allowedProxies": []string{}, "allowedTriggers": []string{}}
	err := c.do(ctx, http.MethodPost, "/api/runtime-tokens", "", input, &out, false)
	if err == nil && (out.Token == "" || out.Record.ID == "" || out.Record.Name != name || !sameSet(out.Record.Actions, actions) || !sameSet(out.Record.Connections, connections) || len(out.Record.Proxies) > 0 || len(out.Record.Triggers) > 0 || len(out.Record.Blocked) > 0) {
		err = ErrUpstream
	}
	return out, err
}
func (c *Client) Tokens(ctx context.Context) ([]TokenRecord, error) {
	var out []TokenRecord
	err := c.do(ctx, http.MethodGet, "/api/runtime-tokens", "", nil, &out, false)
	return out, err
}
func (c *Client) RevokeToken(ctx context.Context, id string) error {
	var out struct {
		ID      string `json:"id"`
		Revoked bool   `json:"revoked"`
	}
	err := c.do(ctx, http.MethodDelete, "/api/runtime-tokens/"+url.PathEscape(id), "", nil, &out, false)
	if err == nil && (!out.Revoked || out.ID != id) {
		return ErrUpstream
	}
	return err
}
