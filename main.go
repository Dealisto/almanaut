// Command almanaut is a lightweight, self-hosted homelab inventory and
// documentation server.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Dealisto/almanaut/internal/config"
	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/web"
)

// version is the build version, overridden at link time with
// -ldflags "-X main.version=...". It is surfaced at /version.
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// "almanaut healthcheck" probes the local /healthz endpoint and exits 0/1.
	// It backs the container HEALTHCHECK, which cannot use a shell on the
	// distroless image, so the binary doubles as its own probe client.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck(cfg.Addr))
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "almanaut.db")

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := store.Migrate(db, dbPath); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	users := store.NewUserRepo(db)
	if err := web.BootstrapAdmin(users, log.Default(), cfg.AuthUser, cfg.AuthPass, cfg.ResetAdmin); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	handler := web.New(web.Config{
		Hosts:         store.NewHostRepo(db),
		Services:      store.NewServiceRepo(db),
		Networks:      store.NewNetworkRepo(db),
		Domains:       store.NewDomainRepo(db),
		Certificates:  store.NewCertificateRepo(db),
		Backups:       store.NewBackupRepo(db),
		Hardware:      store.NewHardwareRepo(db),
		Subscriptions: store.NewSubscriptionRepo(db),
		Accounts:      store.NewAccountRepo(db),
		Sites:         store.NewSiteRepo(db),
		Locations:     store.NewLocationRepo(db),
		Racks:         store.NewRackRepo(db),
		Relationships: store.NewRelationshipRepo(db),
		Tags:          store.NewTagRepo(db),
		DB:            db,
		Docker:        discovery.NewSocketClient(cfg.DockerSocket),
		NetScan:       discovery.NewNetworkScanner(),
		NetOpts:       web.NetDiscoveryOptions{Enabled: cfg.NetworkScanEnabled, DefaultSubnet: cfg.ScanSubnet},
		Proxmox:       discovery.NewProxmoxClient(cfg.ProxmoxURL, cfg.ProxmoxToken, cfg.ProxmoxInsecure),
		PVEOpts:       web.ProxmoxOptions{Enabled: cfg.ProxmoxURL != "" && cfg.ProxmoxToken != ""},
		AuthEnabled:   true,
		SecureCookies: cfg.SecureCookies,
		Version:       version,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// Run the server until a termination signal arrives, then shut it down
	// cleanly so in-flight requests finish (important under `docker stop`).
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Almanaut listening on %s (data: %s)", cfg.Addr, cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Expiry notifications are opt-in: only run when a topic URL is configured.
	if cfg.NtfyURL != "" {
		notifier := notify.New(
			store.NewCertificateRepo(db),
			store.NewHardwareRepo(db),
			store.NewSubscriptionRepo(db),
			store.NewNotificationRepo(db),
			notify.NewNtfyClient(cfg.NtfyURL, cfg.NtfyToken),
			cfg.NotifyWithinDays,
		)
		go runNotifier(ctx, notifier, cfg.NotifyInterval)
	}

	select {
	case err := <-serverErr:
		log.Fatalf("server: %v", err)
	case <-ctx.Done():
		stop() // restore default signal handling so a second signal force-quits
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		} else {
			log.Println("stopped")
		}
		cancel()
	}
}

// runHealthcheck issues a GET to the local /healthz endpoint and returns a
// process exit code: 0 when it answers 200, 1 otherwise. addr is the server's
// listen address (e.g. ":8080" or "0.0.0.0:8080"); the probe always targets
// loopback regardless of the bind host.
func runHealthcheck(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		log.Printf("healthcheck: invalid address %q: %v", addr, err)
		return 1
	}
	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("healthcheck: %v", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("healthcheck: status %d", resp.StatusCode)
		return 1
	}
	return 0
}

// runNotifier runs one notification pass at startup, then every interval, until
// ctx is cancelled. Each pass is bounded by its own timeout so a hung ntfy
// endpoint cannot wedge the loop.
func runNotifier(ctx context.Context, n *notify.Notifier, interval time.Duration) {
	pass := func() {
		runCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if err := n.Run(runCtx, time.Now()); err != nil {
			log.Printf("notify: run: %v", err)
		}
	}
	pass()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass()
		}
	}
}
