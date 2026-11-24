package web

import (
	"context"
	"github.com/Dealisto/almanaut/internal/discovery"
)

type xenOrchestraScanner interface {
	Resources(context.Context) ([]discovery.XenOrchestraResource, error)
}

// XenOrchestraOptions enables the legacy default Xen Orchestra source.
type XenOrchestraOptions struct{ Enabled bool }
