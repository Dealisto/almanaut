package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContainerErrorResponsePreservesReadFailure(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		for _, failure := range []string{"canceled", "timeout", "truncated"} {
			t.Run(fmt.Sprintf("%d/%s", status, failure), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Length", "1024")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, "incomplete diagnostic")
					w.(http.Flusher).Flush()
					if failure != "truncated" {
						<-r.Context().Done()
					}
				}))
				defer srv.Close()
				client := srv.Client()
				client.Timeout = 3 * time.Second
				if failure == "timeout" {
					client.Timeout = 100 * time.Millisecond
				}
				transport := client.Transport
				readStarted := false
				client.Transport = inventoryReadTransport(func(r *http.Request) (*http.Response, error) {
					resp, err := transport.RoundTrip(r)
					if err == nil {
						resp.Body = &inventoryReadStart{ReadCloser: resp.Body, before: func() {
							readStarted = true
							if failure == "canceled" {
								cancel()
							}
						}}
					}
					return resp, err
				})
				c := &DockerClient{httpClient: client, baseURL: srv.URL}
				got, err := c.Containers(ctx)
				want := error(io.ErrUnexpectedEOF)
				switch failure {
				case "canceled":
					want = context.Canceled
				case "timeout":
					want = context.DeadlineExceeded
					var timeout net.Error
					if !errors.As(err, &timeout) || !timeout.Timeout() {
						t.Errorf("lost timeout classification: %v", err)
					}
				}
				if !readStarted || got != nil || !errors.Is(err, want) {
					t.Errorf("body read=%t, inventory=%v, error=%v; want %v", readStarted, got, err, want)
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) {
					t.Errorf("lost HTTP status: %v", err)
				}
				if err != nil && strings.Contains(err.Error(), "incomplete diagnostic") {
					t.Errorf("reflected an incomplete response body: %v", err)
				}
			})
		}
	}
}

func TestContainerErrorResponseRetainsBoundedDiagnostic(t *testing.T) {
	for _, body := range []string{"", " daemon unavailable \n", strings.Repeat("x", 1024)} {
		resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(body))}
		err := statusError("container", resp)
		want := "container API status 400"
		if excerpt := strings.TrimSpace(body[:min(len(body), 512)]); excerpt != "" {
			want += ": " + excerpt
		}
		if err.Error() != want {
			t.Errorf("diagnostic = %q, want %q", err, want)
		}
	}
}

type inventoryReadTransport func(*http.Request) (*http.Response, error)

func (f inventoryReadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type inventoryReadStart struct {
	io.ReadCloser
	before func()
}

func (r *inventoryReadStart) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.ReadCloser.Read(p)
}
