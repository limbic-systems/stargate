package telemetry

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/limbic-systems/stargate/internal/config"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// captureProcessor is a simple log processor that captures emitted records for testing.
type captureProcessor struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (p *captureProcessor) OnEmit(_ context.Context, record *sdklog.Record) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, *record)
	return nil
}

func (p *captureProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }
func (p *captureProcessor) Shutdown(context.Context) error               { return nil }
func (p *captureProcessor) ForceFlush(context.Context) error             { return nil }

func (p *captureProcessor) getRecords() []sdklog.Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]sdklog.Record, len(p.records))
	copy(cp, p.records)
	return cp
}

func newTestLogger(t *testing.T, includeScrub bool) (*LiveTelemetry, *captureProcessor) {
	t.Helper()
	proc := &captureProcessor{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))
	t.Cleanup(func() { provider.Shutdown(context.Background()) })

	lt := &LiveTelemetry{
		cfg:    config.TelemetryConfig{IncludeScrubCommand: includeScrub},
		logger: provider.Logger("stargate-test"),
	}
	return lt, proc
}

// recordAttrs extracts string attribute values from an SDK log record.
func recordAttrs(rec sdklog.Record) map[string]string {
	attrs := make(map[string]string)
	rec.WalkAttributes(func(kv otellog.KeyValue) bool {
		attrs[string(kv.Key)] = kv.Value.AsString()
		return true
	})
	return attrs
}

func assertAttr(t *testing.T, attrs map[string]string, key, want string) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Errorf("attribute %q missing", key)
		return
	}
	if got != want {
		t.Errorf("attribute %q: got %q, want %q", key, got, want)
	}
}

func TestLogClassification_AllAttributes(t *testing.T) {
	lt, proc := newTestLogger(t, true)

	result := ClassifyResult{
		Decision:         "green",
		Action:           "allow",
		RuleLevel:        "basic",
		RuleReason:       "safe command",
		TotalMs:          2.5,
		LLMCalled:        true,
		LLMDecision:      "allow",
		LLMDurationMs:    150.0,
		CorpusPrecedents: 3,
		ScopeResolved:    "limbic-systems/stargate",
		SessionID:        "sess-123",
		ScrubCommand:     "git status",
		RequestCWD:       "/home/user/project",
	}
	lt.LogClassification(context.Background(), result)

	records := proc.getRecords()
	if len(records) == 0 {
		t.Fatal("no log records emitted")
	}
	rec := records[0]

	if rec.Severity() != otellog.SeverityInfo {
		t.Errorf("severity: got %v, want Info", rec.Severity())
	}

	attrs := recordAttrs(rec)
	assertAttr(t, attrs, "stargate.decision", "green")
	assertAttr(t, attrs, "stargate.action", "allow")
	assertAttr(t, attrs, "stargate.rule.level", "basic")
	assertAttr(t, attrs, "stargate.llm.decision", "allow")
	assertAttr(t, attrs, "stargate.scope.resolved", "limbic-systems/stargate")
	assertAttr(t, attrs, "stargate.scrubbed_command", "git status")
	assertAttr(t, attrs, "stargate.request_cwd", "/home/user/project")
	assertAttr(t, attrs, "stargate.session_id", "sess-123")
}

func TestLogClassification_ScrubCommandGated(t *testing.T) {
	lt, proc := newTestLogger(t, false)

	lt.LogClassification(context.Background(), ClassifyResult{
		Decision:     "yellow",
		Action:       "ask",
		ScrubCommand: "rm -rf /secret",
		RequestCWD:   "/secret/path",
	})

	records := proc.getRecords()
	if len(records) == 0 {
		t.Fatal("no log records emitted")
	}
	attrs := recordAttrs(records[0])

	if _, ok := attrs["stargate.scrubbed_command"]; ok {
		t.Error("scrubbed_command should be absent when IncludeScrubCommand=false")
	}
	if _, ok := attrs["stargate.request_cwd"]; ok {
		t.Error("request_cwd should be absent when IncludeScrubCommand=false")
	}
}

func TestLogClassification_SeverityMapping(t *testing.T) {
	tests := []struct {
		decision string
		want     otellog.Severity
	}{
		{"green", otellog.SeverityInfo},
		{"yellow", otellog.SeverityWarn},
		{"red", otellog.SeverityError},
		{"GREEN", otellog.SeverityInfo},
		{"unknown", otellog.SeverityInfo},
	}

	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			lt, proc := newTestLogger(t, false)
			lt.LogClassification(context.Background(), ClassifyResult{Decision: tt.decision})

			records := proc.getRecords()
			if len(records) == 0 {
				t.Fatal("no log records emitted")
			}
			if got := records[0].Severity(); got != tt.want {
				t.Errorf("decision %q: severity got %v, want %v", tt.decision, got, tt.want)
			}
		})
	}
}

func TestLogClassification_ScopeResolvedTruncated(t *testing.T) {
	lt, proc := newTestLogger(t, false)

	longScope := strings.Repeat("x", 300)
	lt.LogClassification(context.Background(), ClassifyResult{ScopeResolved: longScope})

	records := proc.getRecords()
	if len(records) == 0 {
		t.Fatal("no log records emitted")
	}
	attrs := recordAttrs(records[0])
	got := attrs["stargate.scope.resolved"]
	if len(got) != 256 {
		t.Errorf("scope.resolved length: got %d, want 256", len(got))
	}
}

func TestLogClassification_LLMAttributesConditional(t *testing.T) {
	lt, proc := newTestLogger(t, false)

	lt.LogClassification(context.Background(), ClassifyResult{Decision: "green", LLMCalled: false})

	records := proc.getRecords()
	if len(records) == 0 {
		t.Fatal("no log records emitted")
	}
	attrs := recordAttrs(records[0])
	if _, ok := attrs["stargate.llm.decision"]; ok {
		t.Error("llm.decision should be absent when LLMCalled=false")
	}
}

func TestTruncateBytes(t *testing.T) {
	if got := truncateBytes("hello", 10); got != "hello" {
		t.Errorf("no truncation: got %q", got)
	}
	if got := truncateBytes("hello", 3); got != "hel" {
		t.Errorf("truncation: got %q, want %q", got, "hel")
	}
	if got := truncateBytes("", 5); got != "" {
		t.Errorf("empty: got %q", got)
	}
}
