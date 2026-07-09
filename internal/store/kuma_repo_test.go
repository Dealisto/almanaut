package store

import "testing"

func TestKumaRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewKumaRepo(db)

	all, err := repo.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty mapping, got %v", all)
	}

	if err := repo.Put(1, 100); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := repo.Put(2, 200); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Upsert: re-pointing a service to a new monitor replaces the row.
	if err := repo.Put(1, 101); err != nil {
		t.Fatalf("Put upsert: %v", err)
	}

	all, err = repo.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 || all[1] != 101 || all[2] != 200 {
		t.Fatalf("mapping = %v, want {1:101 2:200}", all)
	}

	if err := repo.Delete(1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(99); err != nil { // absent row is not an error
		t.Fatalf("Delete absent: %v", err)
	}
	all, _ = repo.All()
	if len(all) != 1 || all[2] != 200 {
		t.Fatalf("mapping after delete = %v, want {2:200}", all)
	}
}
