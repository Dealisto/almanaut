package domain

import (
	"fmt"
	"strings"
)

// MaxAttachmentBytes is the per-file upload cap (16 MiB), comfortably inside the
// 32 MiB request limit (web.maxRequestBytes) so multipart overhead still fits.
const MaxAttachmentBytes = 16 << 20

// Attachment is a file attached to an entity, addressed by (EntityType, EntityID).
// Content is populated only on a full read (download); list reads leave it nil.
type Attachment struct {
	ID          int64
	EntityType  string
	EntityID    int64
	Filename    string
	ContentType string
	Size        int64
	Content     []byte
	UploadedAt  string
	UploadedBy  string
}

// SanitizeFilename reduces an uploaded name to a safe display/download base
// name: any directory component (unix or windows separators, regardless of host
// OS) is dropped, control characters are stripped, and an empty result falls
// back to "file".
func SanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "file"
	}
	return out
}

// Validate checks the entity reference, a non-empty sanitised filename, and a
// size within (0, MaxAttachmentBytes].
func (a Attachment) Validate() error {
	if !contains(EntityTypes, a.EntityType) {
		return fmt.Errorf("invalid entity type %q", a.EntityType)
	}
	if a.EntityID <= 0 {
		return fmt.Errorf("entity id is required")
	}
	if strings.TrimSpace(a.Filename) == "" {
		return fmt.Errorf("filename is required")
	}
	if a.Size <= 0 {
		return fmt.Errorf("file is empty")
	}
	if a.Size > MaxAttachmentBytes {
		return fmt.Errorf("file exceeds %d MiB limit", MaxAttachmentBytes>>20)
	}
	return nil
}
