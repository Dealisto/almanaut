package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const csvPreviewLifetime = 15 * time.Minute
const maxCSVPreviewCacheBytes = 32 << 20

type csvCachedPreview struct {
	owner   int64
	expires time.Time
	plan    *csvPreparedPreview
}

type csvPreviewStore struct {
	mu      sync.Mutex
	entries map[string]csvCachedPreview
	now     func() time.Time
}

func newCSVPreviewStore() *csvPreviewStore {
	return &csvPreviewStore{entries: make(map[string]csvCachedPreview), now: time.Now}
}

// add bounds retained uploads globally and per user. Existing review tokens are
// never evicted to admit another upload; users can finish or let them expire.
func (s *csvPreviewStore) add(owner int64, plan *csvPreparedPreview) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	count, bytes := 0, plan.Bytes
	for token, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, token)
			continue
		}
		bytes += entry.plan.Bytes
		if entry.owner == owner {
			count++
		}
	}
	if count >= 4 || len(s.entries) >= 32 || bytes > maxCSVPreviewCacheBytes {
		return "", errors.New("too many pending previews or this preview is too large; apply an existing preview, wait 15 minutes, or split the file")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)
	s.entries[token] = csvCachedPreview{owner: owner, expires: now.Add(csvPreviewLifetime), plan: plan}
	return token, nil
}

// take consumes a token before execution: a retried confirmation cannot create
// duplicate records, including when a client loses the successful response.
func (s *csvPreviewStore) take(owner int64, token string) (*csvPreparedPreview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok {
		return nil, false
	}
	if !s.now().Before(entry.expires) {
		delete(s.entries, token)
		return nil, false
	}
	if entry.owner != owner {
		return nil, false
	}
	delete(s.entries, token)
	return entry.plan, true
}

type csvPreviewPageData struct {
	Title   string
	Summary csvPreviewSummary
	Token   string
	Errors  []string
}

func previewCSV(cat entityCatalog, deps handlerDeps, previews *csvPreviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseMultipartForm(maxCSVPreviewBytes); err != nil {
			http.Error(w, "could not parse upload", http.StatusBadRequest)
			return
		}
		if req.MultipartForm != nil {
			defer req.MultipartForm.RemoveAll()
		}
		rs, ok := cat.resource(req.PostFormValue("type"))
		if !ok {
			http.Error(w, "unknown entity type", http.StatusBadRequest)
			return
		}
		mode := csvImportMode(req.PostFormValue("mode"))
		if mode == "" {
			mode = csvPatch
		}
		file, _, err := req.FormFile("file")
		if err != nil {
			http.Error(w, "missing import file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		plan, rowErrors, err := rs.prepareCSVPreview(req.Context(), deps, file, mode)
		if err != nil {
			serverError(w, req, err)
			return
		}
		if len(rowErrors) > 0 {
			renderStatus(w, req, "csv_preview.html", csvPreviewPageData{Title: "CSV preview", Errors: rowErrors}, http.StatusBadRequest)
			return
		}
		token, err := previews.add(currentUserID(req.Context()), plan)
		if err != nil {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		render(w, req, "csv_preview.html", csvPreviewPageData{Title: "CSV preview", Summary: plan.Summary, Token: token})
	}
}

func applyCSVPreview(_ handlerDeps, previews *csvPreviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid confirmation", http.StatusBadRequest)
			return
		}
		if req.PostForm.Get("confirm") == "" {
			http.Error(w, "you must confirm the reviewed changes", http.StatusBadRequest)
			return
		}
		plan, ok := previews.take(currentUserID(req.Context()), req.PostForm.Get("preview_token"))
		if !ok {
			http.Error(w, "preview expired, already used, or unavailable; upload the file again", http.StatusConflict)
			return
		}
		result, err := plan.apply(req.Context(), actor(req))
		if errors.Is(err, errCSVPreviewStale) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		var rowError *csvRowError
		if errors.As(err, &rowError) {
			http.Error(w, "the file no longer validates; upload again: "+rowError.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, fmt.Sprintf("/data?csv_imported=1&created=%d&updated=%d", result.Created, result.Updated), http.StatusSeeOther)
	}
}
