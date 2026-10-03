package native

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

var ErrUnsupported = errors.New("native revocation unsupported")
var ErrBlocked = errors.New("native revocation blocked or outcome unknown")

func (c *Client) RevokeConnection(ctx context.Context, id string) error {
	var out struct {
		Connection string `json:"connectionId"`
		State      string `json:"state"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/connections/by-id/"+url.PathEscape(id)+"/revoke", "", nil, &out, true); err != nil {
		return err
	}
	if out.Connection != id || out.State != "REVOKED" {
		return ErrUpstream
	}
	return nil
}
func (c *Client) DeleteRevokedConnection(ctx context.Context, id string) error {
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := c.do(ctx, http.MethodDelete, "/v1/connections/by-id/"+url.PathEscape(id)+"/revoked", "", nil, &out, true); err != nil {
		return err
	}
	if !out.Deleted {
		return ErrUpstream
	}
	return nil
}

// CheckCompatibility rejects a native downgrade before the gateway listens.
func (c *Client) CheckCompatibility(ctx context.Context) error {
	var out struct {
		Profile  string `json:"profile"`
		Revision int    `json:"securityRevision"`
		Subject  bool   `json:"trustedSubjectRequests"`
		Revoke   bool   `json:"nonDestructiveRevocation"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/compatibility-capabilities", "", nil, &out, true); err != nil {
		return err
	}
	if out.Profile != "multica-core-v1" || out.Revision != 2 || !out.Subject || !out.Revoke {
		return ErrUnsupported
	}
	return nil
}
