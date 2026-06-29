// Command almanaut is a lightweight, self-hosted homelab inventory and
// documentation server.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Dealisto/almanaut/internal/config"
	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/web"
)

func main() {
	cfg := config.Load()

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

	handler := web.New(
		store.NewHostRepo(db),
		store.NewServiceRepo(db),
		store.NewNetworkRepo(db),
		store.NewDomainRepo(db),
		store.NewCertificateRepo(db),
		store.NewBackupRepo(db),
		store.NewHardwareRepo(db),
		store.NewRelationshipRepo(db),
		store.NewTagRepo(db),
		db,
		discovery.NewSocketClient(cfg.DockerSocket),
		discovery.NewNetworkScanner(),
		web.NetDiscoveryOptions{Enabled: cfg.NetworkScanEnabled, DefaultSubnet: cfg.ScanSubnet},
		discovery.NewProxmoxClient(cfg.ProxmoxURL, cfg.ProxmoxToken, cfg.ProxmoxInsecure),
		web.ProxmoxOptions{Enabled: cfg.ProxmoxURL != "" && cfg.ProxmoxToken != ""},
	)
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
