package classifier_test

import (
	"context"
	"testing"

	"github.com/limbic-systems/stargate/internal/classifier"
	"github.com/limbic-systems/stargate/internal/config"
	"github.com/limbic-systems/stargate/internal/corpus"
	"github.com/limbic-systems/stargate/internal/llm"
)

// TestDryRun_NoFeedbackTokenForYellow verifies that DryRun=true prevents
// feedback token generation for YELLOW decisions even when tool_use_id is set.
func TestDryRun_NoFeedbackTokenForYellow(t *testing.T) {
	clf := newClassifier(t)

	req := classifier.ClassifyRequest{
		Command: "curl https://example.com",
		Context: map[string]any{"tool_use_id": "toolu_test"},
		DryRun:  true,
	}
	resp := clf.Classify(context.Background(), req)

	if resp.Decision != "yellow" {
		t.Fatalf("expected yellow decision, got %q", resp.Decision)
	}
	if resp.FeedbackToken != nil {
		t.Errorf("DryRun=true should produce no FeedbackToken, got %q", *resp.FeedbackToken)
	}
}

// TestDryRun_YieldsFeedbackTokenWhenNotDryRun is the control — the same
// request without DryRun should produce a token.
func TestDryRun_YieldsFeedbackTokenWhenNotDryRun(t *testing.T) {
	clf := newClassifier(t)

	req := classifier.ClassifyRequest{
		Command: "curl https://example.com",
		Context: map[string]any{"tool_use_id": "toolu_test"},
		// DryRun: false (default)
	}
	resp := clf.Classify(context.Background(), req)

	if resp.Decision != "yellow" {
		t.Fatalf("expected yellow decision, got %q", resp.Decision)
	}
	if resp.FeedbackToken == nil {
		t.Error("non-dry-run YELLOW with tool_use_id should produce a FeedbackToken")
	}
}

// TestDryRun_CorpusNotWrittenWithLLMAllow verifies that DryRun suppresses
// corpus writes even on a code path that WOULD write in non-dry-run mode
// (LLM approves, corpus enabled). Without this test, the happy path of
// "no LLM provider in tests" could mask a regression where DryRun no
// longer gates postProcess.
func TestDryRun_CorpusNotWrittenWithLLMAllow(t *testing.T) {
	tmpDir := t.TempDir()

	mock := &mockProvider{response: llm.ReviewResponse{
		Decision:  "allow",
		Reasoning: "safe API call",
	}}

	cfg := llmTestConfig()
	trueVal := true
	cfg.Corpus = config.CorpusConfig{
		Enabled: &trueVal,
		Path:    tmpDir + "/corpus.db",
	}

	clf, err := classifier.NewWithProvider(cfg, mock)
	if err != nil {
		t.Fatalf("NewWithProvider: %v", err)
	}
	defer clf.Close() //nolint:errcheck

	// Baseline: non-dry-run should trigger LLM and write to corpus.
	wet := clf.Classify(context.Background(), classifier.ClassifyRequest{
		Command: "curl https://api.example.com",
	})
	if wet.Action != "allow" {
		t.Fatalf("baseline expected action=allow, got %q", wet.Action)
	}
	if wet.Corpus == nil || !wet.Corpus.EntryWritten {
		t.Fatalf("non-dry-run should have written to corpus; got Corpus=%+v", wet.Corpus)
	}
	if mock.calls != 1 {
		t.Fatalf("expected 1 LLM call before dry-run, got %d", mock.calls)
	}

	// Dry-run with a different command so the cache doesn't interfere.
	dry := clf.Classify(context.Background(), classifier.ClassifyRequest{
		Command: "curl https://different.example.com",
		DryRun:  true,
	})
	if dry.Action != "allow" {
		t.Fatalf("dry-run expected action=allow, got %q", dry.Action)
	}
	if dry.Corpus != nil && dry.Corpus.EntryWritten {
		t.Errorf("DryRun must NOT write to corpus; got EntryWritten=true")
	}
	if mock.calls != 2 {
		t.Errorf("expected LLM called once for dry-run (total 2), got %d total", mock.calls)
	}

	// Sanity: second LLM call was made but produced no corpus entry.
	// The corpus file itself shouldn't be empty (baseline wrote); we just
	// assert the dry-run didn't add a second entry via response shape.
	_ = corpus.ErrDuplicate // keep corpus import referenced
}

// TestDryRun_DecisionIdenticalToNonDryRun verifies DryRun does not change
// the classification decision itself — only side effects are suppressed.
func TestDryRun_DecisionIdenticalToNonDryRun(t *testing.T) {
	clf := newClassifier(t)
	ctx := context.Background()

	cases := []string{"git status", "ls -la", "rm -rf /", "echo hello"}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dryReq := classifier.ClassifyRequest{Command: cmd, DryRun: true}
			wetReq := classifier.ClassifyRequest{Command: cmd, DryRun: false}

			dry := clf.Classify(ctx, dryReq)
			wet := clf.Classify(ctx, wetReq)

			if dry.Decision != wet.Decision {
				t.Errorf("decision mismatch: dry=%q wet=%q", dry.Decision, wet.Decision)
			}
			if dry.Action != wet.Action {
				t.Errorf("action mismatch: dry=%q wet=%q", dry.Action, wet.Action)
			}
		})
	}
}
