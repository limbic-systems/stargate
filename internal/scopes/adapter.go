package scopes

import "github.com/limbic-systems/stargate/internal/types"

// ResolverAdapter wraps *ResolverRegistry to satisfy types.ResolverProvider.
type ResolverAdapter struct {
	rr *ResolverRegistry
}

// NewResolverAdapter wraps a ResolverRegistry so it satisfies types.ResolverProvider.
func NewResolverAdapter(rr *ResolverRegistry) *ResolverAdapter {
	return &ResolverAdapter{rr: rr}
}

// Get implements types.ResolverProvider. Resolver and types.ResolverFunc have
// identical signatures, so a direct type conversion avoids closure overhead.
func (a *ResolverAdapter) Get(name string) (types.ResolverFunc, bool) {
	r, ok := a.rr.Get(name)
	if !ok {
		return nil, false
	}
	return types.ResolverFunc(r), true
}
