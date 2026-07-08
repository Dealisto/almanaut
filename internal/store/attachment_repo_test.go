package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newAttachmentRepo(t *testing.T) *AttachmentRepo {
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
	return NewAttachmentRepo(db)
}

func TestAttachmentCreateGetRoundTrip(t *testing.T) {
	repo := newAttachmentRepo(t)
	content := []byte("hello \x00 binary")
	id, err := repo.Create(domain.Attachment{
		EntityType: "host", EntityID: 1, Filename: "note.txt",
		ContentType: "text/plain", Size: int64(len(content)), Content: content,
		UploadedAt: "2026-07-08T00:00:00Z", UploadedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Filename != "note.txt" || got.ContentType != "text/plain" || !bytes.Equal(got.Content, content) {
		t.Fatalf("round-trip wrong: %+v", got)
	}
	if _, err := repo.Get(9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestAttachmentListForEntityMetadataOnly(t *testing.T) {
	repo := newAttachmentRepo(t)
	_, _ = repo.Create(domain.Attachment{EntityType: "host", EntityID: 1, Filename: "a.txt", ContentType: "text/plain", Size: 3, Content: []byte("abc"), UploadedAt: "t"})
	_, _ = repo.Create(domain.Attachment{EntityType: "host", EntityID: 1, Filename: "b.txt", ContentType: "text/plain", Size: 3, Content: []byte("xyz"), UploadedAt: "t"})
	_, _ = repo.Create(domain.Attachment{EntityType: "service", EntityID: 1, Filename: "other.txt", ContentType: "text/plain", Size: 1, Content: []byte("z"), UploadedAt: "t"})

	list, err := repo.ListForEntity("host", 1)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListForEntity: %+v, %v", list, err)
	}
	for _, a := range list {
		if a.Content != nil {
			t.Errorf("ListForEntity must not load content, got %d bytes for %q", len(a.Content), a.Filename)
		}
		if a.Filename == "" || a.Size == 0 {
			t.Errorf("metadata missing: %+v", a)
		}
	}
}

func TestAttachmentDeleteAndByEntity(t *testing.T) {
	repo := newAttachmentRepo(t)
	id, _ := repo.Create(domain.Attachment{EntityType: "host", EntityID: 1, Filename: "a.txt", ContentType: "text/plain", Size: 1, Content: []byte("a"), UploadedAt: "t"})
	other, _ := repo.Create(domain.Attachment{EntityType: "host", EntityID: 1, Filename: "b.txt", ContentType: "text/plain", Size: 1, Content: []byte("b"), UploadedAt: "t"})
	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-delete = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteByEntity("host", 1); err != nil {
		t.Fatalf("DeleteByEntity: %v", err)
	}
	if list, _ := repo.ListForEntity("host", 1); len(list) != 0 {
		t.Fatalf("DeleteByEntity left %d rows (other id was %d)", len(list), other)
	}
}
