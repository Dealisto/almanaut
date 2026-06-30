package web

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// statusRecorder remembers the status code written to the response so the
// request logger can report it. Status defaults to 200 (the value net/http
// uses when a handler writes a body without calling WriteHeader).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// requestLogger returns middleware that logs one line per request
// ("<method> <path> <status> <duration>") through the given logger, and copies
// the chi request ID from context into the X-Request-Id response header.
// It never alters the response status or body. It must be registered after
// middleware.RequestID so the id is present in context.
func requestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := middleware.GetReqID(r.Context()); id != "" {
				w.Header().Set("X-Request-Id", id)
			}
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			logger.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
		})
	}
}

// contentSecurityPolicy confines the page to same-origin resources. It keeps
// 'unsafe-inline' for scripts and styles because the UI relies on an inline
// <style> block and inline onsubmit="confirm(...)" handlers on the delete
// forms; raw HTML in user notes is already escaped (see renderMarkdown), so the
// residual value here is blocking external resource loads, framing, base-uri
// hijacking, and cross-origin form submission.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'"

// maxRequestBytes caps the size of any request body. The only large upload is
// the data import (an import replaces all data, so a real export is far below
// this); every other route takes small forms. The cap must be applied before
// csrfProtect, which reads the form body of every unsafe request to check the
// token — without it, an authenticated client could exhaust memory with a huge
// upload (or a small file that decodes into enormous in-memory structures)
// before any handler runs.
const maxRequestBytes = 32 << 20 // 32 MiB

// limitBody rejects requests whose declared length exceeds maxRequestBytes with
// 413, and caps the readable body at that size for clients that lie about (or
// omit) Content-Length, so a chunked upload still cannot read past the ceiling.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets conservative response headers on every request: block
// MIME sniffing, deny framing (clickjacking), suppress the Referer header, and
// apply the same-origin Content-Security-Policy above.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// recoverer returns middleware that converts a handler panic into a clean 500,
// logging the panic value and stack through the given logger. It re-panics on
// http.ErrAbortHandler, the net/http sentinel that must propagate untouched.
//
// Handlers in this package render into a buffer before writing (see render),
// so a panic occurs before any bytes reach the client; writing the 500 header
// here is therefore safe.
func recoverer(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				logger.Printf("panic: %v\n%s", rec, debug.Stack())
				w.WriteHeader(http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
