package web

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/go-chi/chi/v5"
)

// addAttachment stores an uploaded file against this resource's entity, then
// redirects back to the entity detail page. Mirrors addJournal (per-resource
// route), so the entity type comes from rs.sing and the id from the URL.
func (rs resource[T]) addAttachment(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		// The csrfProtect middleware has already parsed the multipart form (via
		// FormValue) using net/http's default 32 MiB maxMemory, so this call is
		// effectively an idempotent safety net; limitBody caps the whole request
		// at 32 MiB and the per-file cap is enforced on the read below.
		if err := req.ParseMultipartForm(domain.MaxAttachmentBytes); err != nil {
			http.Error(w, "could not parse upload", http.StatusBadRequest)
			return
		}
		defer func() {
			if req.MultipartForm != nil {
				_ = req.MultipartForm.RemoveAll()
			}
		}()
		file, header, err := req.FormFile("file")
		if err != nil {
			http.Error(w, "missing file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Read at most the cap + 1 byte so an oversize file is detected without
		// buffering unbounded data.
		content, err := io.ReadAll(io.LimitReader(file, domain.MaxAttachmentBytes+1))
		if err != nil {
			serverError(w, req, err)
			return
		}
		if int64(len(content)) > domain.MaxAttachmentBytes {
			http.Error(w, fmt.Sprintf("file exceeds %d MiB limit", domain.MaxAttachmentBytes>>20), http.StatusRequestEntityTooLarge)
			return
		}

		ct := header.Header.Get("Content-Type")
		if ct == "" || ct == "application/octet-stream" {
			ct = http.DetectContentType(content) // sniffs first 512 bytes
		}

		att := domain.Attachment{
			EntityType:  rs.sing,
			EntityID:    id,
			Filename:    domain.SanitizeFilename(header.Filename),
			ContentType: ct,
			Size:        int64(len(content)),
			Content:     content,
			UploadedAt:  nowRFC3339(),
			UploadedBy:  actor(req),
		}
		if err := att.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := d.attachments.Create(att); err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, fmt.Sprintf("%s/%d", rs.basePath(), id), http.StatusSeeOther)
	}
}

// downloadAttachment streams an attachment as a forced download. It never serves
// inline: Content-Disposition: attachment + X-Content-Type-Options: nosniff so
// an uploaded HTML/SVG cannot execute in the app origin (stored-XSS defence).
func downloadAttachment(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		att, err := d.attachments.Get(id)
		if err != nil {
			notFoundOrServerError(w, req, "attachment", err)
			return
		}
		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.Filename))
		w.Header().Set("Content-Length", strconv.FormatInt(att.Size, 10))
		_, _ = w.Write(att.Content)
	}
}

// deleteAttachment removes one attachment and redirects back to its entity's
// detail page (resolved from the stored row, like deleteJournal).
func deleteAttachment(cat entityCatalog, d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		att, err := d.attachments.Get(id)
		if err != nil {
			notFoundOrServerError(w, req, "attachment", err)
			return
		}
		if err := d.attachments.Delete(id); err != nil {
			notFoundOrServerError(w, req, "attachment", err)
			return
		}
		http.Redirect(w, req, cat.path(att.EntityType, att.EntityID), http.StatusSeeOther)
	}
}

// humanizeBytes renders a byte count as B / KiB / MiB for the detail list.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
