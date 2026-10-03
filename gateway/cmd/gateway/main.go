// Command gateway serves the local-development compatibility skeleton.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/klinsmaya/open-connector/gateway/internal/httpapi"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "serve", "serve, migrate, or quarantine-restore")
	dbFile := flag.String("database-url-file", "", "path to a local database URL secret")
	control := flag.String("control-listen", "127.0.0.1:8080", "local control listener")
	execution := flag.String("mcp-listen", "127.0.0.1:8081", "local execution listener")
	nativeURL := flag.String("native-url", "", "explicit native runtime origin")
	nativeID := flag.String("native-id", "runtime", "native runtime identity used by auth configs")
	nativeSecret := flag.String("native-admin-file", "", "native admin bearer file")
	vaultFile := flag.String("vault-key-file", "", "32-byte raw gateway encryption key file")
	publicOrigin := flag.String("public-origin", "", "browser-visible control origin")
	flag.Parse()
	if *mode != "serve" && *mode != "migrate" && *mode != "quarantine-restore" {
		return errors.New("invalid mode")
	}
	for _, addr := range []string{*control, *execution} {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("this development build requires loopback listeners")
		}
	}
	if *control == *execution {
		return errors.New("control and execution listeners must differ")
	}
	if *dbFile == "" {
		return errors.New("database-url-file is required")
	}
	b, err := os.ReadFile(*dbFile)
	if err != nil {
		return errors.New("database secret is unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := store.Open(startup, strings.TrimSpace(string(b)))
	cancel()
	if err != nil {
		return err
	}
	defer db.Pool.Close()
	if *mode == "migrate" {
		if err = db.Migrate(ctx); err != nil {
			return errors.New("migration failed; inspect isolated database migration ledger")
		}
		return nil
	}
	if *mode == "quarantine-restore" {
		if err = db.QuarantineRestore(ctx); err != nil {
			return errors.New("restore quarantine failed")
		}
		return nil
	}
	if err = db.Ready(ctx); err != nil {
		return errors.New("database is not ready or is quarantined")
	}
	var flow *httpapi.ConnectAPI
	if *nativeURL != "" || *nativeSecret != "" || *vaultFile != "" || *publicOrigin != "" {
		admin, e := os.ReadFile(*nativeSecret)
		if e != nil {
			return errors.New("native admin secret unavailable")
		}
		key, e := os.ReadFile(*vaultFile)
		if e != nil {
			return errors.New("vault key unavailable")
		}
		vault, e := credentials.NewVault(key)
		if e != nil {
			return e
		}
		upstream, e := native.New(*nativeURL, strings.TrimSpace(string(admin)), true)
		if e != nil {
			return e
		}
		origin, e := url.Parse(*publicOrigin)
		if e != nil || !native.SafeURL(origin, true) || origin.Path != "" || origin.RawQuery != "" {
			return errors.New("invalid public origin")
		}
		flow = &httpapi.ConnectAPI{DB: db, Runtimes: map[string]*native.Client{*nativeID: upstream}, Vault: vault, PublicOrigin: *publicOrigin, AllowLoopback: true}
	}
	ch, mh := httpapi.Handlers(db, flow)
	a, err := net.Listen("tcp", *control)
	if err != nil {
		return errors.New("control listener unavailable")
	}
	defer a.Close()
	bListener, err := net.Listen("tcp", *execution)
	if err != nil {
		return errors.New("execution listener unavailable")
	}
	defer bListener.Close()
	servers := []*http.Server{{Handler: ch, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}, {Handler: mh, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}}
	done := make(chan error, 2)
	go func() { done <- servers[0].Serve(a) }()
	go func() { done <- servers[1].Serve(bListener) }()
	var servingErr error
	select {
	case <-ctx.Done():
	case servingErr = <-done:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
	if servingErr != nil && !errors.Is(servingErr, http.ErrServerClosed) {
		return errors.New("listener stopped unexpectedly")
	}
	return nil
}
