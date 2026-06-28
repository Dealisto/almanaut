package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContainersParsesAPIResponse(t *testing.T) {
	const payload = `[{"Id":"abc","Names":["/jellyfin"],"Image":"jellyfin/jellyfin:latest","State":"running","Labels":{"com.docker.compose.project":"media"},"Ports":[{"PrivatePort":8096,"PublicPort":8096,"Type":"tcp"},{"PrivatePort":7359,"PublicPort":0,"Type":"udp"}]}]`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			t.Errorf("path = %q, want /containers/json", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	c := &DockerClient{httpClient: ts.Client(), baseURL: ts.URL}
	got, err := c.Containers(context.Background())
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d containers, want 1", len(got))
	}
	if got[0].Name != "jellyfin" {
		t.Errorf("Name = %q, want jellyfin (leading slash stripped)", got[0].Name)
	}
	if got[0].Image != "jellyfin/jellyfin:latest" {
		t.Errorf("Image = %q", got[0].Image)
	}
	if got[0].ComposeProject != "media" {
		t.Errorf("ComposeProject = %q, want media", got[0].ComposeProject)
	}
	// Unpublished port (PublicPort 0) must be dropped.
	if len(got[0].Ports) != 1 || got[0].Ports[0].Public != 8096 || got[0].Ports[0].Proto != "tcp" {
		t.Errorf("Ports = %+v, want one tcp 8096", got[0].Ports)
	}
}

func TestContainersErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	c := &DockerClient{httpClient: ts.Client(), baseURL: ts.URL}
	if _, err := c.Containers(context.Background()); err == nil {
		t.Fatal("expected an error on non-200 status")
	}
}
