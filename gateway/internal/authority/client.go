// Package authority checks current Multica task and invocation authorization.
package authority

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Check struct {
	Subject     string              `json:"subject"`
	Actor       string              `json:"actor"`
	Agent       string              `json:"agent"`
	Task        string              `json:"task"`
	Connections map[string][]string `json:"connections"`
}
type Client struct {
	url, token string
	http       *http.Client
}

func New(origin, token string, loopback bool) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || !native.SafeURL(u, loopback) || u.Path != "" || u.RawQuery != "" || len(token) < 32 {
		return nil, errors.New("invalid source authority configuration")
	}
	return &Client{url: origin + "/internal/composio/authorize", token: token, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Authorize(ctx context.Context, check Check) error {
	denied := errors.New("source authority denied or unavailable")
	b, err := json.Marshal(check)
	if err != nil {
		return denied
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(b))
	if err != nil {
		return denied
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := c.http.Do(req)
	if err != nil {
		return denied
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return denied
	}
	b, err = io.ReadAll(io.LimitReader(res.Body, 4097))
	if err != nil || len(b) > 4096 {
		return denied
	}
	var result struct {
		Allowed bool `json:"allowed"`
	}
	if json.Unmarshal(b, &result) != nil || !result.Allowed {
		return denied
	}
	return nil
}
