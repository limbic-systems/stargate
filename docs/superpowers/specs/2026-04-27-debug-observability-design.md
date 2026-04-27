# Debug & Observability Design

**Date:** 2026-04-27
**Status:** Approved (panel reviewed 2026-04-27)
**Scope:** `/test` debug enrichment, `corpus recent` CLI, `stargate explain` CLI

## Problem

Stargate runs on ephemeral fly.io VMs with Grafana Cloud handling metrics/traces. When a command is misclassified, the debugging workflow requires SSH access to the running instance to investigate why a decision was made and iterate on classification prompt/rule changes. The current tooling has gaps:

1. **No visibility into LLM decision inputs** — `corpus inspect` shows the decision output (reasoning, risk factors) but not what the LLM was asked (rendered prompts, injected precedents, scrubbed command).
2. **No rule evaluation trace** — when a rule doesn't fire as expected, there's no way to see which of the 8 matching steps failed or what resolver values were produced.
3. **No time-based corpus querying** — `corpus search` matches by structural similarity. There's no way to ask "show me recent RED decisions" to find the problematic classification.
4. **No formatted investigation output** — debugging requires parsing raw JSON from `/test` or running multiple CLI commands in sequence.

## Design Constraints

- Ephemeral VM storage — corpus and state are lost on restart, accepted as fine
- Debugging happens via SSH/SCP to running fly.io instances
- Local reproduction by pulling corpus + config from remote, iterating with `/test` locally
- `/classify` must never expose debug data — security boundary
- All debug output must go through the scrubber — no secret leakage
- No new config fields, no corpus schema changes, no new HTTP endpoints

## Design

### 1. `/test` Debug Enrichment

When `DryRun=true`, the classifier populates a `Debug` field on `ClassifyResponse`. This field uses a `json:"-"` tag on the base struct. The `/test` handler serializes it into the response via a wrapper type. `/classify` never sees it.

#### ClassifyResponse Change

```go
// internal/classifier/classifier.go — add to existing ClassifyResponse struct

type ClassifyResponse struct {
    // ... existing fields unchanged ...
    Debug   *DebugInfo `json:"-"` // only populated when DryRun=true; omitted by /classify
}
```

#### DebugInfo Structure

`RuleTraceEntry` and `ResolveDebug` live in the `rules` package (not `classifier`) since they are populated by `matchRule` in `engine.go`. `DebugInfo` and the remaining types live in `classifier`.

```go
// internal/classifier/debug.go

type DebugInfo struct {
    ScrubbedCommand    string              `json:"scrubbed_command"`
    RuleTrace          []RuleTraceEntry    `json:"rule_trace"`
    Cache              *CacheDebug         `json:"cache"`
    PrecedentsInjected []PrecedentDebug    `json:"precedents_injected,omitempty"`
    RenderedPrompts    *PromptDebug        `json:"rendered_prompts,omitempty"`
    LLMRawResponse     string             `json:"llm_raw_response,omitempty"` // raw API response body (not scrubbed — see accepted-risks.md)
}

// internal/rules/trace.go — lives in rules package to avoid circular dependency

type RuleTraceEntry struct {
    Level          string        `json:"level"`           // "red", "green", "yellow"
    Index          int           `json:"index"`           // index within level
    Rule           RuleSnapshot  `json:"rule"`            // copy of rule definition
    CommandTested  string        `json:"command_tested"`  // which CommandInfo.Name
    Result         string        `json:"result"`          // "match" or "skip"
    FailedStep     string        `json:"failed_step,omitempty"` // which of 8 steps failed
    Detail         string        `json:"detail,omitempty"`      // human-readable why
    ResolveDetail  *ResolveDebug `json:"resolve_detail,omitempty"`
}

type RuleSnapshot struct {
    Command     string   `json:"command,omitempty"`
    Commands    []string `json:"commands,omitempty"`
    Subcommands []string `json:"subcommands,omitempty"`
    Flags       []string `json:"flags,omitempty"`
    Args        []string `json:"args,omitempty"`
    Pattern     string   `json:"pattern,omitempty"`
    Scope       string   `json:"scope,omitempty"`
    Context     string   `json:"context,omitempty"`
    Resolve     *struct {
        Resolver string `json:"resolver"`
        Scope    string `json:"scope"`
    } `json:"resolve,omitempty"`
    LLMReview *bool  `json:"llm_review,omitempty"`
    Reason    string `json:"reason"`
}

type ResolveDebug struct {
    Resolver      string   `json:"resolver"`
    ResolvedValue string   `json:"resolved_value,omitempty"`
    Resolved      bool     `json:"resolved"`
    Error         string   `json:"error,omitempty"`
    Scope         string   `json:"scope"`
    ScopePatterns []string `json:"scope_patterns"`
    Matched       bool     `json:"matched"`
}

type CacheDebug struct {
    Checked bool   `json:"checked"`
    Hit     bool   `json:"hit"`
    Entry   *struct {
        Decision string `json:"decision"`
        Action   string `json:"action"`
    } `json:"entry,omitempty"`
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

#### Data Flow

All debug data is already computed internally during classification. The implementation threads these values into `DebugInfo` rather than discarding them:

- **`scrubbed_command`** — produced by `scrub.Scrubber.Command()`, already called for corpus writes and LLM prompts
- **`rule_trace`** — requires `rules.Engine.Evaluate` to return trace entries alongside `Result`
- **`cache`** — cache lookup result already available in `reviewWithLLM`
- **`precedents_injected`** — corpus search results already fetched in `reviewWithLLM`
- **`rendered_prompts`** — prompt assembly happens in `llm.BuildClassifyPrompt`, needs to return the rendered strings
- **`llm_raw_response`** — raw HTTP response body from the Anthropic API (includes usage metadata, stop reason, model version). Not scrubbed — maximizes diagnostic value for debugging LLM issues. Surfaced through the provider interface as an additive return value

#### Serialization

`ClassifyResponse.Debug` has `json:"-"` so standard `json.Marshal` omits it. The `/test` handler uses a wrapper struct to include it:

```go
type testResponse struct {
    *classifier.ClassifyResponse
    Debug *classifier.DebugInfo `json:"debug,omitempty"`
}
```

`/classify` continues to use `json.NewEncoder(w).Encode(resp)` unchanged.

#### Security

- `scrubbed_command` is post-scrubber output — secrets already redacted
- `rendered_prompts` contain the scrubbed command, not the raw command — the LLM never sees unscrubbed input, and neither does debug output
- `llm_raw_response` may contain the LLM echoing parts of the scrubbed command — acceptable since the command was already scrubbed before being sent
- Resolver values (e.g., resolved domain names, repo owners) are not secrets — they come from command arguments and `.git/config`
- Scope patterns are from `stargate.toml` — operator-defined, not sensitive
- `/test` is only accessible on localhost (server binds 127.0.0.1) — debug data doesn't leave the machine unless explicitly exported via SSH

### 2. Rule Engine Trace

`rules.Engine.Evaluate` gains a trace mode controlled by a parameter.

#### Interface Change

The existing signature is unchanged. A new method handles trace mode:

```go
// Existing — unchanged, no performance impact on /classify
func (e *Engine) Evaluate(ctx context.Context, cmds []CommandInfo, rawCommand string, cwd string) *Result

// New — returns Result with Trace populated
func (e *Engine) EvaluateWithTrace(ctx context.Context, cmds []CommandInfo, rawCommand string, cwd string) *Result
```

Both methods share the same internal evaluation logic. `EvaluateWithTrace` passes a per-invocation `evalContext` (stack-local, not stored on `Engine`) through the call chain to `matchRule`. This context carries the trace slice. The `Engine` struct is never mutated — safe for concurrent `/classify` and `/test` requests. The existing `Evaluate` method is untouched — no new parameter, no caller changes needed.

The classifier calls `Evaluate` for `/classify` and `EvaluateWithTrace` for `/test` (when `DryRun=true`).

#### Trace Entry Content

Each entry records:
- Which rule (level + index) was tested against which command
- Whether it matched or was skipped
- If skipped, which of the 8 matching steps failed first: `command`, `subcommands`, `flags`, `args`, `scope`, `context`, `resolve`, `pattern`
- A human-readable `detail` string explaining the failure (e.g., `"command 'curl' != 'rm'"`, `"flags [-s] missing required [-rf]"`)
- For resolver steps: the full `ResolveDebug` with resolved value, scope patterns, and match result

The 8 matching steps in `matchRule` (engine.go:274-358) already evaluate in order and short-circuit. The trace captures the first failing step.

#### Performance

Trace mode allocates a `[]RuleTraceEntry` slice only when `trace=true`. For a typical config with 83 rules and 1-3 commands, this is ~250 entries at ~200 bytes each = ~50KB per `/test` request. Negligible for a debugging endpoint.

### 3. `corpus recent` CLI Command

New subcommand for time-based corpus querying.

#### Usage

```
stargate corpus recent [flags]
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--limit N` | 20 | Max entries to return |
| `--decision red\|yellow\|green` | (all) | Filter by classification |
| `--action block\|allow\|review` | (all) | Filter by action |
| `--llm` | false | Only show LLM-reviewed decisions |
| `--since 1h` | (no limit) | Time window (Go duration) |
| `--json` | false | Output as JSON array |

#### Table Output (default)

```
ID       AGE  DEC  ACTION CMD                              REASON
a3f2...  2m   RED  block  rm -rf /tmp/build/*              destructive deletion pattern
b7c1...  5m   YLW  allow  curl -s https://api.example.com  LLM: low-risk API fetch
c9d0...  8m   GRN  allow  git status                       safe read-only command
```

- ID: first 8 hex characters (sufficient for `corpus inspect` lookup)
- AGE: human-friendly relative time (2m, 3h, 1d)
- DEC: 3-char abbreviation (RED/YLW/GRN)
- CMD: truncated to 35 characters
- REASON: truncated to 40 characters

#### JSON Output

```json
[
  {
    "id": "a3f2b7c1...",
    "decision": "red",
    "action": "block",
    "command": "rm -rf /tmp/build/*",
    "reason": "destructive deletion pattern",
    "age_seconds": 120,
    "llm_reviewed": false
  }
]
```

#### Implementation

New method on `corpus.Corpus`:

```go
type RecentFilter struct {
    Limit    int
    Decision string
    Action   string
    LLM      bool
    Since    time.Duration
}

func (c *Corpus) Recent(filter RecentFilter) ([]RecentEntry, error)
```

SQL query: `SELECT ... FROM precedents WHERE <filters> ORDER BY created_at DESC LIMIT ?`. The `created_at` column stores RFC3339 timestamps, which sort lexicographically. The `--since` filter computes the cutoff time in Go (`time.Now().Add(-duration).Format(time.RFC3339)`) and uses `WHERE created_at >= ?` in SQL. No schema changes.

### 4. `stargate explain` CLI Command

Pretty-prints `/test` debug output for SSH investigation sessions.

#### Usage

```
stargate explain "curl -s https://api.example.com" [flags]
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--verbose` | false | Show all rules evaluated (default: only rules relevant to matched command) |
| `--json` | false | Dump raw `/test` JSON response with debug object |
| `--server URL` | `$STARGATE_URL` or `http://127.0.0.1:9099` | Server to query |

#### Output Format

```
DECISION: yellow -> allow (LLM approved)
TRACE ID: sg_tr_a3f2b7c1d9e0...
COMMAND:  curl -s https://api.example.com
SCRUBBED: curl -s https://api.example.com

-- Rule Evaluation ----------------------------------------
  RED  #0  rm [-rf,-fr] [/]           -> skip (command: 'curl' != 'rm')
  RED  #4  curl [resolve:url_domain]  -> skip (resolve: 'api.example.com' not in blocked_domains)
  GRN  #5  curl [resolve:url_domain]  -> skip (resolve: 'api.example.com' not in allowed_domains ["*.internal.co"])
  YLW  #3  curl [llm_review=true]     -> MATCH

-- Cache ---------------------------------------------------
  checked: yes, hit: no

-- Corpus Precedents (3 injected) --------------------------
  #1  allow  sim=0.82  curl [-s]       3h ago
  #2  allow  sim=0.74  curl [-sS]      1d ago
  #3  deny   sim=0.68  curl [-o]       2d ago

-- LLM Prompt ----------------------------------------------
  [system] You are a security reviewer...
  [user]   Classify this command: ...

-- LLM Response --------------------------------------------
  Decision:     allow
  Reasoning:    Low-risk read-only HTTP GET to a public API...
  Risk factors: none identified
  Raw:          { "decision": "allow", ... }

-- Timing --------------------------------------------------
  parse: 0.04ms  rules: 0.12ms  llm: 3241ms  total: 3248ms

Note: `Timing.ParseUs` and `RulesUs` are microseconds, converted to ms for display. `LLMMs` and `TotalMs` are already milliseconds.
```

#### Filtering Logic

Default mode filters the rule trace to rules that were plausible candidates for the matched command. A rule is included if any of:
- Its `command` or `commands` field matches the command name
- It has a `pattern` field (regex rules can match any command)
- It has no `command`/`commands` field (rules with only `subcommands`, `flags`, `args`, `scope`, or `context` apply to all commands)
- Its `result` is `"match"` (the winning rule is always shown)

This reduces noise from ~83 rules to ~5-10 per command while preserving all rules that could plausibly have fired.

`--verbose` disables filtering and shows all rule evaluations.

#### Implementation

Calls `POST /test` with the command, deserializes the response including the debug object, and formats each section. The `/test` endpoint already exists — `explain` is purely a formatting layer.

Sections with null data are omitted (e.g., if the decision is GREEN and no LLM was called, the Cache/Precedents/LLM sections are absent).

## Changes By Package

| Package | File | Change |
|---------|------|--------|
| `internal/classifier` | `debug.go` (new) | `DebugInfo`, `CacheDebug`, `PrecedentDebug`, `PromptDebug` types |
| `internal/classifier` | `classifier.go` | Add `Debug *DebugInfo` field to `ClassifyResponse`; populate when `DryRun=true`; call `EvaluateWithTrace` |
| `internal/rules` | `trace.go` (new) | `RuleTraceEntry`, `RuleSnapshot`, `ResolveDebug` types |
| `internal/rules` | `engine.go` | New `EvaluateWithTrace` method; `matchRule` returns trace entry when tracing |
| `internal/llm` | prompt assembly | Return rendered prompt strings alongside sending to provider |
| `internal/llm` | provider interface | Surface raw response body |
| `internal/corpus` | `admin.go` | New `Recent(filter)` query method |
| `internal/server` | `test_endpoint.go` | Serialize `Debug` via wrapper struct |
| `cmd/stargate` | `corpus.go` | New `corpus recent` subcommand |
| `cmd/stargate` | `explain.go` (new) | `explain` subcommand with pretty-printing |

## What Doesn't Change

- `/classify` endpoint — no debug data, no behavior change, no performance impact
- Corpus SQLite schema — `created_at` column already exists
- Config format — no new fields
- LLM provider interface contract — raw response surfacing is additive
- Existing CLI commands — `corpus search`, `corpus inspect`, `config dump` unchanged

## Security Considerations

### Trust Boundaries

- **`/test` debug data stays on localhost.** The server binds to 127.0.0.1. Debug data is only accessible via SSH to the running instance or by pulling the `/test` response via `stargate explain`.
- **All commands in debug output are scrubbed.** `scrubbed_command` and `rendered_prompts.user` go through `scrub.Scrubber.Command()` before being stored in `DebugInfo`. The LLM never sees unscrubbed input.
- **Scope patterns are operator-defined.** They come from `stargate.toml` (the trust anchor) and are not sensitive. Exposing them in `resolve_detail` is equivalent to `config dump`.
- **Resolver values are derived from command arguments.** `url_domain` extracts from args, `github_repo_owner` reads `.git/config`. These are already visible in `corpus inspect` today.
- **`llm_raw_response` contains scrubbed content.** The LLM receives the scrubbed command; its response may echo parts of it. No unscrubbed secrets appear.

### Fail-Closed Behavior

- Debug enrichment only runs when `DryRun=true`. If debug population panics or errors, it does not affect `/classify` behavior.
- Debug assembly is wrapped in `recover()` — if any debug population step panics (e.g., nil pointer in rule trace building), the classification result is still returned without the `Debug` field. The panic is logged to stderr for operator awareness.
- Rule trace allocation failure (OOM on pathological rule count) is bounded — trace mode is only active for `/test`, which is a debugging tool not on the hot path.

### No New Attack Surface

- No new HTTP endpoints. `/test` already exists; it gains richer response data.
- No new config fields. No new trust boundaries.
- `corpus recent` is a read-only CLI query against existing data.
- `explain` is a CLI formatting wrapper around `/test`.

### Accepted Risks (see `docs/accepted-risks.md`)

The following panel findings were evaluated and accepted with the rationale that localhost access already provides equivalent information via existing CLI tools (`config dump`, `config rules`, `config scopes`, `corpus search/inspect`). Degrading debug output fidelity would harm the primary use case without meaningful security improvement:

- **Unscrubbed `llm_raw_response`** — raw API body included as-is for maximum diagnostic value. File retrieval content may appear. Operator already has full machine access.
- **`RuleSnapshot` in trace entries** — full rule definitions included. `config rules` already exposes the same data.
- **`ScopePatterns` in resolve detail** — glob patterns included. `config scopes` already exposes the same data.
- **`rendered_prompts` in default output** — system + user prompts included without opt-in gate. `config dump` already exposes the system prompt; precedents and scopes are available via existing CLI commands.

**Operational guidance:** `/test` debug output should be treated as security-sensitive material. Do not share outside operational debugging contexts (e.g., do not paste into public issue trackers or chat).
