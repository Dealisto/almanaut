package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newAccountRepo(t *testing.T) *AccountRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewAccountRepo(db)
}

func TestAccountRepoCRUD(t *testing.T) {
	repo := newAccountRepo(t)

	id, err := repo.Create(domain.Account{
		Name: "Proxmox root", Kind: "admin", Username: "root@pam",
		PasswordManager: "Bitwarden", SecretRef: "Homelab > Proxmox",
		URL: "https://pve.lan:8006", Status: "active", Notes: "primary node",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Proxmox root" || got.Username != "root@pam" || got.SecretRef != "Homelab > Proxmox" {
		t.Errorf("Get returned %+v", got)
	}

	updated := domain.Account{
		ID: id, Name: "Proxmox root", Kind: "admin", Username: "root@pam",
		PasswordManager: "1Password", SecretRef: "Vault/Proxmox",
		URL: "https://pve.lan:8006", Status: "disabled", Notes: "rotated",
	}
	if err := repo.Update(updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = repo.Get(id)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got != updated {
		t.Errorf("Update did not persist all fields:\n got  %+v\n want %+v", got, updated)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err = repo.List()
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}
