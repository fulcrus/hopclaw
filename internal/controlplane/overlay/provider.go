package overlay

import (
	"github.com/fulcrus/hopclaw/controlplane"
)

// Provider exposes the effective control-plane view consumed by runtime and
// operator surfaces. Callers depend on this interface instead of the concrete
// resolver implementation so the overlay compiler can evolve independently.
type Provider = controlplane.EffectiveConfigProvider
