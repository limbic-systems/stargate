// Package scopes provides the scope registry for contextual trust matching.
package scopes

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
)

// Registry holds scope name → pattern mappings for contextual trust matching.
// It is safe for concurrent read access after construction.
type Registry struct {
	scopes map[string][]string
}

// NewRegistry constructs a Registry from a map of scope name to patterns.
// It validates all patterns and rejects bare wildcards ("*" and "**") that
// would defeat the scope layer by matching everything.
func NewRegistry(scopes map[string][]string) (*Registry, error) {
	for name, patterns := range scopes {
		for _, p := range patterns {
			if p == "*" || p == "**" {
				return nil, fmt.Errorf("scope %q: bare wildcard pattern %q is not allowed (it matches everything, defeating the scope layer)", name, p)
			}
			if !doublestar.ValidatePattern(p) {
				return nil, fmt.Errorf("scope %q: invalid glob pattern %q", name, p)
			}
		}
	}

	// Deep-copy the map so callers cannot mutate registry state after construction.
	copied := make(map[string][]string, len(scopes))
	for name, patterns := range scopes {
		ps := make([]string, len(patterns))
		copy(ps, patterns)
		copied[name] = ps
	}

	return &Registry{scopes: copied}, nil
}

// Match reports whether value matches any pattern in the named scope.
// Returns false (fail-closed) if the scope does not exist or has no patterns.
func (r *Registry) Match(scopeName string, value string) bool {
	patterns, ok := r.scopes[scopeName]
	if !ok {
		return false
	}
	for _, p := range patterns {
		matched, err := doublestar.Match(p, value)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// Has reports whether the named scope exists in the registry.
// Used during config validation to verify referenced scopes are defined.
func (r *Registry) Has(scopeName string) bool {
	_, ok := r.scopes[scopeName]
	return ok
}

// Scopes returns a deep copy of the scope map for LLM prompt injection (§4.5).
// Callers may not use the returned map to mutate registry state.
func (r *Registry) Scopes() map[string][]string {
	out := make(map[string][]string, len(r.scopes))
	for name, patterns := range r.scopes {
		ps := make([]string, len(patterns))
		copy(ps, patterns)
		out[name] = ps
	}
	return out
}
