package authority

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceAuthorityFailsClosedAndNeverFollowsRedirect(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++ }))
	defer destination.Close()
	status := 200
	body := `{"allowed":true}`
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/composio/authorize" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("wrong authority request")
		}
		if status == 302 {
			w.Header().Set("Location", destination.URL)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer source.Close()
	c, err := New(source.URL, strings.Repeat("x", 32), true)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Authorize(context.Background(), Check{}); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		status int
		body   string
	}{{403, `{"allowed":true}`}, {200, `{"allowed":false}`}, {200, `broken`}, {502, `{"allowed":true}`}, {302, `{"allowed":true}`}} {
		status, body = sample.status, sample.body
		if c.Authorize(context.Background(), Check{}) == nil {
			t.Fatalf("admitted %d %s", status, body)
		}
	}
	if destinationCalls != 0 {
		t.Fatal("authority secret followed redirect")
	}
}
