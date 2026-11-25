package web

import (
	"context"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

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
			// Preserve optional interfaces (including flushing) and record the
			// first final status, including implicit 200 responses from Write.
			rec := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(rec, r)
			status := rec.Status()
			if status == 0 {
				status = http.StatusOK // An empty handler still sends 200.
			}
			logger.Printf("%s %s %d %s", r.Method, r.URL.Path, status, time.Since(start))
		})
	}
}

// contentSecurityPolicy confines the page to same-origin resources. Scripts
// must be external; confirmation handlers live in app.js. Inline styles remain
// necessary for dynamically sized rack elevations. User notes cannot inject
// raw HTML (see renderMarkdown).
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
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
	return limitBodyBytes(maxRequestBytes, next)
}

// Login forms never need the inventory upload allowance. Apply this before
// CSRF parsing so even a chunked login request has a small memory ceiling.
func limitLoginBody(next http.Handler) http.Handler {
	return limitBodyBytes(8<<10, next)
}

func limitBodyBytes(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > limit {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
			} else {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			}
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets conservative response headers on every request: block
// MIME sniffing, deny framing (clickjacking), suppress the Referer header, and
// apply the same-origin Content-Security-Policy above. Dynamic responses may
// contain inventory, API tokens, or authentication secrets, so browsers and
// shared caches must not store them. Static asset handlers override this policy
// with their own revalidation headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
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

// loggerCtxKey keys the per-request logger stored by injectLogger.
type loggerCtxKey struct{}

// injectLogger stores logger in each request's context so handlers can report
// internal errors through serverError without threading a logger parameter
// through every handler signature. Register it after middleware.RequestID.
func injectLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), loggerCtxKey{}, logger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// loggerFrom returns the logger injected by injectLogger, or log.Default() if
// none is present (e.g. a handler exercised without the middleware stack).
func loggerFrom(ctx context.Context) *log.Logger {
	if l, ok := ctx.Value(loggerCtxKey{}).(*log.Logger); ok && l != nil {
		return l
	}
	return log.Default()
}

// serverError logs an internal error with the request id, method, and path,
// then sends a generic 500 to the client. Use it for unexpected failures
// (e.g. database errors) so internal detail is never written to the response.
func serverError(w http.ResponseWriter, r *http.Request, err error) {
	id := middleware.GetReqID(r.Context())
	loggerFrom(r.Context()).Printf("server error: %s %s reqid=%q: %v", r.Method, r.URL.Path, id, err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
