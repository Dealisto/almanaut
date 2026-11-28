package backup

import (
	"context"
	"fmt"
	"os"

	"github.com/Dealisto/almanaut/internal/store"
)

// Create copies an existing Almanaut database to a new private file and verifies
// the copy. It includes committed WAL data without changing the source schema.
// Failed copies are removed; existing destinations are never overwritten.
func Create(ctx context.Context, source, destination string) (Verification, error) {
	db, err := openReadOnly(ctx, source)
	if err != nil {
		return Verification{}, err
	}
	defer db.Close()
	if _, err := schemaInfo(ctx, db); err != nil {
		return Verification{}, err
	}
	if err := store.SnapshotDatabase(ctx, db, destination); err != nil {
		return Verification{}, err
	}
	info, err := Verify(ctx, destination)
	if err != nil {
		_ = os.Remove(destination)
		return Verification{}, fmt.Errorf("verify snapshot: %w", err)
	}
	return info, nil
}
