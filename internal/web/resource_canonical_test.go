package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

func TestMonitoredEntityNoopSavePreservesHistory(t *testing.T) {
	for _, kind := range []string{"host", "service"} {
		t.Run(kind, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			form := url.Values{"name": {"box"}, "type": {"vm"}, "kind": {"container"}}
			path := "/" + kind + "s"
			postForm(t, srv, path, form)
			now := time.Now()
			if err := store.NewLivenessRepo(db).Upsert(kind, 1, domain.LivenessStatus{
				Status: domain.LivenessUp, CheckedAt: now, ChangedAt: now,
			}); err != nil {
				t.Fatal(err)
			}
			postForm(t, srv, path+"/1", form)
			events, err := store.NewChangelogRepo(db).ListForEntity(kind, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 {
				t.Fatalf("no-op save recorded %d events, want create only: %+v", len(events), events)
			}
		})
	}
}

func canonicalHostResource(t *testing.T) (resource[domain.Host], handlerDeps, *recordingDispatcher) {
	t.Helper()
	_, db := newTestServerDB(t)
	dispatcher := &recordingDispatcher{}
	rs := resource[domain.Host]{
		name: "hosts", sing: "host", repo: store.NewHostRepo(db),
		label: func(h domain.Host) string { return h.Name },
		id:    func(h domain.Host) int64 { return h.ID },
		setID: func(h *domain.Host, id int64) { h.ID = id },
	}
	d := handlerDeps{
		db: db, changelog: store.NewChangelogRepo(db),
		customFields: store.NewCustomFieldRepo(db), webhooks: dispatcher,
	}
	return rs, d, dispatcher
}

func TestEntityAPIAndWebhooksUseStoredLiveness(t *testing.T) {
	rs, d, dispatcher := canonicalHostResource(t)
	router := chi.NewRouter()
	router.Post("/api/hosts", rs.createJSON(d))
	router.Put("/api/hosts/{id}", rs.updateJSON(d))
	request := func(method, path, name string, status int, wantLive string) {
		t.Helper()
		body := fmt.Sprintf(`{"name":%q,"type":"vm","liveness":{"Status":"forged"}}`, name)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code != status {
			t.Fatalf("%s = %d: %s", method, rec.Code, rec.Body)
		}
		var got domain.Host
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.IPs == nil {
			t.Fatal("API returned null IPs, want the stored empty array")
		}
		if (wantLive == "" && got.Liveness != nil) || (wantLive != "" && (got.Liveness == nil || got.Liveness.Status != wantLive)) {
			t.Fatalf("API liveness = %+v, want %q", got.Liveness, wantLive)
		}
		events := dispatcher.all()
		var payload domain.Host
		if err := json.Unmarshal(events[len(events)-1].Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.IPs == nil {
			t.Fatal("webhook returned null IPs, want the stored empty array")
		}
		if (wantLive == "" && payload.Liveness != nil) || (wantLive != "" && (payload.Liveness == nil || payload.Liveness.Status != wantLive)) {
			t.Fatalf("webhook liveness = %+v, want %q", payload.Liveness, wantLive)
		}
	}
	request(http.MethodPost, "/api/hosts", "box", http.StatusCreated, "")
	now := time.Now()
	if err := store.NewLivenessRepo(d.db).Upsert("host", 1, domain.LivenessStatus{
		Status: domain.LivenessUp, CheckedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	request(http.MethodPut, "/api/hosts/1", "renamed", http.StatusOK, domain.LivenessUp)
	if got := len(dispatcher.all()); got != 2 {
		t.Fatalf("events = %d, want create and rename", got)
	}
	request(http.MethodPut, "/api/hosts/1", "renamed", http.StatusOK, domain.LivenessUp)
	if got := len(dispatcher.all()); got != 2 {
		t.Fatalf("derived-only submission emitted an event; count = %d", got)
	}
}

func TestEntityAPIRejectsForgedDerivedStateInResponse(t *testing.T) {
	for _, tc := range []struct {
		path, field, body string
	}{
		{"/api/services", "liveness", `{"name":"app","kind":"container","liveness":{"Status":"forged"}}`},
		{"/api/certificates", "probe", `{"subject":"example.test","expires_on":"2030-01-01","probe":{"Issuer":"forged"}}`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			srv, _, token := apiAuthServer(t)
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				path, status := tc.path, http.StatusCreated
				if method == http.MethodPut {
					path, status = path+"/1", http.StatusOK
				}
				req := httptest.NewRequest(method, path, strings.NewReader(tc.body))
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code != status {
					t.Fatalf("%s = %d: %s", method, rec.Code, rec.Body)
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if _, exists := body[tc.field]; exists {
					t.Fatalf("%s echoed unpersisted %s: %s", method, tc.field, rec.Body)
				}
			}
		})
	}
}

type failingCanonicalHostRepo struct {
	*store.HostRepo
	reads, failAt int
	err           error
}

func (r *failingCanonicalHostRepo) GetTx(tx *sql.Tx, id int64) (domain.Host, error) {
	r.reads++
	if r.reads == r.failAt {
		return domain.Host{}, r.err
	}
	return r.HostRepo.GetTx(tx, id)
}

func TestCanonicalReloadFailureRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update bool
		failAt int
	}{
		{"create event reload", false, 1},
		{"create response reload", false, 2},
		{"update event reload", true, 2},
		{"update response reload", true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs, d, dispatcher := canonicalHostResource(t)
			repo := store.NewHostRepo(d.db)
			item := domain.Host{Name: "changed", Type: "vm"}
			if tc.update {
				id, err := repo.Create(domain.Host{Name: "original", Type: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				item.ID = id
			}
			failure := errors.New("canonical reload unavailable")
			rs.repo = &failingCanonicalHostRepo{HostRepo: repo, failAt: tc.failAt, err: failure}
			var err error
			if tc.update {
				err = rs.updateEntity(d, item, nil, "test")
			} else {
				_, err = rs.createEntity(d, item, nil, "test")
			}
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want reload failure", err)
			}
			hosts, err := repo.List()
			if err != nil {
				t.Fatal(err)
			}
			if (!tc.update && len(hosts) != 0) || (tc.update && (len(hosts) != 1 || hosts[0].Name != "original")) {
				t.Fatalf("failed write persisted: %+v", hosts)
			}
			events, err := d.changelog.ListRecent(10)
			if err != nil || len(events) != 0 || len(dispatcher.all()) != 0 {
				t.Fatalf("failed write published history/events: %+v, %v, %+v", events, err, dispatcher.all())
			}
		})
	}
}
