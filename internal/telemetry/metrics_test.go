package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// newTestMetrics creates a metrics instance backed by an in-memory reader
// for testing. Returns the metrics, the reader (for collecting), and a cleanup func.
func newTestMetrics(t *testing.T) (*metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { provider.Shutdown(context.Background()) })

	m := provider.Meter("stargate-test")
	mt, err := initMetrics(m)
	if err != nil {
		t.Fatalf("initMetrics: %v", err)
	}
	return mt, reader
}

// collectMetrics reads all metrics from the manual reader.
func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return rm
}

// findCounter finds a counter metric by name and returns the sum of all data points.
func findCounter(rm metricdata.ResourceMetrics, name string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					var total int64
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
					return total
				}
			}
		}
	}
	return 0
}

// findHistogramCount finds a histogram metric and returns the total count across data points.
func findHistogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if hist, ok := m.Data.(metricdata.Histogram[float64]); ok {
					var total uint64
					for _, dp := range hist.DataPoints {
						total += dp.Count
					}
					return total
				}
			}
		}
	}
	return 0
}

func TestMetrics_ClassificationsTotal(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.classificationsTotal.Add(context.Background(), 1)
	mt.classificationsTotal.Add(context.Background(), 1)

	rm := collectMetrics(t, reader)
	got := findCounter(rm, "stargate_classifications_total")
	if got != 2 {
		t.Errorf("classificationsTotal: got %d, want 2", got)
	}
}

func TestMetrics_LLMCallsTotal(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.llmCallsTotal.Add(context.Background(), 1)

	rm := collectMetrics(t, reader)
	got := findCounter(rm, "stargate_llm_calls_total")
	if got != 1 {
		t.Errorf("llmCallsTotal: got %d, want 1", got)
	}
}

func TestMetrics_ParseErrorsTotal(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.parseErrorsTotal.Add(context.Background(), 3)

	rm := collectMetrics(t, reader)
	got := findCounter(rm, "stargate_parse_errors_total")
	if got != 3 {
		t.Errorf("parseErrorsTotal: got %d, want 3", got)
	}
}

func TestMetrics_ClassifyDuration(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.classifyDuration.Record(context.Background(), 5.0)
	mt.classifyDuration.Record(context.Background(), 150.0)

	rm := collectMetrics(t, reader)
	got := findHistogramCount(rm, "stargate_classify_duration")
	if got != 2 {
		t.Errorf("classifyDuration count: got %d, want 2", got)
	}
}

func TestMetrics_ParseDuration(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.parseDuration.Record(context.Background(), 0.05)

	rm := collectMetrics(t, reader)
	got := findHistogramCount(rm, "stargate_parse_duration")
	if got != 1 {
		t.Errorf("parseDuration count: got %d, want 1", got)
	}
}

func TestMetrics_LLMDuration(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.llmDuration.Record(context.Background(), 500.0)

	rm := collectMetrics(t, reader)
	got := findHistogramCount(rm, "stargate_llm_duration")
	if got != 1 {
		t.Errorf("llmDuration count: got %d, want 1", got)
	}
}

func TestMetrics_FeedbackTotal(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.feedbackTotal.Add(context.Background(), 1)

	rm := collectMetrics(t, reader)
	got := findCounter(rm, "stargate_feedback_total")
	if got != 1 {
		t.Errorf("feedbackTotal: got %d, want 1", got)
	}
}

func TestMetrics_CorpusHitsTotal(t *testing.T) {
	mt, reader := newTestMetrics(t)

	mt.corpusHitsTotal.Add(context.Background(), 2)

	rm := collectMetrics(t, reader)
	got := findCounter(rm, "stargate_corpus_hits_total")
	if got != 2 {
		t.Errorf("corpusHitsTotal: got %d, want 2", got)
	}
}

func TestMetrics_LiveTelemetry_RecordClassification(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { provider.Shutdown(context.Background()) })

	mt, err := initMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("initMetrics: %v", err)
	}

	lt := &LiveTelemetry{metrics: mt}
	lt.RecordClassification("green", "rule_basic", 2.5)

	rm := collectMetrics(t, reader)
	if got := findCounter(rm, "stargate_classifications_total"); got != 1 {
		t.Errorf("RecordClassification counter: got %d, want 1", got)
	}
	if got := findHistogramCount(rm, "stargate_classify_duration"); got != 1 {
		t.Errorf("RecordClassification histogram: got %d, want 1", got)
	}
}

func TestMetrics_LiveTelemetry_NilMetrics(t *testing.T) {
	// When metrics is nil (ExportMetrics=false), recording should not panic.
	lt := &LiveTelemetry{metrics: nil}
	lt.RecordClassification("green", "rule", 1.0)
	lt.RecordLLMCall("allow", 100.0)
	lt.RecordParseError()
	lt.RecordFeedback("executed")
	lt.RecordCorpusHit("exact")
	lt.RecordCorpusWrite("allow")
	lt.RecordScopeResolution("github", "resolved")
	lt.SetRulesLoaded("red", 5)
	lt.SetCorpusEntries("allow", 10)
}
