package telemetry

import (
	"context"
	"testing"

	"github.com/limbic-systems/stargate/internal/config"
	"github.com/limbic-systems/stargate/internal/ttlmap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func newTestTracer(t *testing.T) (*LiveTelemetry, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { tp.Shutdown(context.Background()) })

	lt := &LiveTelemetry{
		cfg:            config.TelemetryConfig{},
		tracerProvider: tp,
		tracer:         tp.Tracer("stargate-test"),
	}
	return lt, exporter
}

func TestStartClassifySpan(t *testing.T) {
	lt, exporter := newTestTracer(t)

	ctx, span := lt.StartClassifySpan(context.Background())
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count: got %d, want 1", len(spans))
	}
	if spans[0].Name != "stargate.classify" {
		t.Errorf("span name: got %q, want %q", spans[0].Name, "stargate.classify")
	}

	// Context should contain the span.
	if trace.SpanFromContext(ctx) != span {
		t.Error("context should contain the started span")
	}
}

func TestStartSpan_ChildOfClassify(t *testing.T) {
	lt, exporter := newTestTracer(t)

	ctx, classifySpan := lt.StartClassifySpan(context.Background())
	_, childSpan := lt.StartSpan(ctx, "stargate.parse")
	childSpan.End()
	classifySpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("span count: got %d, want 2", len(spans))
	}

	// Find the child span.
	var parseSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "stargate.parse" {
			parseSpan = &spans[i]
			break
		}
	}
	if parseSpan == nil {
		t.Fatal("stargate.parse span not found")
	}

	// Child should have the classify span as parent.
	classifySC := classifySpan.SpanContext()
	if parseSpan.Parent.SpanID() != classifySC.SpanID() {
		t.Errorf("parse span parent: got %s, want %s", parseSpan.Parent.SpanID(), classifySC.SpanID())
	}
}

func TestTraceIDFromContext(t *testing.T) {
	lt, _ := newTestTracer(t)

	ctx, span := lt.StartClassifySpan(context.Background())
	defer span.End()

	traceID := lt.TraceIDFromContext(ctx)
	if traceID == "" {
		t.Fatal("TraceIDFromContext returned empty string")
	}
	if len(traceID) != 32 {
		t.Errorf("TraceID length: got %d, want 32 hex chars", len(traceID))
	}
}

func TestTraceIDFromContext_NoSpan(t *testing.T) {
	lt, _ := newTestTracer(t)

	traceID := lt.TraceIDFromContext(context.Background())
	if traceID != "" {
		t.Errorf("TraceIDFromContext with no span: got %q, want empty", traceID)
	}
}

func TestStartFeedbackSpan_WithLink(t *testing.T) {
	lt, exporter := newTestTracer(t)

	// Create a classify span to get a trace ID.
	ctx, classifySpan := lt.StartClassifySpan(context.Background())
	originalTraceID := lt.TraceIDFromContext(ctx)
	classifySpan.End()

	// Create a feedback span linked to the original.
	_, feedbackSpan := lt.StartFeedbackSpan(context.Background(), originalTraceID)
	feedbackSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("span count: got %d, want 2", len(spans))
	}

	// Find the feedback span.
	var fbSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "stargate.feedback" {
			fbSpan = &spans[i]
			break
		}
	}
	if fbSpan == nil {
		t.Fatal("stargate.feedback span not found")
	}

	// Should have a link to the original trace.
	if len(fbSpan.Links) == 0 {
		t.Fatal("feedback span has no links")
	}
	linkTraceID := fbSpan.Links[0].SpanContext.TraceID().String()
	if linkTraceID != originalTraceID {
		t.Errorf("link trace ID: got %q, want %q", linkTraceID, originalTraceID)
	}

	// Feedback span should be on a different trace than the classify span.
	if fbSpan.SpanContext.TraceID().String() == originalTraceID {
		t.Error("feedback span should be on a new trace, not the original")
	}

	// Should have stargate.trace_id attribute.
	found := false
	for _, attr := range fbSpan.Attributes {
		if string(attr.Key) == "stargate.trace_id" && attr.Value.AsString() == originalTraceID {
			found = true
			break
		}
	}
	if !found {
		t.Error("feedback span missing stargate.trace_id attribute")
	}
}

func TestStartFeedbackSpan_WithoutLink(t *testing.T) {
	lt, exporter := newTestTracer(t)

	// Empty trace ID — no link should be added.
	_, feedbackSpan := lt.StartFeedbackSpan(context.Background(), "")
	feedbackSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count: got %d, want 1", len(spans))
	}

	if spans[0].Name != "stargate.feedback" {
		t.Errorf("span name: got %q", spans[0].Name)
	}
	if len(spans[0].Links) != 0 {
		t.Errorf("feedback span should have no links when trace ID is empty, got %d", len(spans[0].Links))
	}
}

func TestToolUseTraceMap_StoreAndLookup(t *testing.T) {
	// Use a LiveTelemetry with only the traceMap — no OTLP exporters needed.
	lt := &LiveTelemetry{
		traceMap: ttlmap.New[string, string](context.Background(), ttlmap.Options{MaxEntries: 100}),
	}
	t.Cleanup(func() { lt.traceMap.Close() })

	lt.StoreToolUseTrace("toolu_001", "abc123def456")
	got := lt.LookupToolUseTrace("toolu_001")
	if got != "abc123def456" {
		t.Errorf("LookupToolUseTrace: got %q, want %q", got, "abc123def456")
	}
}

func TestToolUseTraceMap_MissReturnsEmpty(t *testing.T) {
	lt := &LiveTelemetry{
		traceMap: ttlmap.New[string, string](context.Background(), ttlmap.Options{MaxEntries: 100}),
	}
	t.Cleanup(func() { lt.traceMap.Close() })

	got := lt.LookupToolUseTrace("nonexistent")
	if got != "" {
		t.Errorf("LookupToolUseTrace miss: got %q, want empty", got)
	}
}

func TestSpanErrorStatus(t *testing.T) {
	lt, exporter := newTestTracer(t)

	ctx, span := lt.StartClassifySpan(context.Background())
	_, parseSpan := lt.StartSpan(ctx, "stargate.parse")
	parseSpan.RecordError(errForTest)
	parseSpan.SetStatus(codes.Error, "parse failed")
	parseSpan.End()
	span.End()

	spans := exporter.GetSpans()
	var found bool
	for _, s := range spans {
		if s.Name == "stargate.parse" {
			if s.Status.Code != codes.Error {
				t.Errorf("parse span status code: got %d, want %d (Error)", s.Status.Code, codes.Error)
			}
			if s.Status.Description != "parse failed" {
				t.Errorf("parse span status description: got %q", s.Status.Description)
			}
			found = true
		}
	}
	if !found {
		t.Error("stargate.parse span not found")
	}
}

var errForTest = errString("test error")

type errString string

func (e errString) Error() string { return string(e) }

func newLatitudeTracer(t *testing.T, captureName, tagsJSON string) (*LiveTelemetry, *tracetest.InMemoryExporter) {
	t.Helper()
	lt, exp := newTestTracer(t)
	lt.latitudeEnabled = true
	lt.latitudeCaptureName = captureName
	lt.latitudeTagsJSON = tagsJSON
	return lt, exp
}

func TestStartClassifySpan_LatitudeAttributes(t *testing.T) {
	lt, exporter := newLatitudeTracer(t, "stargate-classify", `["production","v2"]`)

	_, span := lt.StartClassifySpan(context.Background())
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count: got %d, want 1", len(spans))
	}

	attrs := spans[0].Attributes
	assertAttrStr(t, attrs, "latitude.capture.name", "stargate-classify")
	assertAttrStr(t, attrs, "latitude.tags", `["production","v2"]`)
}

func TestStartClassifySpan_NoLatitudeAttributes(t *testing.T) {
	lt, exporter := newTestTracer(t)

	_, span := lt.StartClassifySpan(context.Background())
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count: got %d, want 1", len(spans))
	}

	for _, attr := range spans[0].Attributes {
		key := string(attr.Key)
		if key == "latitude.capture.name" || key == "latitude.tags" {
			t.Errorf("unexpected Latitude attribute %q when disabled", key)
		}
	}
}

func TestStartClassifySpan_LatitudeNoTags(t *testing.T) {
	lt, exporter := newLatitudeTracer(t, "stargate-classify", "")

	_, span := lt.StartClassifySpan(context.Background())
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count: got %d, want 1", len(spans))
	}

	attrs := spans[0].Attributes
	assertAttrStr(t, attrs, "latitude.capture.name", "stargate-classify")

	for _, attr := range attrs {
		if string(attr.Key) == "latitude.tags" {
			t.Error("latitude.tags should not be set when tags are empty")
		}
	}
}

func TestClassifySpan_SessionID(t *testing.T) {
	lt, exporter := newTestTracer(t)

	_, span := lt.StartClassifySpan(context.Background())
	span.SetAttributes(attribute.String("session.id", "sess-abc"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count: got %d, want 1", len(spans))
	}
	assertAttrStr(t, spans[0].Attributes, "session.id", "sess-abc")
}

func assertAttrStr(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if got := attr.Value.AsString(); got != want {
				t.Errorf("attribute %q: got %q, want %q", key, got, want)
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}
