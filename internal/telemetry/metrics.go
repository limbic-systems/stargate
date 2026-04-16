package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// metrics holds all registered OTel metric instruments.
type metrics struct {
	classificationsTotal    metric.Int64Counter
	llmCallsTotal          metric.Int64Counter
	parseErrorsTotal       metric.Int64Counter
	configReloadsTotal     metric.Int64Counter
	corpusHitsTotal        metric.Int64Counter
	corpusWritesTotal      metric.Int64Counter
	feedbackTotal          metric.Int64Counter
	scopeResolutionsTotal  metric.Int64Counter

	classifyDuration metric.Float64Histogram
	parseDuration    metric.Float64Histogram
	llmDuration      metric.Float64Histogram

	rulesLoaded   metric.Int64Gauge
	corpusEntries metric.Int64Gauge
}

// initMetrics registers all metric instruments with the given meter.
func initMetrics(m metric.Meter) (*metrics, error) {
	var (
		mt  metrics
		err error
	)

	// Counters.
	mt.classificationsTotal, err = m.Int64Counter("stargate_classifications_total")
	if err != nil {
		return nil, err
	}
	mt.llmCallsTotal, err = m.Int64Counter("stargate_llm_calls_total")
	if err != nil {
		return nil, err
	}
	mt.parseErrorsTotal, err = m.Int64Counter("stargate_parse_errors_total")
	if err != nil {
		return nil, err
	}
	mt.configReloadsTotal, err = m.Int64Counter("stargate_config_reloads_total")
	if err != nil {
		return nil, err
	}
	mt.corpusHitsTotal, err = m.Int64Counter("stargate_corpus_hits_total")
	if err != nil {
		return nil, err
	}
	mt.corpusWritesTotal, err = m.Int64Counter("stargate_corpus_writes_total")
	if err != nil {
		return nil, err
	}
	mt.feedbackTotal, err = m.Int64Counter("stargate_feedback_total")
	if err != nil {
		return nil, err
	}
	mt.scopeResolutionsTotal, err = m.Int64Counter("stargate_scope_resolutions_total")
	if err != nil {
		return nil, err
	}

	// Histograms — names omit unit suffix; unit set via option for Prometheus auto-suffix.
	mt.classifyDuration, err = m.Float64Histogram("stargate_classify_duration",
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 50, 100, 500, 1000, 5000, 10000),
	)
	if err != nil {
		return nil, err
	}
	mt.parseDuration, err = m.Float64Histogram("stargate_parse_duration",
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5),
	)
	if err != nil {
		return nil, err
	}
	mt.llmDuration, err = m.Float64Histogram("stargate_llm_duration",
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(50, 100, 250, 500, 1000, 2000, 5000, 10000),
	)
	if err != nil {
		return nil, err
	}

	// Gauges.
	mt.rulesLoaded, err = m.Int64Gauge("stargate_rules_loaded")
	if err != nil {
		return nil, err
	}
	mt.corpusEntries, err = m.Int64Gauge("stargate_corpus_entries")
	if err != nil {
		return nil, err
	}

	return &mt, nil
}

// --- LiveTelemetry metric recording methods ---

func (lt *LiveTelemetry) RecordClassification(decision, ruleLevel string, durationMs float64) {
	if lt.metrics == nil {
		return
	}
	ctx := context.Background()
	lt.metrics.classificationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("decision", decision),
			attribute.String("rule_level", ruleLevel),
		),
	)
	lt.metrics.classifyDuration.Record(ctx, durationMs)
}

func (lt *LiveTelemetry) RecordLLMCall(outcome string, durationMs float64) {
	if lt.metrics == nil {
		return
	}
	ctx := context.Background()
	lt.metrics.llmCallsTotal.Add(ctx, 1,
		metric.WithAttributes(attribute.String("outcome", outcome)),
	)
	lt.metrics.llmDuration.Record(ctx, durationMs)
}

func (lt *LiveTelemetry) RecordParseError() {
	if lt.metrics == nil {
		return
	}
	lt.metrics.parseErrorsTotal.Add(context.Background(), 1)
}

func (lt *LiveTelemetry) RecordFeedback(outcome string) {
	if lt.metrics == nil {
		return
	}
	lt.metrics.feedbackTotal.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("outcome", outcome)),
	)
}

func (lt *LiveTelemetry) RecordCorpusHit(hitType string) {
	if lt.metrics == nil {
		return
	}
	lt.metrics.corpusHitsTotal.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("type", hitType)),
	)
}

func (lt *LiveTelemetry) RecordCorpusWrite(decision string) {
	if lt.metrics == nil {
		return
	}
	lt.metrics.corpusWritesTotal.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("decision", decision)),
	)
}

func (lt *LiveTelemetry) RecordScopeResolution(resolver, result string) {
	if lt.metrics == nil {
		return
	}
	lt.metrics.scopeResolutionsTotal.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("resolver", resolver),
			attribute.String("result", result),
		),
	)
}

func (lt *LiveTelemetry) SetRulesLoaded(level string, count int) {
	if lt.metrics == nil {
		return
	}
	lt.metrics.rulesLoaded.Record(context.Background(), int64(count),
		metric.WithAttributes(attribute.String("level", level)),
	)
}

func (lt *LiveTelemetry) SetCorpusEntries(decision string, count int) {
	if lt.metrics == nil {
		return
	}
	lt.metrics.corpusEntries.Record(context.Background(), int64(count),
		metric.WithAttributes(attribute.String("decision", decision)),
	)
}
