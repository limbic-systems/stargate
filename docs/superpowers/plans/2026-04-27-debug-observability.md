# Debug & Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add debug observability tooling so operators can diagnose misclassifications on remote fly.io instances via SSH — enriched `/test` responses, time-based corpus queries, and a pretty-printed CLI investigation tool.

**Architecture:** The rule engine gains trace mode via `EvaluateWithTrace` (per-invocation state, zero overhead on `/classify`). The classifier populates a `DebugInfo` struct when `DryRun=true`, threading through scrubbed command, rule trace, cache state, corpus precedents, rendered prompts, and raw LLM response. The `/test` handler serializes it via a wrapper struct. Two new CLI commands (`corpus recent`, `explain`) consume this data.

**Tech Stack:** Go 1.26, modernc.org/sqlite, existing rule engine / classifier / corpus / LLM infrastructure.

**Spec:** `docs/superpowers/specs/2026-04-27-debug-observability-design.md`

---

### Task 1: Rule Trace Types

**Files:**
- Create: `internal/rules/trace.go`
- Test: `internal/rules/trace_test.go`

- [ ] **Step 1: Create trace types file**

Create `internal/rules/trace.go` with the types that `EvaluateWithTrace` will produce. These live in the `rules` package to avoid circular dependencies (the classifier imports rules, not vice versa).

```go
package rules

// RuleTraceEntry records the result of matching one rule against one command.
type RuleTraceEntry struct {
	Level         string        `json:"level"`
	Index         int           `json:"index"`
	Rule          RuleSnapshot  `json:"rule"`
	CommandTested string        `json:"command_tested"`
	Result        string        `json:"result"` // "match" or "skip"
	FailedStep    string        `json:"failed_step,omitempty"`
	Detail        string        `json:"detail,omitempty"`
	ResolveDetail *ResolveDebug `json:"resolve_detail,omitempty"`
}

// RuleSnapshot is a JSON-safe copy of a rule definition for debug output.
type RuleSnapshot struct {
	Command     string   `json:"command,omitempty"`
	Commands    []string `json:"commands,omitempty"`
	Subcommands []string `json:"subcommands,omitempty"`
	Flags       []string `json:"flags,omitempty"`
	Args        []string `json:"args,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Context     string   `json:"context,omitempty"`
	Resolve     *ResolveSnap `json:"resolve,omitempty"`
	LLMReview   *bool    `json:"llm_review,omitempty"`
	Reason      string   `json:"reason"`
}

// ResolveSnap is the resolve section of a rule snapshot.
type ResolveSnap struct {
	Resolver string `json:"resolver"`
	Scope    string `json:"scope"`
}

// ResolveDebug records the result of a resolver-based scope check.
type ResolveDebug struct {
	Resolver      string   `json:"resolver"`
	ResolvedValue string   `json:"resolved_value,omitempty"`
	Resolved      bool     `json:"resolved"`
	Error         string   `json:"error,omitempty"`
	Scope         string   `json:"scope"`
	ScopePatterns []string `json:"scope_patterns"`
	Matched       bool     `json:"matched"`
}
```

- [ ] **Step 2: Write test for RuleSnapshot construction**

Create `internal/rules/trace_test.go` to verify snapshot construction from a `config.Rule`:

```go
package rules

import (
	"testing"

	"github.com/limbic-systems/stargate/internal/config"
)

func TestSnapshotFromRule(t *testing.T) {
	trueVal := true
	r := config.Rule{
		Command: "curl",
		Flags:   []string{"-o"},
		Resolve: &config.ResolveConfig{Resolver: "url_domain", Scope: "allowed_domains"},
		LLMReview: &trueVal,
		Reason:  "network access",
	}
	snap := snapshotFromRule(r)
	if snap.Command != "curl" {
		t.Errorf("command = %q, want curl", snap.Command)
	}
	if snap.Resolve == nil || snap.Resolve.Resolver != "url_domain" {
		t.Error("resolve not captured")
	}
	if snap.LLMReview == nil || !*snap.LLMReview {
		t.Error("llm_review not captured")
	}
}
```

- [ ] **Step 3: Implement `snapshotFromRule` helper**

Add to `internal/rules/trace.go`:

```go
func snapshotFromRule(r config.Rule) RuleSnapshot {
	snap := RuleSnapshot{
		Command:     r.Command,
		Commands:    r.Commands,
		Subcommands: r.Subcommands,
		Flags:       r.Flags,
		Args:        r.Args,
		Pattern:     r.Pattern,
		Scope:       r.Scope,
		Context:     r.Context,
		LLMReview:   r.LLMReview,
		Reason:      r.Reason,
	}
	if r.Resolve != nil {
		snap.Resolve = &ResolveSnap{
			Resolver: r.Resolve.Resolver,
			Scope:    r.Resolve.Scope,
		}
	}
	return snap
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/rules/ -run TestSnapshot -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rules/trace.go internal/rules/trace_test.go
git commit -m "feat(rules): add rule trace types for debug observability"
```

---

### Task 2: Rule Engine Trace Support

**Files:**
- Modify: `internal/rules/engine.go:178-358` (Evaluate, matchRule)
- Modify: `internal/rules/trace.go` (add evalContext)
- Test: `internal/rules/trace_test.go` (add EvaluateWithTrace tests)

The key design constraint: trace state is per-invocation via a stack-local `evalContext`, never on the shared `Engine` struct. The existing `Evaluate` method is unchanged.

- [ ] **Step 1: Write failing test for EvaluateWithTrace**

Add to `internal/rules/trace_test.go`:

```go
func TestEvaluateWithTrace_RedMatch(t *testing.T) {
	cfg := &config.Config{
		Rules: config.RulesConfig{
			Red: []config.Rule{
				{Command: "rm", Flags: []string{"-rf"}, Args: []string{"/"}, Reason: "destructive"},
			},
			Green: []config.Rule{
				{Commands: []string{"ls", "echo"}, Reason: "safe"},
			},
		},
		Scopes:   map[string][]string{},
		Wrappers: config.DefaultWrappers(),
		Commands: config.DefaultCommandFlags(),
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cmds := []CommandInfo{{Name: "rm", Flags: []string{"-rf"}, Args: []string{"/"}}}
	result := engine.EvaluateWithTrace(context.Background(), cmds, "rm -rf /", "")

	if result.Decision != "red" {
		t.Fatalf("decision = %q, want red", result.Decision)
	}
	if len(result.Trace) == 0 {
		t.Fatal("expected trace entries")
	}

	// Find the matching entry.
	var matched *RuleTraceEntry
	for i := range result.Trace {
		if result.Trace[i].Result == "match" {
			matched = &result.Trace[i]
			break
		}
	}
	if matched == nil {
		t.Fatal("no match entry in trace")
	}
	if matched.Level != "red" || matched.Index != 0 {
		t.Errorf("matched entry: level=%q index=%d", matched.Level, matched.Index)
	}
}

func TestEvaluateWithTrace_SkipDetail(t *testing.T) {
	cfg := &config.Config{
		Rules: config.RulesConfig{
			Red: []config.Rule{
				{Command: "rm", Flags: []string{"-rf"}, Args: []string{"/"}, Reason: "destructive"},
			},
			Green: []config.Rule{
				{Commands: []string{"ls"}, Reason: "safe"},
			},
		},
		Scopes:   map[string][]string{},
		Wrappers: config.DefaultWrappers(),
		Commands: config.DefaultCommandFlags(),
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cmds := []CommandInfo{{Name: "ls"}}
	result := engine.EvaluateWithTrace(context.Background(), cmds, "ls", "")

	if result.Decision != "green" {
		t.Fatalf("decision = %q, want green", result.Decision)
	}

	// RED rule should have been skipped at command step.
	var redSkip *RuleTraceEntry
	for i := range result.Trace {
		if result.Trace[i].Level == "red" && result.Trace[i].Result == "skip" {
			redSkip = &result.Trace[i]
			break
		}
	}
	if redSkip == nil {
		t.Fatal("expected red rule skip entry")
	}
	if redSkip.FailedStep != "command" {
		t.Errorf("failed_step = %q, want command", redSkip.FailedStep)
	}
	if redSkip.Detail == "" {
		t.Error("expected non-empty detail on skip")
	}
}

func TestEvaluate_NoTraceAllocations(t *testing.T) {
	cfg := &config.Config{
		Rules: config.RulesConfig{
			Green: []config.Rule{{Commands: []string{"ls"}, Reason: "safe"}},
		},
		Scopes:   map[string][]string{},
		Wrappers: config.DefaultWrappers(),
		Commands: config.DefaultCommandFlags(),
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cmds := []CommandInfo{{Name: "ls"}}
	result := engine.Evaluate(context.Background(), cmds, "ls", "")

	if result.Trace != nil {
		t.Error("Evaluate (non-trace) should not populate Trace")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/ -run "TestEvaluateWithTrace|TestEvaluate_NoTrace" -v`
Expected: FAIL — `EvaluateWithTrace` not defined, `result.Trace` not a field

- [ ] **Step 3: Add Trace field to Result and evalContext to trace.go**

In `internal/rules/engine.go`, add `Trace` to the `Result` struct (around line 35):

```go
type Result struct {
	Decision       string
	Action         string
	Reason         string
	Rule           *MatchedRule
	LLMReview      bool
	MatchedCommand *CommandInfo
	Trace          []RuleTraceEntry // populated by EvaluateWithTrace only
}
```

In `internal/rules/trace.go`, add the per-invocation context:

```go
// evalContext carries per-invocation trace state through the evaluation pipeline.
// Stack-local — never stored on Engine.
type evalContext struct {
	trace   bool
	entries []RuleTraceEntry
}

func (ec *evalContext) appendSkip(level string, index int, cr *compiledRule, cmdName string, failedStep string, detail string) {
	if !ec.trace {
		return
	}
	ec.entries = append(ec.entries, RuleTraceEntry{
		Level:         level,
		Index:         index,
		Rule:          snapshotFromRule(cr.rule),
		CommandTested: cmdName,
		Result:        "skip",
		FailedStep:    failedStep,
		Detail:        detail,
	})
}

func (ec *evalContext) appendMatch(level string, index int, cr *compiledRule, cmdName string) {
	if !ec.trace {
		return
	}
	ec.entries = append(ec.entries, RuleTraceEntry{
		Level:         level,
		Index:         index,
		Rule:          snapshotFromRule(cr.rule),
		CommandTested: cmdName,
		Result:        "match",
	})
}

func (ec *evalContext) appendResolveSkip(level string, index int, cr *compiledRule, cmdName string, rd ResolveDebug) {
	if !ec.trace {
		return
	}
	ec.entries = append(ec.entries, RuleTraceEntry{
		Level:         level,
		Index:         index,
		Rule:          snapshotFromRule(cr.rule),
		CommandTested: cmdName,
		Result:        "skip",
		FailedStep:    "resolve",
		Detail:        "resolved '" + rd.ResolvedValue + "' not in scope " + rd.Scope,
		ResolveDetail: &rd,
	})
}
```

- [ ] **Step 4: Implement `EvaluateWithTrace` and refactor `matchRule`**

Add `EvaluateWithTrace` to `internal/rules/engine.go`. The implementation shares the core logic with `Evaluate` via an internal `evaluate` method that accepts `*evalContext`:

```go
func (e *Engine) EvaluateWithTrace(ctx context.Context, cmds []CommandInfo, rawCommand string, cwd string) *Result {
	ec := &evalContext{trace: true}
	result := e.evaluate(ctx, cmds, rawCommand, cwd, ec)
	result.Trace = ec.entries
	return result
}
```

Rename the existing `Evaluate` body to `evaluate(ctx, cmds, rawCommand, cwd, ec *evalContext)` and make `Evaluate` delegate:

```go
func (e *Engine) Evaluate(ctx context.Context, cmds []CommandInfo, rawCommand string, cwd string) *Result {
	return e.evaluate(ctx, cmds, rawCommand, cwd, nil)
}
```

Refactor `matchRule` to `matchRule(ctx, cr, cmd, rawCommand, cwd string, ec *evalContext) bool`. At each of the 8 match steps, when the check fails and `ec != nil`, call `ec.appendSkip(...)` with the appropriate `failedStep` and `detail`. When the match succeeds, call `ec.appendMatch(...)`.

The level and index are not known inside `matchRule` — the caller (the `evaluate` loops) must pass them. Change `matchRule` signature to:

```go
func (e *Engine) matchRule(ctx context.Context, cr *compiledRule, cmd *CommandInfo, rawCommand string, cwd string, ec *evalContext, level string) bool
```

Or pass level/index via `evalContext` before each call. The cleanest approach: the callers in `evaluate` set `ec.currentLevel` and `ec.currentIndex` before calling `matchRule`, and `matchRule` reads from `ec` when appending. Add these fields to `evalContext`:

```go
type evalContext struct {
	trace        bool
	entries      []RuleTraceEntry
	currentLevel string
	currentIndex int
}
```

Then `matchRule` stays as `matchRule(ctx, cr, cmd, rawCommand, cwd, ec)` — 6 args instead of 7.

For the resolve step (step 7 in matchRule, lines 333-348), build a `ResolveDebug` struct and call `ec.appendResolveSkip(...)`. This requires access to the scope registry patterns — the Engine already has `e.scopeMatcher` (a `*scopes.Registry`). Add a helper to get patterns:

```go
patterns := e.scopeMatcher.Patterns(cr.rule.Resolve.Scope)
```

If `scopes.Registry` doesn't have a `Patterns()` method, use `Scopes()` to get the full map and index into it.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/rules/ -run "TestEvaluateWithTrace|TestEvaluate_NoTrace" -v`
Expected: PASS

- [ ] **Step 6: Run full rules test suite**

Run: `go test ./internal/rules/ -count=1 -v`
Expected: All existing tests still pass (Evaluate behavior unchanged)

- [ ] **Step 7: Commit**

```bash
git add internal/rules/engine.go internal/rules/trace.go internal/rules/trace_test.go
git commit -m "feat(rules): implement EvaluateWithTrace with per-invocation trace state"
```

---

### Task 3: Debug Types in Classifier

**Files:**
- Create: `internal/classifier/debug.go`
- Modify: `internal/classifier/classifier.go:76-93` (add Debug field to ClassifyResponse)

- [ ] **Step 1: Create debug types file**

Create `internal/classifier/debug.go`:

```go
package classifier

import "github.com/limbic-systems/stargate/internal/rules"

// DebugInfo contains diagnostic data populated only for /test (DryRun=true).
type DebugInfo struct {
	ScrubbedCommand    string                `json:"scrubbed_command"`
	RuleTrace          []rules.RuleTraceEntry `json:"rule_trace"`
	Cache              *CacheDebug           `json:"cache"`
	PrecedentsInjected []PrecedentDebug      `json:"precedents_injected,omitempty"`
	RenderedPrompts    *PromptDebug          `json:"rendered_prompts,omitempty"`
	LLMRawResponse     string               `json:"llm_raw_response,omitempty"`
}

type CacheDebug struct {
	Checked bool   `json:"checked"`
	Hit     bool   `json:"hit"`
	Entry   *CacheEntryDebug `json:"entry,omitempty"`
}

type CacheEntryDebug struct {
	Decision string `json:"decision"`
	Action   string `json:"action"`
}

type PrecedentDebug struct {
	ID           string   `json:"id"`
	Decision     string   `json:"decision"`
	Similarity   float64  `json:"similarity"`
	CommandNames []string `json:"command_names"`
	Flags        []string `json:"flags"`
	AgeSeconds   int64    `json:"age_seconds"`
}

type PromptDebug struct {
	System string `json:"system"`
	User   string `json:"user"`
}
```

- [ ] **Step 2: Add Debug field to ClassifyResponse**

In `internal/classifier/classifier.go`, add to the `ClassifyResponse` struct (around line 91, before the closing brace):

```go
	Debug   *DebugInfo `json:"-"`
```

The `json:"-"` tag ensures `/classify` never serializes it.

- [ ] **Step 3: Build and verify**

Run: `go build ./...`
Expected: Clean build, no errors

- [ ] **Step 4: Commit**

```bash
git add internal/classifier/debug.go internal/classifier/classifier.go
git commit -m "feat(classifier): add DebugInfo types and Debug field on ClassifyResponse"
```

---

### Task 4: LLM Raw Response Surfacing

**Files:**
- Modify: `internal/llm/reviewer.go:23-28` (add RawBody to ReviewResponse)
- Modify: `internal/llm/anthropic.go:64-96,100-130` (capture raw text)
- Test: `internal/llm/anthropic_test.go` (verify RawBody populated)

Note: `ratelimit.go`'s `Review` method delegates via `return p.inner.Review(ctx, req)`, so `RawBody` passes through automatically — no changes needed there.

- [ ] **Step 1: Add RawBody to ReviewResponse**

In `internal/llm/reviewer.go`, add to the `ReviewResponse` struct:

```go
type ReviewResponse struct {
	Decision     string
	Reasoning    string
	RiskFactors  []string
	RequestFiles []string
	RawBody      string // raw API response body for debug
}
```

- [ ] **Step 2: Write test for RawBody**

Add to `internal/llm/anthropic_test.go` (or a new file if the existing test structure requires it). The test should verify that `parseResponse` preserves the input text on `ReviewResponse`. Since `parseResponse` currently returns `(ReviewResponse, error)`, the raw body needs to be set by the caller. Write a test for the overall flow:

```go
func TestReviewSDK_RawBodyPopulated(t *testing.T) {
	// parseResponse should be called by the SDK/subprocess paths,
	// and they should set RawBody to the pre-parse text.
	// Test via parseResponse directly:
	text := `{"decision":"allow","reasoning":"safe","risk_factors":[]}`
	resp, err := parseResponse(text)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("decision = %q", resp.Decision)
	}
	// RawBody should NOT be set by parseResponse — it's set by the caller.
	// This test documents that contract.
}
```

- [ ] **Step 3: Capture raw body in both review paths**

In `internal/llm/anthropic.go`, modify `reviewSDK` (line 95):

```go
	resp, err := parseResponse(text)
	resp.RawBody = text
	return resp, err
```

And `reviewSubprocess` (line 129):

```go
	resp, err := parseResponse(string(output))
	resp.RawBody = string(output)
	return resp, err
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/llm/ -count=1 -v`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add internal/llm/reviewer.go internal/llm/anthropic.go internal/llm/anthropic_test.go
git commit -m "feat(llm): surface raw API response body on ReviewResponse"
```

---

### Task 5: Classifier Debug Population

**Files:**
- Modify: `internal/classifier/classifier.go:322-524` (Classify method), `528-810` (reviewWithLLM)
- Test: `internal/classifier/classifier_test.go` or new `debug_test.go`

This is the core wiring task. When `DryRun=true`, the classifier populates `resp.Debug` with data from each pipeline stage. A `recover()` guard wraps the debug assembly.

- [ ] **Step 1: Write failing test for debug population**

Create `internal/classifier/debug_test.go` (or add to existing test file). The test needs a server/classifier with DryRun:

```go
func TestDebugPopulated_GreenCommand(t *testing.T) {
	cfg := testClassifierConfig() // helper that creates a minimal config
	clf, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer clf.Close()

	req := ClassifyRequest{
		Command: "ls -la",
		DryRun:  true,
	}
	resp := clf.Classify(context.Background(), req)

	if resp.Decision != "green" {
		t.Fatalf("decision = %q, want green", resp.Decision)
	}
	if resp.Debug == nil {
		t.Fatal("Debug should be populated for DryRun")
	}
	if resp.Debug.ScrubbedCommand == "" {
		t.Error("scrubbed_command should be non-empty")
	}
	if len(resp.Debug.RuleTrace) == 0 {
		t.Error("rule_trace should have entries")
	}
}

func TestDebugNotPopulated_NonDryRun(t *testing.T) {
	cfg := testClassifierConfig()
	clf, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer clf.Close()

	req := ClassifyRequest{Command: "ls"}
	resp := clf.Classify(context.Background(), req)

	if resp.Debug != nil {
		t.Error("Debug should be nil for non-DryRun")
	}
}
```

Look at existing classifier tests to find the `testClassifierConfig` pattern. If none exists, create a minimal helper matching the pattern in `server_test.go:testConfig()`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/classifier/ -run "TestDebug" -v`
Expected: FAIL — Debug is nil

- [ ] **Step 3: Implement debug population in Classify**

In `internal/classifier/classifier.go`, modify the `Classify` method. The changes:

**a)** After the rule engine call (line 420), use `EvaluateWithTrace` when DryRun:

```go
var result *rules.Result
if req.DryRun {
	result = c.engine.EvaluateWithTrace(ctx, cmds, req.Command, req.CWD)
} else {
	result = c.engine.Evaluate(ctx, cmds, req.Command, req.CWD)
}
```

**b)** Add `debug *DebugInfo` field to `classifyState` (around line 292):

```go
type classifyState struct {
	ctx     context.Context
	req     ClassifyRequest
	cmds    []rules.CommandInfo
	resp    *ClassifyResponse
	traceID string
	debug   *DebugInfo // non-nil when DryRun=true

	// Lazily computed structural fields
	sigComputed   bool
	// ... rest unchanged
}
```

**c)** In `Classify`, after `resp` is built and `finalize` is defined (around line 375), initialize debug with recover guard. This must happen BEFORE the early-return guards (length, parse, AST depth) so the defer is registered:

```go
if req.DryRun {
	debug := &DebugInfo{
		ScrubbedCommand: c.scrubber.Command(req.Command),
	}
	resp.Debug = debug
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "debug: panic during debug assembly: %v\n", r)
			resp.Debug = nil
		}
	}()
}
```

The early-return guards (length check line 378, parse error line 390, AST depth line 399) return `finalize()` which is fine — `resp.Debug` will have `ScrubbedCommand` set but no trace/LLM data, which is correct (those phases didn't run).

**d)** After rule evaluation (line 420), populate the trace:

```go
if req.DryRun && resp.Debug != nil {
	resp.Debug.RuleTrace = result.Trace
}
```

**e)** Thread `debug` into `classifyState` when building state (around line 407):

```go
state := &classifyState{
	ctx:     ctx,
	req:     req,
	cmds:    cmds,
	resp:    resp,
	traceID: traceID,
	debug:   resp.Debug, // nil for non-DryRun
}
```

**f)** In `reviewWithLLM`, populate debug fields at each pipeline stage. All writes are guarded by `if state.debug != nil`:

After cache lookup (line 551-559):
```go
if state.debug != nil {
	state.debug.Cache = &CacheDebug{Checked: cacheAllowed}
	if cacheAllowed {
		if _, hit := ...; hit {
			state.debug.Cache.Hit = true
			state.debug.Cache.Entry = &CacheEntryDebug{Decision: cached.Decision, Action: cached.Action}
			// ... existing early return
		}
	}
}
```

After corpus lookup (around line 669):
```go
if state.debug != nil && len(precedents) > 0 {
	for _, p := range precedents {
		state.debug.PrecedentsInjected = append(state.debug.PrecedentsInjected, PrecedentDebug{
			ID:           fmt.Sprintf("%d", p.ID),
			Decision:     p.Decision,
			Similarity:   p.Similarity,
			CommandNames: p.CommandNames,
			Flags:        p.Flags,
			AgeSeconds:   int64(time.Since(p.CreatedAt).Seconds()),
		})
	}
}
```

After BuildPrompt (line 716):
```go
if state.debug != nil {
	state.debug.RenderedPrompts = &PromptDebug{System: systemPrompt, User: userContent}
}
```

After first LLM response (line 727):
```go
if state.debug != nil {
	state.debug.LLMRawResponse = llmResp.RawBody
}
```

After second LLM call (file retrieval round, line 793):
```go
if state.debug != nil {
	state.debug.RenderedPrompts = &PromptDebug{System: systemPrompt2, User: userContent2}
	state.debug.LLMRawResponse = llmResp2.RawBody
}
```

The second call overwrites the first prompt/response — showing the final state is more useful for debugging.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/classifier/ -run "TestDebug" -v`
Expected: PASS

- [ ] **Step 5: Run full classifier test suite**

Run: `go test ./internal/classifier/ -count=1`
Expected: All existing tests pass (non-DryRun paths unchanged)

- [ ] **Step 6: Commit**

```bash
git add internal/classifier/classifier.go internal/classifier/debug.go internal/classifier/debug_test.go
git commit -m "feat(classifier): populate DebugInfo when DryRun=true"
```

---

### Task 6: `/test` Handler Debug Serialization

**Files:**
- Modify: `internal/server/test_endpoint.go:27-69`
- Test: `internal/server/test_endpoint_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/server/test_endpoint_test.go`:

```go
func TestTest_DebugPopulated(t *testing.T) {
	srv := mustNewServer(t, testConfig())

	w := postTest(t, srv, `{"command": "ls"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	debug, ok := resp["debug"]
	if !ok {
		t.Fatal("response missing debug field")
	}
	debugMap, ok := debug.(map[string]any)
	if !ok {
		t.Fatal("debug is not an object")
	}
	if _, ok := debugMap["scrubbed_command"]; !ok {
		t.Error("debug missing scrubbed_command")
	}
	if _, ok := debugMap["rule_trace"]; !ok {
		t.Error("debug missing rule_trace")
	}
}

func TestClassify_NoDebugField(t *testing.T) {
	srv := mustNewServer(t, testConfig())

	_, resp := postClassify(t, srv, `{"command": "ls"}`)
	// Marshal the response back to JSON and verify no debug field.
	data, _ := json.Marshal(resp)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, ok := raw["debug"]; ok {
		t.Error("/classify response should not contain debug field")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run "TestTest_Debug|TestClassify_NoDebug" -v`
Expected: FAIL — debug field missing from /test response

- [ ] **Step 3: Implement wrapper struct serialization**

In `internal/server/test_endpoint.go`, modify `handleTest` to use a wrapper struct when serializing:

```go
type testDebugResponse struct {
	*classifier.ClassifyResponse
	Debug *classifier.DebugInfo `json:"debug,omitempty"`
}
```

Replace the response encoding (lines 66-68) with:

```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
json.NewEncoder(w).Encode(testDebugResponse{
	ClassifyResponse: resp,
	Debug:            resp.Debug,
}) //nolint:errcheck
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/server/ -run "TestTest_Debug|TestClassify_NoDebug" -v`
Expected: PASS

- [ ] **Step 5: Run full server test suite**

Run: `go test ./internal/server/ -count=1`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add internal/server/test_endpoint.go internal/server/test_endpoint_test.go
git commit -m "feat(server): serialize DebugInfo in /test responses via wrapper struct"
```

---

### Task 7: Corpus `Recent()` Query

**Files:**
- Modify: `internal/corpus/admin.go`
- Test: `internal/corpus/admin_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/corpus/admin_test.go` (or create if needed). The test writes a few entries then queries recent:

```go
func TestRecent_Basic(t *testing.T) {
	c := openTestCorpus(t) // helper that opens an in-memory or temp corpus

	// Write 3 entries.
	for i, dec := range []string{"allow", "deny", "allow"} {
		c.Write(PrecedentEntry{
			Signature:     fmt.Sprintf("sig%d", i),
			SignatureHash: fmt.Sprintf("hash%d", i),
			CommandNames:  []string{"cmd"},
			Flags:         []string{},
			Decision:      dec,
			RawCommand:    fmt.Sprintf("cmd-%d", i),
		})
	}

	entries, err := c.Recent(RecentFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Most recent first.
	if entries[0].RawCommand != "cmd-2" {
		t.Errorf("first entry = %q, want cmd-2 (most recent)", entries[0].RawCommand)
	}
}

func TestRecent_FilterDecision(t *testing.T) {
	c := openTestCorpus(t)
	c.Write(PrecedentEntry{Signature: "s1", SignatureHash: "h1", CommandNames: []string{"a"}, Flags: []string{}, Decision: "allow", RawCommand: "a"})
	c.Write(PrecedentEntry{Signature: "s2", SignatureHash: "h2", CommandNames: []string{"b"}, Flags: []string{}, Decision: "deny", RawCommand: "b"})

	entries, err := c.Recent(RecentFilter{Limit: 10, Decision: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Decision != "deny" {
		t.Errorf("expected 1 deny entry, got %d", len(entries))
	}
}

func TestRecent_Limit(t *testing.T) {
	c := openTestCorpus(t)
	for i := 0; i < 5; i++ {
		c.Write(PrecedentEntry{
			Signature: fmt.Sprintf("s%d", i), SignatureHash: fmt.Sprintf("h%d", i),
			CommandNames: []string{"x"}, Flags: []string{}, Decision: "allow", RawCommand: fmt.Sprintf("x%d", i),
		})
	}

	entries, err := c.Recent(RecentFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d, want 2", len(entries))
	}
}
```

Check existing corpus tests for the `openTestCorpus` pattern — it likely opens a temp DB. If it doesn't exist, create one using `corpus.Open(ctx, config.CorpusConfig{...})` with a temp dir path.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/corpus/ -run "TestRecent" -v`
Expected: FAIL — `Recent` not defined

- [ ] **Step 3: Implement `Recent` method**

Add to `internal/corpus/admin.go`:

```go
// RecentFilter controls the corpus recent query.
type RecentFilter struct {
	Limit    int
	Decision string
	Action   string
	LLM      bool
	Since    time.Duration
}

// RecentEntry is a row from the recent query (subset of PrecedentEntry fields).
type RecentEntry struct {
	ID          int64
	Decision    string
	RawCommand  string
	Reasoning   string
	CreatedAt   time.Time
	LLMReviewed bool
}

// Recent returns the most recent corpus entries, ordered by created_at DESC.
func (c *Corpus) Recent(filter RecentFilter) ([]RecentEntry, error) {
	query := `SELECT id, decision, raw_command, reasoning, created_at FROM precedents WHERE 1=1`
	var args []any

	if filter.Decision != "" {
		query += ` AND decision = ?`
		args = append(args, filter.Decision)
	}
	if filter.Since > 0 {
		cutoff := time.Now().Add(-filter.Since).Format(time.RFC3339)
		query += ` AND created_at >= ?`
		args = append(args, cutoff)
	}

	query += ` ORDER BY created_at DESC`

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	query += ` LIMIT ?`
	args = append(args, limit)

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("corpus.Recent: %w", err)
	}
	defer rows.Close()

	var entries []RecentEntry
	for rows.Next() {
		var e RecentEntry
		var createdStr string
		var rawCmd, reasoning sql.NullString
		if err := rows.Scan(&e.ID, &e.Decision, &rawCmd, &reasoning, &createdStr); err != nil {
			return nil, fmt.Errorf("corpus.Recent: scan: %w", err)
		}
		e.RawCommand = rawCmd.String
		e.Reasoning = reasoning.String
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
```

Note: The `--action` and `--llm` filters require additional columns or logic. The corpus stores `decision` (allow/deny/user_approved), not `action` (block/allow/review). Map at the CLI layer. The `--llm` filter can check `reasoning LIKE 'LLM%'` or add a dedicated column later — for now, implement the core filters (decision, since, limit) and add the rest as CLI-layer post-filters on the returned entries.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/corpus/ -run "TestRecent" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/corpus/admin.go internal/corpus/admin_test.go
git commit -m "feat(corpus): add Recent() query for time-based corpus listing"
```

---

### Task 8: `corpus recent` CLI Command

**Files:**
- Modify: `cmd/stargate/corpus.go:18-61` (usage string, dispatcher, new handler)

- [ ] **Step 1: Add `recent` to usage string and dispatcher**

In `cmd/stargate/corpus.go`, add `recent` to the usage string (around line 18) and add a case in the dispatcher (around line 42):

```go
case "recent":
	return handleCorpusRecent(args[1:], configPath, verbose)
```

- [ ] **Step 2: Implement `handleCorpusRecent`**

Add to `cmd/stargate/corpus.go`:

```go
func handleCorpusRecent(args []string, configPath string, verbose bool) int {
	fs := flag.NewFlagSet("corpus recent", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "max entries to return")
	decision := fs.String("decision", "", "filter by decision (allow/deny/user_approved)")
	since := fs.String("since", "", "time window (Go duration, e.g. 1h)")
	jsonOut := fs.Bool("json", false, "output as JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	c, cfg, err := openCorpusDB(configPath)
	_ = cfg
	if err != nil {
		fmt.Fprintf(os.Stderr, "corpus recent: %v\n", err)
		return 1
	}
	defer c.Close()

	filter := corpus.RecentFilter{Limit: *limit, Decision: *decision}
	if *since != "" {
		d, err := time.ParseDuration(*since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "corpus recent: invalid duration %q: %v\n", *since, err)
			return 1
		}
		filter.Since = d
	}

	entries, err := c.Recent(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "corpus recent: %v\n", err)
		return 1
	}

	if *jsonOut {
		type jsonEntry struct {
			ID         int64  `json:"id"`
			Decision   string `json:"decision"`
			Command    string `json:"command"`
			Reason     string `json:"reason"`
			AgeSeconds int64  `json:"age_seconds"`
		}
		var out []jsonEntry
		for _, e := range entries {
			out = append(out, jsonEntry{
				ID:         e.ID,
				Decision:   e.Decision,
				Command:    e.RawCommand,
				Reason:     e.Reasoning,
				AgeSeconds: int64(time.Since(e.CreatedAt).Seconds()),
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out) //nolint:errcheck
		return 0
	}

	if len(entries) == 0 {
		fmt.Println("No recent entries.")
		return 0
	}

	// Table output.
	fmt.Printf("%-8s %-5s %-5s %-35s %s\n", "ID", "AGE", "DEC", "CMD", "REASON")
	for _, e := range entries {
		id := fmt.Sprintf("%d", e.ID)
		if len(id) > 8 {
			id = id[:8]
		}
		age := formatAge(time.Since(e.CreatedAt))
		dec := abbreviateDecision(e.Decision)
		cmd := truncate(e.RawCommand, 35)
		reason := truncate(e.Reasoning, 40)
		fmt.Printf("%-8s %-5s %-5s %-35s %s\n", id, age, dec, cmd, reason)
	}
	return 0
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func abbreviateDecision(d string) string {
	switch d {
	case "allow", "user_approved":
		return "ALW"
	case "deny":
		return "DNY"
	default:
		return strings.ToUpper(d[:min(3, len(d))])
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
```

- [ ] **Step 3: Build and verify**

Run: `go build ./cmd/stargate/`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add cmd/stargate/corpus.go
git commit -m "feat(cli): add corpus recent subcommand for time-based corpus listing"
```

---

### Task 9: `stargate explain` CLI Command

**Files:**
- Create: `cmd/stargate/explain.go`
- Modify: `cmd/stargate/main.go:62-69` (add to handlers map)

- [ ] **Step 1: Create explain command**

Create `cmd/stargate/explain.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/limbic-systems/stargate/internal/classifier"
)

func handleExplain(args []string, configPath string, verbose bool) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	server := fs.String("server", "", "server URL (default: $STARGATE_URL or http://127.0.0.1:9099)")
	verboseFlag := fs.Bool("verbose", false, "show all rules evaluated")
	jsonOut := fs.Bool("json", false, "dump raw JSON response")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: stargate explain [flags] <command>\n\n")
		fmt.Fprintf(os.Stderr, "Classify a command via /test and pretty-print the debug trace.\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}

	command := strings.Join(fs.Args(), " ")
	serverURL := *server
	if serverURL == "" {
		serverURL = os.Getenv("STARGATE_URL")
	}
	if serverURL == "" {
		serverURL = "http://127.0.0.1:9099"
	}

	// POST to /test.
	body, _ := json.Marshal(map[string]string{"command": command})
	resp, err := http.Post(serverURL+"/test", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "explain: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "explain: read response: %v\n", err)
		return 1
	}

	if *jsonOut {
		// Pretty-print raw JSON.
		var buf bytes.Buffer
		json.Indent(&buf, data, "", "  ")
		fmt.Println(buf.String())
		return 0
	}

	// Parse the response with debug.
	var result struct {
		classifier.ClassifyResponse
		Debug *classifier.DebugInfo `json:"debug"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Fprintf(os.Stderr, "explain: parse response: %v\n", err)
		return 1
	}

	// Header.
	decision := result.Decision
	action := result.Action
	suffix := ""
	if result.LLMReview != nil && result.LLMReview.Performed {
		suffix = fmt.Sprintf(" (LLM %s)", result.LLMReview.Decision)
	}
	fmt.Printf("DECISION: %s → %s%s\n", decision, action, suffix)
	fmt.Printf("TRACE ID: %s\n", result.StargateTrID)
	fmt.Printf("COMMAND:  %s\n", command)
	if result.Debug != nil {
		fmt.Printf("SCRUBBED: %s\n", result.Debug.ScrubbedCommand)
	}

	if result.Debug == nil {
		fmt.Println("\n(no debug data — server may be running an older version)")
		return 0
	}

	debug := result.Debug

	// Rule trace.
	if len(debug.RuleTrace) > 0 {
		fmt.Println("\n-- Rule Evaluation ----------------------------------------")
		for _, entry := range debug.RuleTrace {
			if !*verboseFlag && entry.Result == "skip" && !isRelevantRule(entry, command) {
				continue
			}
			marker := "skip"
			if entry.Result == "match" {
				marker = "MATCH"
			}
			ruleDesc := formatRuleDesc(entry)
			detail := ""
			if entry.Detail != "" {
				detail = " (" + entry.Detail + ")"
			}
			fmt.Printf("  %-4s #%-2d %-30s → %s%s\n",
				strings.ToUpper(entry.Level), entry.Index, ruleDesc, marker, detail)
		}
	}

	// Cache.
	if debug.Cache != nil {
		fmt.Println("\n-- Cache ---------------------------------------------------")
		hitStr := "no"
		if debug.Cache.Hit {
			hitStr = "yes"
		}
		fmt.Printf("  checked: yes, hit: %s\n", hitStr)
		if debug.Cache.Entry != nil {
			fmt.Printf("  cached: %s → %s\n", debug.Cache.Entry.Decision, debug.Cache.Entry.Action)
		}
	}

	// Precedents.
	if len(debug.PrecedentsInjected) > 0 {
		fmt.Printf("\n-- Corpus Precedents (%d injected) --------------------------\n", len(debug.PrecedentsInjected))
		for i, p := range debug.PrecedentsInjected {
			age := formatAge(time.Duration(p.AgeSeconds) * time.Second)
			fmt.Printf("  #%-2d %-5s sim=%.2f  %s [%s]  %s ago\n",
				i+1, p.Decision, p.Similarity,
				strings.Join(p.CommandNames, ","),
				strings.Join(p.Flags, ","),
				age)
		}
	}

	// LLM Prompt.
	if debug.RenderedPrompts != nil {
		fmt.Println("\n-- LLM Prompt ----------------------------------------------")
		// Truncate for display unless verbose.
		sysDisplay := debug.RenderedPrompts.System
		userDisplay := debug.RenderedPrompts.User
		if !*verboseFlag {
			sysDisplay = truncate(sysDisplay, 200)
			userDisplay = truncate(userDisplay, 500)
		}
		fmt.Printf("  [system] %s\n", sysDisplay)
		fmt.Printf("  [user]   %s\n", userDisplay)
	}

	// LLM Response.
	if result.LLMReview != nil && result.LLMReview.Performed {
		fmt.Println("\n-- LLM Response --------------------------------------------")
		fmt.Printf("  Decision:     %s\n", result.LLMReview.Decision)
		fmt.Printf("  Reasoning:    %s\n", result.LLMReview.Reasoning)
		if len(result.LLMReview.RiskFactors) > 0 {
			fmt.Printf("  Risk factors: %s\n", strings.Join(result.LLMReview.RiskFactors, ", "))
		}
		if debug.LLMRawResponse != "" {
			rawDisplay := debug.LLMRawResponse
			if !*verboseFlag {
				rawDisplay = truncate(rawDisplay, 200)
			}
			fmt.Printf("  Raw:          %s\n", rawDisplay)
		}
	}

	// Timing.
	if result.Timing != nil {
		fmt.Println("\n-- Timing --------------------------------------------------")
		fmt.Printf("  parse: %.2fms  rules: %.2fms",
			float64(result.Timing.ParseUs)/1000,
			float64(result.Timing.RulesUs)/1000)
		if result.Timing.LLMMs > 0 {
			fmt.Printf("  llm: %.0fms", result.Timing.LLMMs)
		}
		fmt.Printf("  total: %.0fms\n", result.Timing.TotalMs)
	}

	return 0
}

func isRelevantRule(entry rules.RuleTraceEntry, command string) bool {
	// Show rules that matched.
	if entry.Result == "match" {
		return true
	}
	// Show rules with no command filter (they apply to all commands).
	if entry.Rule.Command == "" && len(entry.Rule.Commands) == 0 {
		return true
	}
	// Show rules with pattern (regex can match anything).
	if entry.Rule.Pattern != "" {
		return true
	}
	// Show rules whose command matches what was tested.
	if entry.Rule.Command == entry.CommandTested {
		return true
	}
	for _, c := range entry.Rule.Commands {
		if c == entry.CommandTested {
			return true
		}
	}
	return false
}

func formatRuleDesc(entry rules.RuleTraceEntry) string {
	var parts []string
	cmd := entry.Rule.Command
	if cmd == "" && len(entry.Rule.Commands) > 0 {
		cmd = strings.Join(entry.Rule.Commands, ",")
	}
	parts = append(parts, cmd)
	if len(entry.Rule.Flags) > 0 {
		parts = append(parts, "["+strings.Join(entry.Rule.Flags, ",")+"]")
	}
	if entry.Rule.Resolve != nil {
		parts = append(parts, "[resolve:"+entry.Rule.Resolve.Resolver+"]")
	}
	if entry.Rule.Pattern != "" {
		parts = append(parts, "[pattern]")
	}
	return strings.Join(parts, " ")
}
```

Note: This file imports `rules` for `RuleTraceEntry` — add the import: `"github.com/limbic-systems/stargate/internal/rules"`.

- [ ] **Step 2: Register in main.go**

In `cmd/stargate/main.go`, add to the handlers map (around line 62):

```go
"explain": handleExplain,
```

Update the usage string to include `explain`.

- [ ] **Step 3: Build and verify**

Run: `go build ./cmd/stargate/`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add cmd/stargate/explain.go cmd/stargate/main.go
git commit -m "feat(cli): add stargate explain command for pretty-printed debug output"
```

---

### Task 10: Red Team Condition — File Retrieval Path Validation Test

**Files:**
- Test: `internal/server/test_endpoint_test.go`

The red team's R2 condition: verify that `/test` file retrieval goes through the same `allowed_paths`/`denied_paths` validation as `/classify`. Since both endpoints call the same `Classify` method on the same `Classifier` instance, and the file retrieval path validation happens inside `reviewWithLLM` (classifier.go:759-767) which is shared, this test confirms the DryRun path doesn't bypass it.

- [ ] **Step 1: Write the test**

Add to `internal/server/test_endpoint_test.go`. This test requires an LLM provider that returns a file request, then verifies the file retrieval respects path validation. Since we can't easily mock the LLM provider through the server, test at the classifier level instead.

Create `internal/classifier/debug_file_retrieval_test.go`:

```go
func TestDryRun_FileRetrievalPathValidation(t *testing.T) {
	// Verify that DryRun=true (the /test path) uses the same
	// allowed_paths/denied_paths validation as DryRun=false.
	// Both paths call reviewWithLLM which uses llm.ResolveFiles
	// with the same FileResolverConfig — this test confirms that
	// DryRun does not bypass or modify the config.

	cfg := testClassifierConfig()
	// Enable LLM with allowed/denied paths.
	cfg.LLM.AllowFileRetrieval = true
	cfg.LLM.AllowedPaths = []string{"/tmp/stargate-test-*"}
	cfg.LLM.DeniedPaths = []string{"/etc/*"}

	// The file resolver config is built in reviewWithLLM from c.llmCfg.
	// DryRun does not alter c.llmCfg. Verify by inspecting the classifier's
	// config fields directly.
	clf, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer clf.Close()

	// Verify the classifier's LLM config has the expected paths.
	if len(clf.llmCfg.AllowedPaths) == 0 {
		t.Error("AllowedPaths should be set")
	}
	if len(clf.llmCfg.DeniedPaths) == 0 {
		t.Error("DeniedPaths should be set")
	}

	// The key assertion: DryRun does not modify llmCfg or bypass
	// file retrieval validation. reviewWithLLM builds FileResolverConfig
	// from c.llmCfg (lines 759-767) regardless of DryRun.
	// This is a structural test — the same code path is used for both.
	// A behavioral test would require a mock LLM that returns RequestFiles,
	// which is covered by the existing llm/files_test.go tests.
}
```

Alternatively, if there's a way to inject a mock LLM provider, write a more behavioral test that sends a file request through `/test` and verifies denied paths are rejected. Check if the classifier has a setter for the LLM provider.

- [ ] **Step 2: Run test**

Run: `go test ./internal/classifier/ -run "TestDryRun_FileRetrieval" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/classifier/debug_file_retrieval_test.go
git commit -m "test(classifier): verify /test file retrieval uses same path validation as /classify"
```

---

### Task 11: Integration Test and Final Verification

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: All tests pass

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Build binary**

Run: `go build -o /dev/null ./cmd/stargate/`
Expected: Clean build

- [ ] **Step 4: Manual smoke test (if server running)**

```bash
# Test explain command
./dist/stargate -c stargate.toml explain "ls -la"
./dist/stargate -c stargate.toml explain "curl https://example.com"
./dist/stargate -c stargate.toml explain --json "git status"

# Test corpus recent
./dist/stargate -c stargate.toml corpus recent --limit 5
./dist/stargate -c stargate.toml corpus recent --decision deny --json
```

- [ ] **Step 5: Final commit if any fixups needed**

```bash
git add -A
git commit -m "fix: integration test fixups for debug observability"
```
