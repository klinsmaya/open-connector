package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/klinsmaya/open-connector/gateway/internal/store"
)

type toolkitView struct {
	usage       int64
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	AuthSchemes []string `json:"auth_schemes"`
}
type authView struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Toolkit           toolkitView `json:"toolkit"`
	AuthScheme        string      `json:"auth_scheme"`
	IsComposioManaged bool        `json:"is_composio_managed"`
	Status            string      `json:"status"`
}
type cursorClaims struct {
	Snapshot string `json:"s"`
	Project  string `json:"p"`
	Path     string `json:"r"`
	Filters  string `json:"f"`
	Last     string `json:"l"`
	Version  int    `json:"v"`
}

func catalog(authority Authority, w http.ResponseWriter, r *http.Request, project string) {
	auth := r.URL.Path == "/api/v3.1/auth_configs"
	detail := strings.HasPrefix(r.URL.Path, "/api/v3.1/toolkits/")
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	allowed := map[string]bool{"limit": true, "cursor": true}
	if auth {
		for _, k := range []string{"toolkit_slug", "is_composio_managed", "show_disabled", "search"} {
			allowed[k] = true
		}
	} else if !detail {
		allowed["sort_by"] = true
		allowed["category"] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 {
			failure(w, 400, "INVALID_INPUT")
			return
		}
	}
	if detail && len(q) != 0 {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	limit := 100
	if raw, ok := q["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 1000 {
			failure(w, 400, "INVALID_INPUT")
			return
		}
		limit = n
	}
	if v := q.Get("sort_by"); v != "" && v != "alphabetically" && v != "usage" {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	for _, key := range []string{"is_composio_managed", "show_disabled"} {
		if value, ok := q[key]; ok && value[0] != "true" && value[0] != "false" {
			failure(w, 400, "INVALID_INPUT")
			return
		}
	}
	rawCursor := q.Get("cursor")
	q.Del("cursor")
	scope := cursorClaims{Project: project, Path: r.URL.Path, Filters: q.Encode(), Version: 1}
	entries, err := authority.Catalog(r.Context(), project)
	if err != nil {
		failure(w, 503, "CATALOG_UNAVAILABLE")
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	snapshot, _ := json.Marshal(entries)
	scope.Snapshot = fmt.Sprintf("%x", sha256.Sum256(snapshot))
	if rawCursor != "" {
		last, ok := readCursor(rawCursor, scope, r.Header.Get("x-api-key"))
		if !ok {
			failure(w, 400, "INVALID_CURSOR")
			return
		}
		scope.Last = last
	}
	// Scope and filter before pagination. Stable sorting is part of the cursor
	// contract, not the native catalog's ordering.
	if auth {
		out := make([]authView, 0)
		for _, e := range entries {
			if !e.Enabled && q.Get("show_disabled") != "true" {
				continue
			}
			if q.Get("is_composio_managed") == "true" {
				continue
			}
			if q.Get("toolkit_slug") != "" && !contains(strings.Split(q.Get("toolkit_slug"), ","), e.Toolkit) {
				continue
			}
			if search := strings.ToLower(q.Get("search")); search != "" && !strings.Contains(strings.ToLower(e.Name+" "+e.ID), search) {
				continue
			}
			status := "DISABLED"
			if e.Enabled {
				status = "ENABLED"
			}
			out = append(out, authView{ID: e.ID, Name: e.Name, Toolkit: toolkit(e), AuthScheme: e.AuthType, Status: status})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		total := len(out)
		start := sort.Search(len(out), func(i int) bool { return out[i].ID > scope.Last })
		out = out[start:]
		next := ""
		if len(out) > limit {
			out = out[:limit]
			scope.Last = out[len(out)-1].ID
			next = signCursor(scope, r.Header.Get("x-api-key"))
		}
		writeList(w, out, next, total)
		return
	}
	out := make([]toolkitView, 0)
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.Enabled || seen[e.Toolkit] {
			continue
		}
		seen[e.Toolkit] = true
		out = append(out, toolkit(e))
	}
	orderKey := func(t toolkitView) string {
		if q.Get("sort_by") == "usage" {
			return fmt.Sprintf("%020d:%s", int64(math.MaxInt64)-t.usage, t.Slug)
		}
		return strings.ToLower(t.Name) + ":" + t.Slug
	}
	sort.Slice(out, func(i, j int) bool { return orderKey(out[i]) < orderKey(out[j]) })
	if detail {
		slug := strings.TrimPrefix(r.URL.Path, "/api/v3.1/toolkits/")
		for _, e := range out {
			if e.Slug == slug {
				writeJSON(w, e)
				return
			}
		}
		failure(w, 404, "NOT_FOUND")
		return
	}
	// Category metadata is not provisioned yet; never silently ignore a filter.
	if q.Get("category") != "" {
		failure(w, 501, "UNSUPPORTED_CAPABILITY")
		return
	}
	total := len(out)
	start := sort.Search(len(out), func(i int) bool { return orderKey(out[i]) > scope.Last })
	out = out[start:]
	next := ""
	if len(out) > limit {
		out = out[:limit]
		scope.Last = orderKey(out[len(out)-1])
		next = signCursor(scope, r.Header.Get("x-api-key"))
	}
	writeList(w, out, next, total)
}

func toolkit(e store.CatalogEntry) toolkitView {
	return toolkitView{Slug: e.Toolkit, Name: e.Name, AuthSchemes: []string{e.AuthType}, usage: e.Usage}
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func writeList(w http.ResponseWriter, items any, cursor string, total int) {
	writeJSON(w, map[string]any{"items": items, "next_cursor": cursor, "total_items": total})
}

func signCursor(claims cursorClaims, key string) string {
	payload, _ := json.Marshal(claims)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func readCursor(raw string, want cursorClaims, key string) (string, bool) {
	if len(raw) > 4096 {
		return "", false
	}
	payload, signature, ok := strings.Cut(raw, ".")
	if !ok {
		return "", false
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", false
	}
	var got cursorClaims
	if json.Unmarshal(body, &got) != nil || got.Version != want.Version || got.Project != want.Project || got.Path != want.Path || got.Filters != want.Filters || got.Snapshot != want.Snapshot {
		return "", false
	}
	return got.Last, true
}
