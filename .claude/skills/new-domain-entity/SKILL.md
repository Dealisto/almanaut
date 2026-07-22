---
name: new-domain-entity
description: Scaffold a new domain entity in almanaut following the established domain + store + migration + web pattern (e.g. host, service, network, backup, certificate, tag).
disable-model-invocation: true
---

# new-domain-entity

almanaut's entities all follow the same shape. To add a new one named `<Entity>`
(e.g. `Cluster`), create these files, matching the conventions of an existing
entity like `tag` or `service`.

## Files to create / touch

1. **`internal/domain/<entity>.go`** — the struct with `yaml:"..."` tags, plus a
   `Validate() error` method returning `fmt.Errorf` on invalid input. Add any
   normalization helpers (cf. `NormalizeTag`). If the entity references others,
   validate the reference against the existing `EntityTypes` slice.
2. **`internal/domain/<entity>_test.go`** — table-driven tests for `Validate()`
   and any helpers (valid + each invalid case).
3. **`internal/store/migrations/NNNN_<entity_plural>.sql`** — the table. Use the
   **new-migration** skill so numbering and append-only rules are respected.
4. **`internal/store/<entity>_repo.go`** — `<Entity>Repo` struct wrapping `*sql.DB`,
   a `New<Entity>Repo(db) *<Entity>Repo` constructor, and CRUD methods. **Always
   parameterize SQL** with `?`. Wrap every error with `fmt.Errorf("...: %w", err)`.
   Return `[]domain.<Entity>{}` (non-nil) from list methods.
5. **`internal/store/<entity>_repo_test.go`** — repo tests against a temp SQLite
   DB (follow the setup in an existing `*_repo_test.go`).
6. **Web layer** (`internal/web/`) — wire handlers + a `templates/<entity>.html`
   (and `<entity>_form.html` if editable) only if the entity is user-facing.
   Templates are server-rendered `html/template`; **no client JS**.

## Conventions (hold the new code to these)

- Errors wrapped with `%w`; messages lowercase, no trailing punctuation.
- Repo methods take/return `domain` types, never raw rows outward.
- Doc comment on every exported type/function.
- `gofmt` runs automatically on save (PostToolUse hook).
- Do **not** run `go test` locally — App Control blocks it; CI is the gate.

## Reference shapes

Domain struct + Validate (`internal/domain/tag.go`):
```go
type Tag struct {
    ID         int64  `yaml:"id"`
    EntityType string `yaml:"entity_type"`
    EntityID   int64  `yaml:"entity_id"`
    Name       string `yaml:"name"`
}

func (t Tag) Validate() error {
    if !contains(EntityTypes, t.EntityType) {
        return fmt.Errorf("invalid entity type %q", t.EntityType)
    }
    // ...
    return nil
}
```

Repo constructor + parameterized query (`internal/store/tag_repo.go`):
```go
type TagRepo struct{ db *sql.DB }

func NewTagRepo(db *sql.DB) *TagRepo { return &TagRepo{db: db} }

func (r *TagRepo) Add(t domain.Tag) error {
    _, err := r.db.Exec(
        `INSERT OR IGNORE INTO tags (entity_type, entity_id, name) VALUES (?, ?, ?)`,
        t.EntityType, t.EntityID, domain.NormalizeTag(t.Name),
    )
    if err != nil {
        return fmt.Errorf("insert tag: %w", err)
    }
    return nil
}
```
