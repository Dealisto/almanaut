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
	"github.com/Dealisto/almanaut/internal/job"
	"github.com/Dealisto/almanaut/internal/kuma"
	"github.com/Dealisto/almanaut/internal/liveness"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/web"
	"github.com/Dealisto/almanaut/internal/webhook"
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

	var dispatcher webhook.Dispatcher = webhook.Noop{}
	if cfg.WebhooksEnabled {
		dispatcher = webhook.NewQueue(store.NewWebhookRepo(db), webhook.Options{
			Timeout:     cfg.WebhookTimeout,
			MaxAttempts: cfg.WebhookMaxAttempts,
		})
		log.Println("outbound webhooks enabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Uptime Kuma monitor sync: opt-in, enabled only when URL + user + pass
	// are all configured. The syncer receives the same post-commit events as
	// the webhook queue (fan-out) and also serves the admin Sync-now button.
	kumaOpts := web.KumaOptions{}
	kumaEnabled := cfg.KumaURL != "" && cfg.KumaUser != "" && cfg.KumaPass != ""
	if kumaEnabled {
		syncer := kuma.NewSyncer(
			kuma.NewClient(cfg.KumaURL, cfg.KumaUser, cfg.KumaPass, cfg.KumaInsecure),
			store.NewServiceRepo(db),
			store.NewKumaRepo(db),
			log.Default(),
		)
		go syncer.Start(ctx)
		dispatcher = webhook.Multi{dispatcher, syncer}
		kumaOpts = web.KumaOptions{Enabled: true, URL: cfg.KumaURL, Syncer: syncer}
		log.Println("uptime kuma sync enabled")
	}

	// Background jobs run through a shared runner (the Scheduled-tasks admin
	// page reflects it); it is always constructed so /tasks is always mounted
	// for admins, even before any job is registered below.
	runner := job.New(log.Default())

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
		Contacts:      store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db),
		Tags:          store.NewTagRepo(db),
		VLANs:         store.NewVLANRepo(db),
		Reservations:  store.NewReservationRepo(db),
		DB:            db,
		Docker:        discovery.NewSocketClient(cfg.DockerSocket),
		NetScan:       discovery.NewNetworkScanner(),
		NetOpts:       web.NetDiscoveryOptions{Enabled: cfg.NetworkScanEnabled, DefaultSubnet: cfg.ScanSubnet},
		Proxmox:       discovery.NewProxmoxClient(cfg.ProxmoxURL, cfg.ProxmoxToken, cfg.ProxmoxInsecure),
		PVEOpts:       web.ProxmoxOptions{Enabled: cfg.ProxmoxURL != "" && cfg.ProxmoxToken != ""},
		AuthEnabled:   true,
		SecureCookies: cfg.SecureCookies,
		Version:       version,
		Webhooks:      dispatcher,
		Kuma:          kumaOpts,
		Tasks:         runner,
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

	// Expiry notifications are opt-in: only run when at least one channel is
	// configured. Every configured channel receives each alert (fan-out).
	var senders []notify.Sender
	if cfg.NtfyURL != "" {
		senders = append(senders, notify.NewNtfyClient(cfg.NtfyURL, cfg.NtfyToken))
	}
	if cfg.DiscordWebhookURL != "" {
		senders = append(senders, notify.NewDiscordClient(cfg.DiscordWebhookURL))
	}
	// Shared across every notification-emitting job; a no-op success when no
	// channel is configured, so it is always safe to build.
	sender := notify.NewMultiSender(senders...)
	// The expiry-notifications job is registered only when a channel is
	// configured, preserving the opt-in.
	if len(senders) > 0 {
		notifier := notify.New(
			store.NewCertificateRepo(db),
			store.NewHardwareRepo(db),
			store.NewSubscriptionRepo(db),
			store.NewNotificationRepo(db),
			sender,
			cfg.NotifyWithinDays,
		)
		runner.Register(job.Definition{
			Name:     "expiry-notifications",
			Title:    "Expiry notifications",
			Interval: cfg.NotifyInterval,
			Timeout:  time.Minute,
			Run:      func(ctx context.Context) error { return notifier.Run(ctx, time.Now()) },
		})
	}

	// Liveness checks are opt-in: a background pass of TCP dials against
	// every host/service, recording up/down transitions and notifying via
	// the same sender fan-out as expiry notifications.
	if cfg.LivenessEnabled {
		checker := liveness.New(
			store.NewHostRepo(db),
			store.NewServiceRepo(db),
			store.NewLivenessRepo(db),
			sender,
			nil, // default TCP dialer
			cfg.LivenessTimeout,
			nil, // slog.Default
			nil, // time.Now
		)
		runner.Register(job.Definition{
			Name:     "liveness",
			Title:    "Liveness checks",
			Interval: cfg.LivenessInterval,
			Timeout:  cfg.LivenessInterval, // a batch of sequential dials may exceed the 1m default
			Run:      checker.Run,
		})
		log.Println("liveness checks enabled")
	}

	go runner.Start(ctx)

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
