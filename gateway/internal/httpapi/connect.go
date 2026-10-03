package httpapi

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
)

// ConnectAPI uses only deployment-selected native endpoints and secrets.
type ConnectAPI struct {
	DB            *store.Store
	Runtimes      map[string]*native.Client
	Vault         *credentials.Vault
	PublicOrigin  string
	MCPOrigin     string
	AllowLoopback bool
}

func strictBody(r *http.Request, out any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(out) == nil && dec.Decode(new(any)) == io.EOF
}

func (c *ConnectAPI) create(w http.ResponseWriter, r *http.Request, project string) {
	var input struct {
		User     string `json:"user_id"`
		Config   string `json:"auth_config_id"`
		Callback string `json:"callback_url"`
	}
	if !strictBody(r, &input) {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	callback, err := url.Parse(input.Callback)
	if err != nil || !native.SafeURL(callback, c.AllowLoopback) || callback.Path != "/api/integrations/composio/callback" || callback.Query().Get("state") == "" {
		failure(w, 400, "INVALID_CALLBACK")
		return
	}
	callbackQuery, queryErr := url.ParseQuery(callback.RawQuery)
	if queryErr != nil {
		failure(w, 400, "INVALID_CALLBACK")
		return
	}
	for k, v := range callbackQuery {
		if k != "state" || len(v) != 1 {
			failure(w, 400, "INVALID_CALLBACK")
			return
		}
	}
	nonce := credentials.NewToken()
	f, err := c.DB.BeginConnect(r.Context(), project, input.User, input.Config, input.Callback, callback.Scheme+"://"+callback.Host, nonce)
	if err != nil {
		failure(w, 409, "CONNECT_NOT_AVAILABLE")
		return
	}
	writeJSON(w, map[string]any{"redirect_url": c.PublicOrigin + "/connect/" + f.ID + "?nonce=" + nonce, "connected_account_id": f.ID, "expires_at": f.Expires.Format(time.RFC3339)})
}

var connectPage = template.Must(template.New("connect").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Connect account</title><h1>Connect {{.Toolkit}}</h1><p>Continue to authorize this account. You will then return to Multica to confirm your signed-in identity.</p><form method="post" action="{{.Action}}"><button type="submit">Continue</button></form></html>`))

func (c *ConnectAPI) browser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "connect" {
		failure(w, 404, "NOT_FOUND")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q["nonce"]) != 1 {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	// Native callback status is never evidence of success.
	for k, v := range q {
		if len(v) != 1 || (k != "nonce" && k != "status" && k != "connection_request_id" && k != "app_id" && k != "error" && k != "service" && k != "code" && k != "message") {
			failure(w, 400, "INVALID_INPUT")
			return
		}
	}
	f, err := c.DB.ReadConnect(r.Context(), parts[1], q.Get("nonce"))
	if err != nil {
		failure(w, 410, "CONNECT_EXPIRED_OR_UNAVAILABLE")
		return
	}
	upstream := c.Runtimes[f.Runtime]
	if upstream == nil || c.Vault == nil {
		failure(w, 503, "RUNTIME_UNAVAILABLE")
		return
	}
	path := c.PublicOrigin + "/connect/" + f.ID
	nonceQuery := "?nonce=" + url.QueryEscape(q.Get("nonce"))
	if len(parts) == 2 && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = connectPage.Execute(w, map[string]string{"Toolkit": f.Toolkit, "Action": path + "/start" + nonceQuery})
		return
	}
	if len(parts) != 3 {
		failure(w, 405, "METHOD_NOT_ALLOWED")
		return
	}
	if parts[2] == "start" && r.Method == http.MethodPost {
		if origin := r.Header.Get("Origin"); origin != "" && origin != c.PublicOrigin {
			failure(w, 403, "INVALID_ORIGIN")
			return
		}
		if f.Phase == "PENDING" {
			if c.DB.ClaimConnect(r.Context(), f) != nil {
				failure(w, 409, "CONNECT_IN_PROGRESS")
				return
			}
			started, e := upstream.Start(r.Context(), native.Subject(f.Project, f.Subject), f.Toolkit, path+"/poll"+nonceQuery)
			if e != nil {
				failure(w, 502, "NATIVE_CREATE_OUTCOME_UNKNOWN")
				return
			}
			authURL, e := url.Parse(started.AuthorizationURL)
			expires, expiryErr := time.Parse(time.RFC3339, started.ExpiresAt)
			if e != nil || !native.SafeURL(authURL, c.AllowLoopback) || expiryErr != nil || !expires.After(time.Now()) {
				failure(w, 502, "NATIVE_CREATE_OUTCOME_UNKNOWN")
				return
			}
			encrypted, e := c.Vault.Seal([]byte(started.AuthorizationURL), f.Project+"/"+f.ID)
			if e != nil || c.DB.SaveNativeRequest(r.Context(), f, started.ID, encrypted, expires) != nil {
				failure(w, 503, "NATIVE_CREATE_OUTCOME_UNKNOWN")
				return
			}
			http.Redirect(w, r, started.AuthorizationURL, http.StatusSeeOther)
			return
		}
		if f.Phase == "CREATING" && f.NativeRequest != "" && len(f.Authorization) > 0 {
			plain, e := c.Vault.Open(f.Authorization, f.Project+"/"+f.ID)
			if e != nil {
				failure(w, 503, "CONNECT_UNAVAILABLE")
				return
			}
			http.Redirect(w, r, string(plain), http.StatusSeeOther)
			return
		}
		failure(w, 409, "CONNECT_IN_PROGRESS")
		return
	}
	if parts[2] == "poll" && r.Method == http.MethodGet && (f.Phase == "CREATING" || f.Phase == "VERIFYING") && f.NativeRequest != "" {
		result, e := upstream.Poll(r.Context(), native.Subject(f.Project, f.Subject), f.NativeRequest)
		if e != nil {
			failure(w, 502, "NATIVE_POLL_UNAVAILABLE")
			return
		}
		if result.Status == "failed" || result.Status == "expired" {
			if c.DB.FailConnect(r.Context(), f) != nil {
				failure(w, 503, "CONNECT_UNAVAILABLE")
				return
			}
			failure(w, 409, "AUTHORIZATION_FAILED")
			return
		}
		if result.Status != "connected" || result.AppID == "" {
			failure(w, 409, "AUTHORIZATION_PENDING")
			return
		}
		account, e := upstream.Connection(r.Context(), result.AppID)
		if e != nil || account.Service != f.Toolkit || account.Status != "active" || account.AuthType != "oauth2" {
			failure(w, 409, "NATIVE_ACCOUNT_INVALID")
			return
		}
		ticket := credentials.NewToken()
		if c.DB.VerifyCandidate(r.Context(), f, account.ID, ticket) != nil {
			failure(w, 409, "CONNECT_IN_PROGRESS")
			return
		}
		target, _ := url.Parse(f.Callback)
		target.Path = "/api/integrations/composio/verify"
		target.RawQuery = url.Values{"verify_ticket": []string{ticket}}.Encode()
		http.Redirect(w, r, target.String(), http.StatusSeeOther)
		return
	}
	failure(w, 409, "CONNECT_IN_PROGRESS")
}
func (c *ConnectAPI) complete(w http.ResponseWriter, r *http.Request, project string) {
	var input struct {
		Ticket  string `json:"verify_ticket"`
		Subject string `json:"subject"`
	}
	if !strictBody(r, &input) || input.Ticket == "" || input.Subject == "" {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	candidate, err := c.DB.ConnectCandidate(r.Context(), project, input.Ticket)
	if err != nil {
		failure(w, 403, "IDENTITY_VERIFICATION_FAILED")
		return
	}
	if candidate.Subject == input.Subject && candidate.Phase != "ACTIVE" {
		upstream := c.Runtimes[candidate.Runtime]
		if upstream == nil {
			failure(w, 503, "RUNTIME_UNAVAILABLE")
			return
		}
		polled, e := upstream.Poll(r.Context(), native.Subject(project, candidate.Subject), candidate.NativeRequest)
		account, accountErr := upstream.Connection(r.Context(), candidate.NativeConnection)
		if e != nil || accountErr != nil || polled.Status != "connected" || polled.AppID != candidate.NativeConnection || account.Status != "active" || account.Service != candidate.Toolkit || account.AuthType != "oauth2" {
			failure(w, 409, "NATIVE_ACCOUNT_INVALID")
			return
		}
	}
	id, callback, err := c.DB.CompleteConnect(r.Context(), project, input.Ticket, input.Subject)
	if errors.Is(err, store.ErrConnectBusy) {
		failure(w, 503, "CONNECT_RETRY_REQUIRED")
		return
	}
	if err != nil {
		failure(w, 403, "IDENTITY_VERIFICATION_FAILED")
		return
	}
	target, _ := url.Parse(callback)
	q := target.Query()
	q.Set("status", "success")
	q.Set("connected_account_id", id)
	target.RawQuery = q.Encode()
	writeJSON(w, map[string]string{"connected_account_id": id, "callback_url": target.String()})
}
