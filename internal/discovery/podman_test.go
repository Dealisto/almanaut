package discovery

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Podman's compatibility handler returns Docker summary fields. podman-compose
// supplies both project-label namespaces; installations can also have only its
// native label. Exercise the actual configured Unix-socket transport, not just
// a substituted HTTP client.
func TestPodmanCompatibilitySocket(t *testing.T) {
	const payload = `[
		{"Id":"podman-media","Names":["/jellyfin"],"Image":"docker.io/jellyfin/jellyfin:latest","State":"running",
		 "Labels":{"com.docker.compose.project":"media","io.podman.compose.project":"native-media"},
		 "Ports":[{"IP":"0.0.0.0","PrivatePort":8096,"PublicPort":18096,"Type":"tcp"},
		          {"IP":"::","PrivatePort":8096,"PublicPort":18096,"Type":"tcp"},
		          {"IP":"0.0.0.0","PrivatePort":7359,"PublicPort":7359,"Type":"udp"},
		          {"PrivatePort":8920,"Type":"tcp"}]},
		{"Id":"podman-native","Names":["metrics"],"Image":"quay.io/example/metrics:latest","State":"running",
		 "Labels":{"io.podman.compose.project":"monitoring"},"Ports":[]},
		{"Id":"podman-standalone","Names":["standalone"],"Image":"localhost/standalone:latest","State":"running","Labels":null,"Ports":null}
	]`
	socket := filepath.Join(t.TempDir(), "podman.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/containers/json" {
			t.Errorf("unexpected Podman request: %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	client := NewSocketClient(socket)
	t.Cleanup(client.httpClient.CloseIdleConnections)
	got, err := client.Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Container{
		{ID: "podman-media", Name: "jellyfin", Image: "docker.io/jellyfin/jellyfin:latest", State: "running", ComposeProject: "media", Ports: []Port{
			{Public: 7359, Private: 7359, Proto: "udp"},
			{Public: 18096, Private: 8096, Proto: "tcp"},
		}},
		{ID: "podman-native", Name: "metrics", Image: "quay.io/example/metrics:latest", State: "running", ComposeProject: "monitoring"},
		{ID: "podman-standalone", Name: "standalone", Image: "localhost/standalone:latest", State: "running"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containers = %+v, want %+v", got, want)
	}

	proposals := ProposeServices(got, []domain.Service{{Name: "JELLYFIN"}})
	if !proposals[0].AlreadyTracked {
		t.Error("existing service must still be detected when discovered through Podman")
	}
	if proposals[0].Service.Ports != "7359:7359/udp, 18096:8096/tcp" {
		t.Errorf("ports = %q, want published mappings without IPv4/IPv6 duplicates", proposals[0].Service.Ports)
	}
	if proposals[1].Service.Category != "monitoring" {
		t.Errorf("category = %q, want native Podman project label", proposals[1].Service.Category)
	}
	if proposals[0].Service.Notes != "Discovered from a Docker-compatible API. Image: docker.io/jellyfin/jellyfin:latest" {
		t.Errorf("provenance should not claim the engine is Docker: %q", proposals[0].Service.Notes)
	}
}

func TestContainerProvenanceWithoutImage(t *testing.T) {
	got := ProposeServices([]Container{{ID: "standalone", Name: "standalone"}}, nil)
	if got[0].Service.Notes != "Discovered from a Docker-compatible API." {
		t.Errorf("unexpected provenance: %q", got[0].Service.Notes)
	}
}
