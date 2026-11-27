package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendPreservesErrorResponseReadFailures(t *testing.T) {
	for _, status := range []int{400, 409, 429, 500} {
		for _, failure := range []string{"canceled", "timeout", "truncated"} {
			t.Run(fmt.Sprintf("%d/%s", status, failure), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				started := make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Length", "1024")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"error":"private-token`)
					w.(http.Flusher).Flush()
					if failure != "truncated" {
						<-r.Context().Done()
					}
				}))
				defer srv.Close()
				client := srv.Client()
				transport := client.Transport
				client.Transport = responseReadTransport(func(r *http.Request) (*http.Response, error) {
					resp, err := transport.RoundTrip(r)
					if err == nil {
						resp.Body = &responseReadSignal{ReadCloser: resp.Body, started: started}
					}
					return resp, err
				})
				client.Timeout = 3 * time.Second
				if failure == "timeout" {
					client.Timeout = 100 * time.Millisecond
				}
				if failure == "canceled" {
					go func() {
						select {
						case <-started:
							cancel()
						case <-ctx.Done():
						}
					}()
				}
				_, err := Send(ctx, client, srv.URL+"/private-path", "private-token", testReport())
				want := error(io.ErrUnexpectedEOF)
				switch failure {
				case "canceled":
					want = context.Canceled
				case "timeout":
					want = context.DeadlineExceeded
					var timeout net.Error
					if !errors.As(err, &timeout) || !timeout.Timeout() {
						t.Errorf("response read lost timeout classification: %v", err)
					}
				}
				if !errors.Is(err, want) {
					t.Errorf("response read error = %v, want %v", err, want)
				}
				if errors.Is(err, ErrConflict) || errors.Is(err, ErrRejected) {
					t.Errorf("incomplete response classified as permanent CLI failure: %v", err)
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "private-") {
					t.Errorf("response read lost HTTP status or exposed credentials: %v", err)
				}
			})
		}
	}
}

type responseReadTransport func(*http.Request) (*http.Response, error)

func (f responseReadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type responseReadSignal struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (r *responseReadSignal) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.ReadCloser.Read(p)
}
