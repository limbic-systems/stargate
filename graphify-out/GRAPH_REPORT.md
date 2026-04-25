# Graph Report - .  (2026-04-25)

## Corpus Check
- 91 files · ~120,393 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1196 nodes · 3670 edges · 42 communities detected
- Extraction: 67% EXTRACTED · 33% INFERRED · 0% AMBIGUOUS · INFERRED: 1202 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Telemetry & Observability|Telemetry & Observability]]
- [[_COMMUNITY_Command Cache & Deduplication|Command Cache & Deduplication]]
- [[_COMMUNITY_Corpus Storage & SQLite|Corpus Storage & SQLite]]
- [[_COMMUNITY_File Retrieval & Scrubbing|File Retrieval & Scrubbing]]
- [[_COMMUNITY_Classifier Pipeline & LLM Review|Classifier Pipeline & LLM Review]]
- [[_COMMUNITY_Claude Code Adapter & Hooks|Claude Code Adapter & Hooks]]
- [[_COMMUNITY_Shell Parser & AST Walking|Shell Parser & AST Walking]]
- [[_COMMUNITY_Evasion Test Corpus|Evasion Test Corpus]]
- [[_COMMUNITY_Security Framework & Risks|Security Framework & Risks]]
- [[_COMMUNITY_Rule Engine & Matching|Rule Engine & Matching]]
- [[_COMMUNITY_Classify RequestResponse|Classify Request/Response]]
- [[_COMMUNITY_Test Endpoint & CLI|Test Endpoint & CLI]]
- [[_COMMUNITY_LLM Provider & Anthropic SDK|LLM Provider & Anthropic SDK]]
- [[_COMMUNITY_Config Subcommands|Config Subcommands]]
- [[_COMMUNITY_GitHub Scope Resolver|GitHub Scope Resolver]]
- [[_COMMUNITY_AST Walker Internals|AST Walker Internals]]
- [[_COMMUNITY_Feedback Handler|Feedback Handler]]
- [[_COMMUNITY_HTTP Client & Adapter|HTTP Client & Adapter]]
- [[_COMMUNITY_Corpus Tests & Scrubber|Corpus Tests & Scrubber]]
- [[_COMMUNITY_Init Subcommand|Init Subcommand]]
- [[_COMMUNITY_TTLMap Cache|TTLMap Cache]]
- [[_COMMUNITY_Scope Registry|Scope Registry]]
- [[_COMMUNITY_Server HTTP Handlers|Server HTTP Handlers]]
- [[_COMMUNITY_Hook CLI & Flags|Hook CLI & Flags]]
- [[_COMMUNITY_Types & Interfaces|Types & Interfaces]]
- [[_COMMUNITY_Walker Config & Wrappers|Walker Config & Wrappers]]
- [[_COMMUNITY_URL Domain Resolver|URL Domain Resolver]]
- [[_COMMUNITY_Config Validation|Config Validation]]
- [[_COMMUNITY_Design Spec Architecture|Design Spec: Architecture]]
- [[_COMMUNITY_Design Spec Pipeline|Design Spec: Pipeline]]
- [[_COMMUNITY_Design Spec Scopes|Design Spec: Scopes]]
- [[_COMMUNITY_Design Spec Telemetry|Design Spec: Telemetry]]
- [[_COMMUNITY_Design Spec CLI|Design Spec: CLI]]
- [[_COMMUNITY_Implementation Plan|Implementation Plan]]
- [[_COMMUNITY_Design Spec Security|Design Spec: Security]]
- [[_COMMUNITY_Design Spec LLM Review|Design Spec: LLM Review]]
- [[_COMMUNITY_Test Data Rules|Test Data: Rules]]
- [[_COMMUNITY_Design Spec API|Design Spec: API]]
- [[_COMMUNITY_Design Spec Corpus|Design Spec: Corpus]]
- [[_COMMUNITY_README Overview|README Overview]]
- [[_COMMUNITY_Design Spec Config|Design Spec: Config]]
- [[_COMMUNITY_Design Spec Evasion Mitigations|Design Spec: Evasion Mitigations]]

## God Nodes (most connected - your core abstractions)
1. `walk()` - 62 edges
2. `ParseAndWalk()` - 60 edges
3. `findByName()` - 55 edges
4. `New()` - 51 edges
5. `testConfig()` - 32 edges
6. `mustNewServer()` - 31 edges
7. `NewEngine()` - 31 edges
8. `HandlePreToolUse()` - 24 edges
9. `parseTestFlags()` - 23 edges
10. `Open()` - 22 edges

## Surprising Connections (you probably didn't know these)
- `Classification Flow (Parse -> Rules -> LLM Review)` --semantically_similar_to--> `Classification Pipeline - 10-Stage Parse to Decision Flow`  [INFERRED] [semantically similar]
  README.md → docs/superpowers/specs/2026-04-06-stargate-design.md
- `Scope-Based Trust - Operator-Defined Trust Boundaries` --semantically_similar_to--> `Scopes and Resolvers - Contextual Trust Design`  [INFERRED] [semantically similar]
  README.md → docs/superpowers/specs/2026-04-06-stargate-design.md
- `Contextual Trust Layer - Scopes and Resolvers` --semantically_similar_to--> `Scopes and Resolvers - Contextual Trust Design`  [INFERRED] [semantically similar]
  CLAUDE.md → docs/superpowers/specs/2026-04-06-stargate-design.md
- `RED Classification - Hard Block (rm -rf, sudo, nc)` --semantically_similar_to--> `Default RED Rules - Destructive, Privilege Escalation, Exfiltration`  [INFERRED] [semantically similar]
  README.md → docs/superpowers/specs/2026-04-06-stargate-design.md
- `GREEN Classification - Safe to Execute (ls, git status)` --semantically_similar_to--> `Default GREEN Rules - Read-Only, Toolchains, Trusted Scopes`  [INFERRED] [semantically similar]
  README.md → docs/superpowers/specs/2026-04-06-stargate-design.md

## Hyperedges (group relationships)
- **Classification Pipeline Flow: Parse -> Rules -> Cache -> Corpus -> LLM -> Decision** — spec_classification_pipeline, spec_ast_walking, spec_rule_matching, spec_command_cache, spec_structural_signatures, spec_llm_review_protocol, spec_secret_scrubbing [EXTRACTED 1.00]
- **Defense-in-Depth Security Layers: AST + Rules + Scopes + LLM** — claude_md_ast_parsing_layer, claude_md_rule_engine_layer, claude_md_contextual_trust_layer, claude_md_llm_review_layer, claude_md_defense_in_depth [EXTRACTED 1.00]
- **Implementation Milestone Progression with Retrospectives** — plan_m0_skeleton, plan_m1_parser_walker, plan_m1_retrospective, plan_m2_rule_engine, plan_m2_retrospective, plan_m3_scopes_resolvers, plan_m3_retrospective, plan_m4_llm_review, plan_m4_retrospective [EXTRACTED 1.00]

## Communities

### Community 0 - "Telemetry & Observability"
Cohesion: 0.05
Nodes (40): RedactedString, severityFromDecision(), truncateBytes(), TestMetrics_LiveTelemetry_NilMetrics(), handleServe(), isLoopbackAddr(), TestIsLoopbackAddr(), basicAuth() (+32 more)

### Community 1 - "Command Cache & Deduplication"
Cohesion: 0.08
Nodes (51): cacheKey(), NewCommandCache(), TestCommandCache(), CachedDecision, CommandCache, New(), New(), Server (+43 more)

### Community 2 - "Corpus Storage & SQLite"
Cohesion: 0.09
Nodes (53): checkPermissions(), Corpus, createSchema(), formatAge(), handleCorpus(), handleCorpusClear(), handleCorpusExport(), handleCorpusImport() (+45 more)

### Community 3 - "File Retrieval & Scrubbing"
Cohesion: 0.08
Nodes (57): FormatPrecedent, anchorPattern(), isAllowed(), ResolveFiles(), makeFile(), newScrubber(), realTempDir(), TestResolveFiles_AllowedPath() (+49 more)

### Community 4 - "Classifier Pipeline & LLM Review"
Cohesion: 0.07
Nodes (54): mockProvider, NewWithProvider(), reviewerFunc, boolPtr(), llmTestConfig(), newClassifier(), TestClassifyASTSummaryCommandsFound(), TestClassifyASTSummaryHasPipes() (+46 more)

### Community 5 - "Claude Code Adapter & Hooks"
Cohesion: 0.09
Nodes (63): bashToolInput, hookOutput, hookOutputJSON, hookSpecificOutput, postToolUseInput, preToolUseInput, TraceData, TraceNotFoundError (+55 more)

### Community 6 - "Shell Parser & AST Walking"
Cohesion: 0.11
Nodes (58): Parse(), ParseAndWalk(), TestParseAndWalkReturnsError(), TestParseDialects(), TestParseInvalidCommand(), TestParseSimpleCommand(), TestWalkBraceExpansion(), TestWalkCasePatternSubstitution() (+50 more)

### Community 7 - "Evasion Test Corpus"
Cohesion: 0.12
Nodes (61): findByName(), hasNameStart(), TestEvasion_AliasRawName(), TestEvasion_AnsiCHexEscape(), TestEvasion_AnsiCMixed(), TestEvasion_AnsiCNullByteTruncation(), TestEvasion_AnsiCOctalEscape(), TestEvasion_AnsiCUnicodeEscape() (+53 more)

### Community 8 - "Security Framework & Risks"
Cohesion: 0.05
Nodes (50): Accepted Risk: Adversarial Instructions in Corpus Reasoning, Accepted Risk: Base64-Encoded Secrets Not Detected, Accepted Risk: files_requested Reveals LLM Reasoning Patterns, Accepted Risk: Precedent Reasoning Accumulation, Accepted Risk: POST /test as Classification Oracle, Accepted Risk: TOCTOU in File Retrieval Path Validation, Accepted Risk: Variable Names May Reveal Secret Existence, AST Parsing Layer - mvdan.cc/sh/v3 Shell Parser (+42 more)

### Community 9 - "Rule Engine & Matching"
Cohesion: 0.16
Nodes (38): compileRules(), decisionToAction(), isDecomposable(), matchArgs(), matchContext(), matchFlags(), matchScope(), NewEngine() (+30 more)

### Community 10 - "Classify Request/Response"
Cohesion: 0.12
Nodes (34): ASTSummary, buildASTSummary(), Classifier, ClassifyRequest, ClassifyResponse, classifyState, collectFlags(), CommandSummary (+26 more)

### Community 11 - "Test Endpoint & CLI"
Cohesion: 0.13
Nodes (37): testFlags, testHTTPRequest, formatOneLiner(), handleTest(), parseTestFlags(), printResponse(), runOffline(), runServer() (+29 more)

### Community 12 - "LLM Provider & Anthropic SDK"
Cohesion: 0.11
Nodes (24): NewResolverAdapter(), HasCLI(), NewAnthropicProvider(), parseResponse(), TestNewAnthropicProvider_NoAuth(), TestNewAnthropicProvider_ReviewWithoutAuth(), TestNewAnthropicProvider_WithEnvAPIKey(), TestParseResponse_Allow() (+16 more)

### Community 13 - "Config Subcommands"
Cohesion: 0.17
Nodes (30): handleConfig(), handleConfigDump(), handleConfigRules(), handleConfigScopes(), handleConfigValidate(), Load(), printRule(), captureStdout() (+22 more)

### Community 14 - "GitHub Scope Resolver"
Cohesion: 0.17
Nodes (29): ownerFromAPIPath(), ownerFromGitConfig(), ownerFromGitPath(), ownerFromGitURL(), ownerFromRepoFlag(), parseGitConfigOriginURL(), parseOwnerRepo(), ResolveGitHubRepoOwner() (+21 more)

### Community 15 - "AST Walker Internals"
Cohesion: 0.18
Nodes (31): WalkerConfig, walkerState, WrapperDef, binCmdOpString(), classifyArgs(), collectPipelineStages(), dblQuotedLiteral(), DefaultWalkerConfig() (+23 more)

### Community 16 - "Feedback Handler"
Cohesion: 0.16
Nodes (24): FeedbackRequest, Handler, TraceInfo, NewHandler(), TestHandleFeedbackExpiredTrace(), TestHandleFeedbackIdempotent(), TestHandleFeedbackInvalidHMAC(), TestHandleFeedbackMissingFields() (+16 more)

### Community 17 - "HTTP Client & Adapter"
Cohesion: 0.16
Nodes (25): ClassifyRequest, ClassifyResponse, ClientConfig, FeedbackBody, FeedbackRequest, Classify(), doPostWithRetry(), isConnectionRefused() (+17 more)

### Community 18 - "Corpus Tests & Scrubber"
Cohesion: 0.17
Nodes (13): TestCorpus(), New(), Scrubber, scrubPattern, newScrubber(), TestNewInvalidPattern(), TestScrubCommandInfo(), TestScrubEnvVars() (+5 more)

### Community 19 - "Init Subcommand"
Cohesion: 0.24
Nodes (17): readFile(), defaultConfigPath(), expandHome(), handleInit(), homeDir(), parseInitFlags(), TestExpandHome(), TestInit_CreatesConfig() (+9 more)

### Community 20 - "TTLMap Cache"
Cohesion: 0.23
Nodes (15): main(), parseGlobalArgs(), ResolveConfigPath(), TestHandlers_AllSubcommandsRegistered(), TestHandlers_UnimplementedReturnNonZero(), TestHandlers_UnknownSubcommandNotRegistered(), TestParseGlobalArgs_ConfigFlag(), TestParseGlobalArgs_ConfigFlagEquals() (+7 more)

### Community 21 - "Scope Registry"
Cohesion: 0.35
Nodes (15): initMetrics(), collectMetrics(), findCounter(), findHistogramCount(), newTestMetrics(), TestMetrics_ClassificationsTotal(), TestMetrics_ClassifyDuration(), TestMetrics_CorpusHitsTotal() (+7 more)

### Community 22 - "Server HTTP Handlers"
Cohesion: 0.27
Nodes (13): init(), StripFenceTags(), TestAllFenceTagNames(), TestCaseInsensitive(), TestIterationBound(), TestMixedContent(), TestNonFenceTagsPreserved(), TestRecursiveTagStripping() (+5 more)

### Community 23 - "Hook CLI & Flags"
Cohesion: 0.32
Nodes (10): assertAttr(), newTestLogger(), recordAttrs(), TestLogClassification_AllAttributes(), TestLogClassification_LLMAttributesConditional(), TestLogClassification_ScopeResolvedTruncated(), TestLogClassification_ScrubCommandGated(), TestLogClassification_SeverityMapping() (+2 more)

### Community 24 - "Types & Interfaces"
Cohesion: 0.29
Nodes (9): rateLimitedProvider, stubProvider, NewRateLimitedProvider(), TestErrRateLimitedIs(), TestRateLimitDisabledWhenNegative(), TestRateLimitDisabledWhenZero(), TestRateLimitExceeded(), TestRateLimitPassesThroughError() (+1 more)

### Community 25 - "Walker Config & Wrappers"
Cohesion: 0.3
Nodes (11): handleHook(), parseHookFlags(), resolveURL(), TestHandleHook_AllowRemoteOverridesLoopbackCheck(), TestHandleHook_NonLoopbackRejected(), TestParseHookFlags_AgentRequired(), TestParseHookFlags_UnknownAgent(), TestParseHookFlags_UnknownEvent() (+3 more)

### Community 26 - "URL Domain Resolver"
Cohesion: 0.39
Nodes (4): extractURLCandidate(), parseURLDomain(), ResolveURLDomain(), TestResolveURLDomain()

### Community 27 - "Config Validation"
Cohesion: 0.53
Nodes (4): entry, New(), Options, TTLMap

### Community 28 - "Design Spec: Architecture"
Cohesion: 0.6
Nodes (3): ReviewerProvider, ReviewRequest, ReviewResponse

### Community 29 - "Design Spec: Pipeline"
Cohesion: 0.4
Nodes (5): Dependency: BurntSushi/toml - TOML Config Parsing, Trust Boundaries - stargate.toml as Root Trust Anchor, M0: Skeleton - CLI, Config, HTTP Server, /health, Configuration File Specification (stargate.toml), Rationale: TOML Config Format - Comments, Unambiguous Types

### Community 30 - "Design Spec: Scopes"
Cohesion: 0.4
Nodes (5): Milestone Transition Protocol - Design Verification, M1 Retrospective - 84 Threads, 20 Rounds, Underspecified Design, M2 Retrospective - 61 Threads, API Schema and Handler Hardening, M3 Retrospective - 28 Threads, Panel Review Effective, M4 Retrospective - 91 Threads, Split-PR Amplification

### Community 31 - "Design Spec: Telemetry"
Cohesion: 0.67
Nodes (4): Stargate Implementation Plan, Stargate - Bash Command Classifier for AI Coding Agents, Stargate Design Specification (PRD v0.2.0), Rationale: Go Language Choice - mvdan.cc/sh, Static Binary, Fast Startup

### Community 32 - "Design Spec: CLI"
Cohesion: 0.5
Nodes (4): Accepted Risk: --allow-remote Without Transport Security, Accepted Risk: tool_name Bypass via Agent Renaming, Agent Adapters - stargate hook Protocol Translation, Claude Code Adapter - PreToolUse/PostToolUse Hook Integration

### Community 33 - "Implementation Plan"
Cohesion: 0.67
Nodes (1): Stats

### Community 34 - "Design Spec: Security"
Cohesion: 0.67
Nodes (1): TestRequest

### Community 35 - "Design Spec: LLM Review"
Cohesion: 0.67
Nodes (3): Accepted Risk: Telemetry Env Var Overrides Bypass stargate.toml, Dependency: go.opentelemetry.io/otel - OpenTelemetry SDK, Telemetry - OpenTelemetry Traces, Metrics, Logs to Grafana Cloud

### Community 36 - "Test Data: Rules"
Cohesion: 0.67
Nodes (3): RED Classification - Hard Block (rm -rf, sudo, nc), Red Commands Test Data - Dangerous Command Samples, Default RED Rules - Destructive, Privilege Escalation, Exfiltration

### Community 37 - "Design Spec: API"
Cohesion: 0.67
Nodes (3): Green Commands Test Data - Safe Command Samples, GREEN Classification - Safe to Execute (ls, git status), Default GREEN Rules - Read-Only, Toolchains, Trusted Scopes

### Community 38 - "Design Spec: Corpus"
Cohesion: 0.67
Nodes (3): YELLOW Classification - Ambiguous, LLM or User Review, Default YELLOW Rules - Network, Docker, Package Install, Shell, Yellow Commands Test Data - Ambiguous Command Samples

### Community 39 - "README Overview"
Cohesion: 1.0
Nodes (2): Panel Review Process - Synthetic Expert Panel, Security Design Checklist - 9 Required Items

### Community 40 - "Design Spec: Config"
Cohesion: 1.0
Nodes (2): Fail-Closed Design - Block on Error, Fail-Closed Design - Parse Error, Timeout, Resolver Failure Handling

### Community 43 - "Design Spec: Evasion Mitigations"
Cohesion: 1.0
Nodes (1): Architecture - HTTP Server + Shell Parser + Rule Engine + LLM

## Knowledge Gaps
- **42 isolated node(s):** `Classification Flow (Parse -> Rules -> LLM Review)`, `RED Classification - Hard Block (rm -rf, sudo, nc)`, `GREEN Classification - Safe to Execute (ls, git status)`, `YELLOW Classification - Ambiguous, LLM or User Review`, `Feedback Loop - Post-execution Outcome Recording` (+37 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **Thin community `Implementation Plan`** (3 nodes): `Stats`, `admin.go`, `admin.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Design Spec: Security`** (3 nodes): `test_endpoint.go`, `TestRequest`, `test_endpoint.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `README Overview`** (2 nodes): `Panel Review Process - Synthetic Expert Panel`, `Security Design Checklist - 9 Required Items`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Design Spec: Config`** (2 nodes): `Fail-Closed Design - Block on Error`, `Fail-Closed Design - Parse Error, Timeout, Resolver Failure Handling`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Design Spec: Evasion Mitigations`** (1 nodes): `Architecture - HTTP Server + Shell Parser + Rule Engine + LLM`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `New()` connect `Command Cache & Deduplication` to `Telemetry & Observability`, `Corpus Storage & SQLite`, `File Retrieval & Scrubbing`, `Classifier Pipeline & LLM Review`, `Claude Code Adapter & Hooks`, `Rule Engine & Matching`, `Classify Request/Response`, `Test Endpoint & CLI`, `LLM Provider & Anthropic SDK`, `Config Subcommands`, `AST Walker Internals`, `Feedback Handler`, `Corpus Tests & Scrubber`, `Types & Interfaces`?**
  _High betweenness centrality (0.118) - this node is a cross-community bridge._
- **Why does `walk()` connect `Evasion Test Corpus` to `Shell Parser & AST Walking`, `AST Walker Internals`?**
  _High betweenness centrality (0.037) - this node is a cross-community bridge._
- **Why does `ParseAndWalk()` connect `Shell Parser & AST Walking` to `Corpus Storage & SQLite`, `AST Walker Internals`, `Classifier Pipeline & LLM Review`, `Evasion Test Corpus`?**
  _High betweenness centrality (0.034) - this node is a cross-community bridge._
- **Are the 2 inferred relationships involving `walk()` (e.g. with `ParseAndWalk()` and `DefaultWalkerConfig()`) actually correct?**
  _`walk()` has 2 INFERRED edges - model-reasoned connections that need verification._
- **Are the 57 inferred relationships involving `ParseAndWalk()` (e.g. with `handleCorpusSearch()` and `walk()`) actually correct?**
  _`ParseAndWalk()` has 57 INFERRED edges - model-reasoned connections that need verification._
- **Are the 47 inferred relationships involving `New()` (e.g. with `handleConfigDump()` and `runOffline()`) actually correct?**
  _`New()` has 47 INFERRED edges - model-reasoned connections that need verification._
- **Are the 13 inferred relationships involving `testConfig()` (e.g. with `TestTest_SameSchemaAsClassify()` and `TestTest_ASTAlwaysPopulated()`) actually correct?**
  _`testConfig()` has 13 INFERRED edges - model-reasoned connections that need verification._