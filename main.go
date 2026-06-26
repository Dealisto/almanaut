// Command almanaut is a lightweight, self-hosted homelab inventory and
// documentation server.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/almanaut/almanaut/internal/config"
	"github.com/almanaut/almanaut/internal/store"
	"github.com/almanaut/almanaut/internal/web"
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

	handler := web.New(store.NewHostRepo(db))
	log.Printf("Almanaut listening on %s (data: %s)", cfg.Addr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
