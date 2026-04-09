package scopes

import (
	"context"

	"github.com/limbic-systems/stargate/internal/rules"
	"github.com/limbic-systems/stargate/internal/types"
)

// ResolverAdapter wraps *ResolverRegistry to satisfy rules.ResolverProvider.
// It bridges the scopes.Resolver signature (types.CommandInfo) to the
// rules.ResolverFunc signature, which is identical after the types extraction.
type ResolverAdapter struct {
	rr *ResolverRegistry
}

// NewResolverAdapter wraps a ResolverRegistry so it satisfies rules.ResolverProvider.
func NewResolverAdapter(rr *ResolverRegistry) *ResolverAdapter {
	return &ResolverAdapter{rr: rr}
}

// Get implements rules.ResolverProvider.
func (a *ResolverAdapter) Get(name string) (rules.ResolverFunc, bool) {
	r, ok := a.rr.Get(name)
	if !ok {
		return nil, false
	}
	return func(ctx context.Context, cmd types.CommandInfo, cwd string) (string, bool, error) {
		return r(ctx, cmd, cwd)
	}, true
}
