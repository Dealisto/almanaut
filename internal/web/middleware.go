package web

import (
	"log"
	"net/http"
	"runtime/debug"
)

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
