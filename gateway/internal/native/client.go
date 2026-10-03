// Package native calls one explicitly configured OpenConnector runtime.
// Provider credentials remain exclusively in that runtime.
package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrUpstream = errors.New("native runtime unavailable or invalid response")

type Client struct {
	base  string
	admin string
	http  *http.Client
}

func New(base, admin string, allowLoopback bool) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || !SafeURL(u, allowLoopback) || u.RawQuery != "" || (u.Path != "" && u.Path != "/") || strings.TrimSpace(admin) == "" || strings.ContainsAny(admin, "\r\n") {
		return nil, errors.New("invalid native runtime configuration")
	}
	return &Client{base: strings.TrimRight(base, "/"), admin: admin, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// SafeURL allows HTTP only for an explicitly selected loopback development host.
func SafeURL(u *url.URL, allowLoopback bool) bool {
	if u == nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return allowLoopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()
}

// Subject avoids collisions between arbitrary project and subject strings.
func Subject(project, subject string) string {
	b, _ := json.Marshal([]string{project, subject})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type Request struct {
	ID               string `json:"connectionRequestId"`
	Status           string `json:"status"`
	AuthorizationURL string `json:"authorizationUrl"`
	ExpiresAt        string `json:"expiresAt"`
	AppID            string `json:"appId"`
}
type Connection struct {
	ID       string `json:"id"`
	Service  string `json:"service"`
	Status   string `json:"status"`
	AuthType string `json:"authType"`
}

func (c *Client) Start(ctx context.Context, subject, service, returnURI string) (Request, error) {
	var out Request
	err := c.do(ctx, http.MethodPost, "/v1/connections/"+url.PathEscape(service)+"/connect", subject, map[string]string{"returnUri": returnURI}, &out, true)
	if err == nil && (out.ID == "" || out.AuthorizationURL == "" || out.Status != "initiated") {
		err = ErrUpstream
	}
	return out, err
}
func (c *Client) Poll(ctx context.Context, subject, id string) (Request, error) {
	var out Request
	err := c.do(ctx, http.MethodGet, "/v1/connection-requests/"+url.PathEscape(id), subject, nil, &out, true)
	if err == nil && out.ID != id {
		err = ErrUpstream
	}
	return out, err
}
func (c *Client) Connection(ctx context.Context, id string) (Connection, error) {
	var out Connection
	err := c.do(ctx, http.MethodGet, "/v1/connections/by-id/"+url.PathEscape(id), "", nil, &out, true)
	if err == nil && out.ID != id {
		err = ErrUpstream
	}
	return out, err
}
func (c *Client) do(ctx context.Context, method, path, subject string, input, out any, envelope bool) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return ErrUpstream
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return ErrUpstream
	}
	req.Header.Set("Authorization", "Bearer "+c.admin)
	req.Header.Set("Content-Type", "application/json")
	if subject != "" {
		req.Header.Set("X-Connector-Subject", subject)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return ErrUpstream
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ErrUpstream
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return ErrUpstream
	}
	if envelope {
		var wire struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
		}
		if json.Unmarshal(b, &wire) != nil || !wire.Success {
			return ErrUpstream
		}
		b = wire.Data
	}
	if json.Unmarshal(b, out) != nil {
		return ErrUpstream
	}
	return nil
}
