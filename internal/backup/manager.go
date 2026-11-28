package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/Dealisto/almanaut/internal/store"
)

const (
	manifestFormat = "almanaut-database-backup-v1"
	databaseName   = "database.sqlite"
	manifestName   = "manifest.json"
)

var ownedDirectory = regexp.MustCompile(`^almanaut-backup-[0-9]+$`)

// Manifest is published only after snapshot verification has succeeded. It is
// durable evidence of the last successful backup even after server restarts.
type Manifest struct {
	Format        string       `json:"format"`
	Directory     string       `json:"directory"`
	Source        string       `json:"source"`
	CreatedAt     time.Time    `json:"created_at"`
	ServerVersion string       `json:"server_version"`
	SHA256        string       `json:"sha256"`
	Verification  Verification `json:"verification"`
}

// Manager creates verified local snapshots and retains a bounded history. It
// does not own the database handle. Run can be registered with the job runner.
type Manager struct {
	db            *sql.DB
	source        string
	directory     string
	keep          int
	version       string
	gate          chan struct{}
	syncDirectory func(string) error
}

// New configures a manager without creating directories or taking a snapshot.
// The source path must identify the supplied database, which must already exist.
func New(db *sql.DB, sourcePath, directory string, keep int, version string) (*Manager, error) {
	if db == nil || sourcePath == "" || directory == "" || keep < 1 {
		return nil, fmt.Errorf("backup requires a database, source path, directory, and positive retention count")
	}
	source, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("resolve backup source: %w", err)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return nil, fmt.Errorf("resolve existing backup source: %w", err)
	}
	sourceInfo, err := os.Stat(source)
	if err != nil || !sourceInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("backup source must be an existing regular database file")
	}
	// A mislabeled handle would produce valid backups of a different database.
	rows, err := db.Query(`PRAGMA database_list`)
	if err != nil {
		return nil, fmt.Errorf("identify backup database: %w", err)
	}
	matched := false
	for rows.Next() {
		var sequence int
		var name, path string
		if err := rows.Scan(&sequence, &name, &path); err != nil {
			rows.Close()
			return nil, fmt.Errorf("identify backup database: %w", err)
		}
		if name == "main" {
			info, err := os.Stat(path)
			matched = err == nil && os.SameFile(sourceInfo, info)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("identify backup database: %w", err)
	}
	if !matched {
		return nil, fmt.Errorf("backup source does not identify the supplied database")
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve backup directory: %w", err)
	}
	return &Manager{db: db, source: source, directory: directory, keep: keep, version: version, gate: make(chan struct{}, 1), syncDirectory: store.SyncDirectory}, nil
}

// Run creates a private per-run directory, writes and verifies its database,
// publishes a manifest, then prunes only older recognized snapshots of this
// source. A failed backup never triggers retention. Incomplete run directories
// left by a process crash have no manifest and are ignored by retention.
func (m *Manager) Run(ctx context.Context) error {
	select {
	case m.gate <- struct{}{}:
		defer func() { <-m.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(m.directory, 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	dir, err := os.MkdirTemp(m.directory, "almanaut-backup-")
	if err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			// Only the new private run directory belongs to this operation.
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, databaseName)
	if err := store.SnapshotDatabase(ctx, m.db, path); err != nil {
		return err
	}
	info, err := Verify(ctx, path)
	if err != nil {
		return fmt.Errorf("verify scheduled snapshot: %w", err)
	}
	digest, err := hashFile(ctx, path)
	if err != nil {
		return err
	}
	manifest := Manifest{
		Format: manifestFormat, Directory: filepath.Base(dir), Source: m.source,
		CreatedAt: time.Now().UTC(), ServerVersion: m.version, SHA256: digest,
		Verification: info,
	}
	if err := writeManifest(ctx, filepath.Join(dir, manifestName), manifest); err != nil {
		return err
	}
	// Flush both the completed directory contents and its entry in the backup
	// root before retention can remove an older good snapshot.
	if err := m.syncDirectory(dir); err != nil {
		return fmt.Errorf("sync completed backup directory: %w", err)
	}
	if err := m.syncDirectory(m.directory); err != nil {
		return fmt.Errorf("sync backup root: %w", err)
	}
	published = true
	if err := m.prune(ctx, manifest.Directory); err != nil {
		return fmt.Errorf("snapshot saved, but retention failed: %w", err)
	}
	return nil
}

// List returns recognized completed snapshots for this source, newest first.
// Filesystem readback survives process restarts. It does not imply that the
// snapshot contents have been re-verified since the manifest was published.
func (m *Manager) List(ctx context.Context) ([]Manifest, error) {
	entries, err := os.ReadDir(m.directory)
	if os.IsNotExist(err) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	out := []Manifest{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !ownedDirectory.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(m.directory, entry.Name())
		files, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue // Retention may have removed this snapshot after ReadDir.
		}
		if err != nil {
			return nil, fmt.Errorf("inspect backup directory: %w", err)
		}
		// Do not claim ownership of directories containing unrelated files.
		if len(files) != 2 || files[0].Name() != databaseName || files[1].Name() != manifestName {
			continue
		}
		var regular = true
		for _, f := range files {
			info, err := f.Info()
			if os.IsNotExist(err) {
				regular = false
				break
			}
			if err != nil {
				return nil, fmt.Errorf("inspect backup file: %w", err)
			}
			if !info.Mode().IsRegular() {
				regular = false
				break
			}
		}
		if !regular {
			continue
		}
		f, err := os.Open(filepath.Join(dir, manifestName))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read backup manifest: %w", err)
		}
		var manifest Manifest
		decoder := json.NewDecoder(io.LimitReader(f, 16*1024))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&manifest)
		var extra any
		end := decoder.Decode(&extra)
		f.Close()
		if err != nil || end != io.EOF || manifest.Format != manifestFormat || manifest.Directory != entry.Name() || manifest.Source != m.source || manifest.CreatedAt.IsZero() || manifest.Verification.Bytes <= 0 {
			continue
		}
		digest, err := hex.DecodeString(manifest.SHA256)
		if err != nil || len(digest) != sha256.Size {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, databaseName))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect snapshot size: %w", err)
		}
		if info.Size() != manifest.Verification.Bytes {
			continue
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Directory > out[j].Directory
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Manager) prune(ctx context.Context, current string) error {
	backups, err := m.List(ctx)
	if err != nil {
		return err
	}
	source, err := os.Stat(m.source)
	if err != nil {
		return fmt.Errorf("identify source before retention: %w", err)
	}
	// Always retain the snapshot just completed, including after a clock jump.
	retained := 1
	for _, snapshot := range backups {
		if snapshot.Directory == current {
			continue
		}
		if retained < m.keep {
			retained++
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := filepath.Join(m.directory, snapshot.Directory)
		path := filepath.Join(dir, databaseName)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if os.SameFile(source, info) {
			continue // Never delete the active source, even if it was restored here.
		}
		// Remove only recognized files, never recursively delete retained data.
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
			return err
		}
		if err := os.Remove(dir); err != nil {
			return err
		}
	}
	return nil
}

func writeManifest(ctx context.Context, path string, manifest Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup manifest: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(manifest); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync backup manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close backup manifest: %w", err)
	}
	return ctx.Err()
}

func hashFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open snapshot for checksum: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		_, _ = h.Write(buf[:n])
		if err == io.EOF {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
		if err != nil {
			return "", fmt.Errorf("checksum snapshot: %w", err)
		}
	}
}
