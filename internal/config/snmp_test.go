package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSNMPConfigExplicitSourcesAndFileSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.json")
	for _, name := range []string{"auth", "privacy"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("secret-passphrase\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	valid := `[{"id":"switch","target":"192.0.2.1","username":"reader","auth_password_file":"auth","privacy_password_file":"privacy","owner_type":"hardware","owner_id":3,"interval":"5m"}]`
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	sources, err := LoadSNMPSources(path)
	if err != nil || len(sources) != 1 {
		t.Fatalf("load %v", err)
	}
	s := sources[0]
	if s.Version != "3" || s.Port != 161 || s.AuthPassword != "secret-passphrase" || s.OwnerID != 3 {
		t.Fatalf("wrong defaults: %s", s)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "secret-passphrase") || strings.Contains(fmt.Sprintf("%+v %#v", s, s), "secret-passphrase") {
		t.Fatal("credentials serialized")
	}
	for _, bad := range []string{`null`, valid + `{}`, strings.Replace(valid, `"username":"reader"`, `"community":"unsafe"`, 1), strings.Replace(valid, `"owner_id":3`, `"owner_id":0`, 1), strings.Replace(valid, `"interval":"5m"`, `"interval":"1s"`, 1), valid + strings.Repeat(" ", 1<<20)} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSNMPSources(path); err == nil {
			t.Fatalf("accepted invalid config beginning %.80s", bad)
		}
	}
	if s, err := LoadSNMPSources(""); err != nil || len(s) != 0 {
		t.Fatalf("disabled: %v", err)
	}
}
