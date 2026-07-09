package domain

import "testing"

func TestRoleCapabilities(t *testing.T) {
	cases := []struct {
		role                     Role
		valid, canWrite, isAdmin bool
	}{
		{RoleAdmin, true, true, true},
		{RoleEditor, true, true, false},
		{RoleViewer, true, false, false},
		{Role("bogus"), false, false, false},
		{Role(""), false, false, false},
	}
	for _, c := range cases {
		if c.role.Valid() != c.valid {
			t.Errorf("%q.Valid() = %v, want %v", c.role, c.role.Valid(), c.valid)
		}
		if c.role.CanWrite() != c.canWrite {
			t.Errorf("%q.CanWrite() = %v, want %v", c.role, c.role.CanWrite(), c.canWrite)
		}
		if c.role.IsAdmin() != c.isAdmin {
			t.Errorf("%q.IsAdmin() = %v, want %v", c.role, c.role.IsAdmin(), c.isAdmin)
		}
	}
}

func TestScopeCapabilities(t *testing.T) {
	if !ScopeReadWrite.Valid() || !ScopeReadWrite.CanWrite() {
		t.Error("read-write scope should be valid and writable")
	}
	if !ScopeReadOnly.Valid() || ScopeReadOnly.CanWrite() {
		t.Error("read-only scope should be valid and non-writable")
	}
	if Scope("bogus").Valid() {
		t.Error("unknown scope must be invalid")
	}
}
