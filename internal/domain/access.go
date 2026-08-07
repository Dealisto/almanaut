package domain

// Role is a built-in access-control role. The set is fixed and mapped to
// capabilities in code — there are deliberately no user-defined roles.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Roles lists the valid roles, most-privileged first (for form selectors).
var Roles = []Role{RoleAdmin, RoleEditor, RoleViewer}

// Valid reports whether r is one of the built-in roles.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleEditor, RoleViewer:
		return true
	}
	return false
}

// CanWrite reports whether the role may create/update/delete inventory.
func (r Role) CanWrite() bool { return r == RoleAdmin || r == RoleEditor }

// IsAdmin reports whether the role may administer users and app settings.
func (r Role) IsAdmin() bool { return r == RoleAdmin }

// Scope is an API token's permission ceiling, independent of its owner's role.
// A request's effective write permission is the intersection of role and scope.
type Scope string

const (
	ScopeReadOnly  Scope = "read-only"
	ScopeReadWrite Scope = "read-write"
	// ScopeAgent is the inventory agent's report-only ceiling: it authenticates
	// POST /api/agent/report and nothing else. It deliberately fails CanWrite so
	// the generic entity API stays closed to it.
	ScopeAgent Scope = "agent"
)

// Scopes lists the valid token scopes (for form selectors).
var Scopes = []Scope{ScopeReadWrite, ScopeReadOnly, ScopeAgent}

// Valid reports whether s is one of the built-in scopes.
func (s Scope) Valid() bool {
	return s == ScopeReadOnly || s == ScopeReadWrite || s == ScopeAgent
}

// CanWrite reports whether a token with this scope may perform mutations.
func (s Scope) CanWrite() bool { return s == ScopeReadWrite }
