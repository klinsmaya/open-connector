package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func accounts(authority Authority, w http.ResponseWriter, r *http.Request, project string) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	arrays := map[string]bool{"user_ids": true, "toolkit_slugs": true, "auth_config_ids": true, "connected_account_ids": true, "statuses": true}
	scalars := map[string]bool{"limit": true, "cursor": true, "order_by": true, "order_direction": true, "account_type": true}
	for k, values := range q {
		if (!arrays[k] && !scalars[k]) || len(values) > 100 || (scalars[k] && len(values) != 1) {
			failure(w, 400, "INVALID_INPUT")
			return
		}
		for _, v := range values {
			if v == "" {
				failure(w, 400, "INVALID_INPUT")
				return
			}
		}
		if arrays[k] {
			sort.Strings(values)
		}
	}
	if v := q.Get("account_type"); v != "" && v != "PRIVATE" && v != "ALL" {
		failure(w, 501, "UNSUPPORTED_CAPABILITY")
		return
	}
	if v := q.Get("order_by"); v != "" && v != "created_at" && v != "updated_at" {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	if v := q.Get("order_direction"); v != "" && v != "asc" && v != "desc" {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	for _, v := range q["statuses"] {
		if !contains([]string{"ACTIVE", "INACTIVE", "INITIATED", "FAILED", "EXPIRED"}, v) {
			failure(w, 400, "INVALID_INPUT")
			return
		}
	}
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			failure(w, 400, "INVALID_INPUT")
			return
		}
	}
	records, err := authority.Accounts(r.Context(), project)
	if err != nil {
		failure(w, 503, "ACCOUNTS_UNAVAILABLE")
		return
	}
	detail := strings.HasPrefix(r.URL.Path, "/api/v3.1/connected_accounts/")
	if detail {
		if len(q) != 0 {
			failure(w, 400, "INVALID_INPUT")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v3.1/connected_accounts/")
		for _, a := range records {
			if a.ID == id {
				writeJSON(w, a)
				return
			}
		}
		failure(w, 404, "NOT_FOUND")
		return
	}
	out := make([]store.Account, 0)
	for _, a := range records {
		if (len(q["user_ids"]) > 0 && !contains(q["user_ids"], a.UserID)) || (len(q["toolkit_slugs"]) > 0 && !contains(q["toolkit_slugs"], a.Toolkit.Slug)) || (len(q["auth_config_ids"]) > 0 && !contains(q["auth_config_ids"], a.ConfigID)) || (len(q["connected_account_ids"]) > 0 && !contains(q["connected_account_ids"], a.ID)) || (len(q["statuses"]) > 0 && !contains(q["statuses"], a.Status)) {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		at, bt := a.Created, b.Created
		if q.Get("order_by") == "updated_at" {
			at, bt = a.Updated, b.Updated
		}
		if at.Equal(bt) {
			return a.ID < b.ID
		}
		if q.Get("order_direction") == "asc" {
			return at.Before(bt)
		}
		return at.After(bt)
	})
	rawCursor := q.Get("cursor")
	q.Del("cursor")
	snapshot, _ := json.Marshal(out)
	scope := cursorClaims{Project: project, Path: r.URL.Path, Filters: q.Encode(), Version: 1, Snapshot: fmt.Sprintf("%x", sha256.Sum256(snapshot))}
	total := len(out)
	start := 0
	if rawCursor != "" {
		last, ok := readCursor(rawCursor, scope, r.Header.Get("x-api-key"))
		if !ok {
			failure(w, 400, "INVALID_CURSOR")
			return
		}
		found := false
		for i, a := range out {
			if a.ID == last {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			failure(w, 400, "INVALID_CURSOR")
			return
		}
	}
	out = out[start:]
	next := ""
	if len(out) > limit {
		out = out[:limit]
		scope.Last = out[len(out)-1].ID
		next = signCursor(scope, r.Header.Get("x-api-key"))
	}
	writeList(w, out, next, total)
}
