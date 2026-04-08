package scopes_test

import (
	"testing"

	"github.com/limbic-systems/stargate/internal/scopes"
)

// --- Exact match ---

func TestMatchExact(t *testing.T) {
	r, err := scopes.NewRegistry(map[string][]string{
		"github-orgs": {"derek", "my-org"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !r.Match("github-orgs", "derek") {
		t.Error("expected 'derek' to match scope github-orgs")
	}
	if !r.Match("github-orgs", "my-org") {
		t.Error("expected 'my-org' to match scope github-orgs")
	}
	if r.Match("github-orgs", "evil-org") {
		t.Error("expected 'evil-org' NOT to match scope github-orgs")
	}
}

// --- Glob matching ---

func TestMatchGlob(t *testing.T) {
	r, err := scopes.NewRegistry(map[string][]string{
		"domains":  {"*.example.com"},
		"clusters": {"dev-*", "staging-*"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Single-star glob matches one path segment.
	if !r.Match("domains", "api.example.com") {
		t.Error("expected 'api.example.com' to match '*.example.com'")
	}
	// "example.com" has no prefix before the dot, so * (which requires ≥1 char) does not match.
	if r.Match("domains", "example.com") {
		t.Error("expected 'example.com' NOT to match '*.example.com' (documented behavior)")
	}

	// But when the exact value is also listed as a pattern it should match.
	r2, err := scopes.NewRegistry(map[string][]string{
		"domains": {"*.example.com", "example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r2.Match("domains", "example.com") {
		t.Error("expected 'example.com' to match because 'example.com' is an explicit pattern")
	}

	if !r.Match("clusters", "dev-cluster") {
		t.Error("expected 'dev-cluster' to match 'dev-*'")
	}
	if r.Match("clusters", "prod-cluster") {
		t.Error("expected 'prod-cluster' NOT to match ['dev-*', 'staging-*']")
	}
}

// --- Missing scope (fail-closed) ---

func TestMatchMissingScope(t *testing.T) {
	r, err := scopes.NewRegistry(map[string][]string{
		"existing": {"value"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Match("nonexistent", "value") {
		t.Error("expected false for nonexistent scope (fail-closed)")
	}
}

// --- Empty scope list ---

func TestMatchEmptyScope(t *testing.T) {
	r, err := scopes.NewRegistry(map[string][]string{
		"empty": {},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Match("empty", "value") {
		t.Error("expected false for scope with no patterns")
	}
}

// --- Config validation ---

func TestNewRegistryRejectsBarestar(t *testing.T) {
	_, err := scopes.NewRegistry(map[string][]string{
		"bad": {"*"},
	})
	if err == nil {
		t.Error("expected error for bare '*' pattern")
	}
}

func TestNewRegistryRejectsDoublestar(t *testing.T) {
	_, err := scopes.NewRegistry(map[string][]string{
		"bad": {"**"},
	})
	if err == nil {
		t.Error("expected error for bare '**' pattern")
	}
}

func TestNewRegistryAcceptsWildcardPatterns(t *testing.T) {
	_, err := scopes.NewRegistry(map[string][]string{
		"ok": {"*.example.com", "my-*"},
	})
	if err != nil {
		t.Errorf("unexpected error for valid wildcard patterns: %v", err)
	}
}

func TestNewRegistryRejectsInvalidGlob(t *testing.T) {
	_, err := scopes.NewRegistry(map[string][]string{
		"bad": {"[invalid"},
	})
	if err == nil {
		t.Error("expected error for invalid glob pattern '[invalid'")
	}
}

// --- Has method ---

func TestHas(t *testing.T) {
	r, err := scopes.NewRegistry(map[string][]string{
		"present": {"foo"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Has("present") {
		t.Error("expected Has to return true for existing scope")
	}
	if r.Has("absent") {
		t.Error("expected Has to return false for missing scope")
	}
}

// --- Scopes returns a copy ---

func TestScopesReturnsCopy(t *testing.T) {
	original := map[string][]string{
		"orgs": {"derek", "my-org"},
	}
	r, err := scopes.NewRegistry(original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := r.Scopes()

	// Mutate the returned map and its slice.
	got["orgs"] = append(got["orgs"], "injected-org")
	got["new-scope"] = []string{"injected"}

	// The registry should be unaffected.
	if r.Has("new-scope") {
		t.Error("mutating returned map should not affect registry")
	}
	if r.Match("orgs", "injected-org") {
		t.Error("appending to returned slice should not affect registry patterns")
	}

	// Original input map mutation should also not have affected the registry.
	original["orgs"] = []string{"evil"}
	if !r.Match("orgs", "derek") {
		t.Error("mutating original input map should not affect registry")
	}
}
