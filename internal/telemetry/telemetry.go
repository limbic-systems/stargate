// Package telemetry provides OpenTelemetry instrumentation for stargate.
// When telemetry is disabled, all operations are no-ops with zero overhead.
package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/limbic-systems/stargate/internal/config"
	"github.com/limbic-systems/stargate/internal/ttlmap"
)

// Telemetry is the interface for all telemetry operations.
// Implemented by LiveTelemetry (enabled) and NoOpTelemetry (disabled).
type Telemetry interface {
	Shutdown(ctx context.Context) error
	StartClassifySpan(ctx context.Context) (context.Context, trace.Span)
	StartSpan(ctx context.Context, name string) (context.Context, trace.Span)
	LogClassification(ctx context.Context, result ClassifyResult)
	RecordClassification(decision, ruleLevel string, durationMs float64)
	RecordLLMCall(outcome string, durationMs float64)
	RecordParseError()
	RecordFeedback(outcome string)
	RecordCorpusHit(hitType string)
	RecordCorpusWrite(decision string)
	RecordScopeResolution(resolver, result string)
	SetRulesLoaded(level string, count int)
	SetCorpusEntries(decision string, count int)
	TraceIDFromContext(ctx context.Context) string
	// StartFeedbackSpan creates a new root span for feedback with a Link to
	// the original classification trace. If originalTraceID is empty, the span
	// is emitted without a Link.
	StartFeedbackSpan(ctx context.Context, originalTraceID string) (context.Context, trace.Span)
	// StoreToolUseTrace maps a tool_use_id to its stargate_trace_id for
	// feedback correlation. Queried by LookupToolUseTrace.
	StoreToolUseTrace(toolUseID, traceID string)
	// LookupToolUseTrace returns the stargate_trace_id for a tool_use_id,
	// or empty string if not found (caller falls back to trace file).
	LookupToolUseTrace(toolUseID string) string
}

// ClassifyResult holds the data needed for telemetry logging and metrics.
// Defined here to avoid circular imports with the classifier package.
type ClassifyResult struct {
	Decision         string // green/yellow/red
	Action           string // allow/deny/ask
	RuleLevel        string // matched rule tier
	RuleReason       string // matched rule reason
	TotalMs          float64
	LLMCalled        bool
	LLMDecision      string
	LLMDurationMs    float64
	CorpusPrecedents int
	ScopeResolved    string
	SessionID        string
	ScrubCommand     string // post-scrubbing command (may be empty)
	RequestCWD       string // per-request CWD from ClassifyRequest
}

// --- NoOpTelemetry ---

// NoOpTelemetry implements Telemetry with zero overhead when disabled.
type NoOpTelemetry struct{}

var _ Telemetry = (*NoOpTelemetry)(nil)

func (n *NoOpTelemetry) Shutdown(context.Context) error                    { return nil }
func (n *NoOpTelemetry) LogClassification(context.Context, ClassifyResult) {}
func (n *NoOpTelemetry) RecordClassification(string, string, float64)      {}
func (n *NoOpTelemetry) RecordLLMCall(string, float64)                     {}
func (n *NoOpTelemetry) RecordParseError()                                 {}
func (n *NoOpTelemetry) RecordFeedback(string)                             {}
func (n *NoOpTelemetry) RecordCorpusHit(string)                            {}
func (n *NoOpTelemetry) RecordCorpusWrite(string)                          {}
func (n *NoOpTelemetry) RecordScopeResolution(string, string)              {}
func (n *NoOpTelemetry) SetRulesLoaded(string, int)                        {}
func (n *NoOpTelemetry) SetCorpusEntries(string, int)                      {}
func (n *NoOpTelemetry) StoreToolUseTrace(string, string)                  {}
func (n *NoOpTelemetry) LookupToolUseTrace(string) string                  { return "" }

func (n *NoOpTelemetry) TraceIDFromContext(context.Context) string { return "" }

func (n *NoOpTelemetry) StartClassifySpan(ctx context.Context) (context.Context, trace.Span) {
	return ctx, nooptrace.Span{}
}

func (n *NoOpTelemetry) StartSpan(ctx context.Context, _ string) (context.Context, trace.Span) {
	return ctx, nooptrace.Span{}
}

func (n *NoOpTelemetry) StartFeedbackSpan(ctx context.Context, _ string) (context.Context, trace.Span) {
	return ctx, nooptrace.Span{}
}

// --- LiveTelemetry ---

// LiveTelemetry implements Telemetry with real OTel providers.
type LiveTelemetry struct {
	cfg            config.TelemetryConfig
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *sdklog.LoggerProvider
	tracer         trace.Tracer
	logger         otellog.Logger
	metrics        *metrics
	traceMap       *ttlmap.TTLMap[string, string]

	latitudeEnabled     bool
	latitudeCaptureName string
	latitudeTagsJSON    string
}

// Init creates a Telemetry instance. Returns NoOpTelemetry when both
// primary telemetry and Latitude are disabled.
func Init(cfg config.TelemetryConfig, latitudeCfg config.LatitudeConfig) (Telemetry, error) {
	if !cfg.Enabled && !latitudeCfg.Enabled {
		return &NoOpTelemetry{}, nil
	}

	// Note: STARGATE_OTEL_* env var overrides are not yet implemented.
	// When added, they should be resolved in config loading (config.Load),
	// not here — the telemetry package receives the final resolved config.

	// Warn on http:// with credentials.
	if cfg.Username != "" || cfg.Password != "" {
		if u, err := url.Parse(cfg.Endpoint); err == nil && u.Scheme == "http" {
			fmt.Fprintf(os.Stderr, "telemetry: WARNING: endpoint %q uses http:// with credentials — consider https://\n", cfg.Endpoint)
		}
	}

	// Build resource with service name.
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "stargate"
	}
	res := resource.NewWithAttributes(
		"",
		semconv.ServiceName(serviceName),
	)

	lt := &LiveTelemetry{cfg: cfg}

	// Build exporter options (shared auth) — only when primary telemetry is
	// enabled. When only Latitude is enabled, cfg.Endpoint may be empty and
	// the resulting opts would be nonsensical; they are never used because
	// cfg.ExportTraces/Metrics/Logs are all false.
	var expOpts exportOpts
	if cfg.Enabled {
		expOpts = buildExportOpts(cfg)
	}

	// TracerProvider — created when either primary traces or Latitude is enabled.
	needsTracer := cfg.ExportTraces || latitudeCfg.Enabled
	if needsTracer {
		var tpOpts []sdktrace.TracerProviderOption
		tpOpts = append(tpOpts,
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)

		// Primary trace exporter.
		if cfg.ExportTraces {
			traceExp, err := otlptracehttp.New(context.Background(), expOpts.trace...)
			if err != nil {
				return nil, fmt.Errorf("telemetry: creating trace exporter: %w", err)
			}
			tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExp))
		}

		// Latitude trace exporter.
		if latitudeCfg.Enabled {
			if u, _ := url.Parse(latitudeCfg.Endpoint); u != nil && u.Scheme == "http" {
				fmt.Fprintf(os.Stderr, "telemetry: WARNING: latitude endpoint %q uses http:// — API key will be transmitted in plaintext\n", latitudeCfg.Endpoint)
			}
			apiKey := os.Getenv("LATITUDE_API_KEY")
			if apiKey == "" {
				return nil, fmt.Errorf("telemetry: LATITUDE_API_KEY env var is required when latitude is enabled")
			}
			latitudeExp, err := otlptracehttp.New(context.Background(),
				otlptracehttp.WithEndpointURL(latitudeCfg.Endpoint),
				otlptracehttp.WithHeaders(map[string]string{
					"Authorization":      "Bearer " + apiKey,
					"X-Latitude-Project": latitudeCfg.ProjectSlug,
				}),
				otlptracehttp.WithTimeout(10*time.Second),
			)
			if err != nil {
				return nil, fmt.Errorf("telemetry: creating latitude exporter: %w", err)
			}
			tpOpts = append(tpOpts, sdktrace.WithBatcher(latitudeExp))

			lt.latitudeEnabled = true
			lt.latitudeCaptureName = latitudeCfg.CaptureName
			if len(latitudeCfg.Tags) > 0 {
				tagsJSON, err := json.Marshal(latitudeCfg.Tags)
				if err != nil {
					return nil, fmt.Errorf("telemetry: marshaling latitude tags: %w", err)
				}
				lt.latitudeTagsJSON = string(tagsJSON)
			}
		}

		lt.tracerProvider = sdktrace.NewTracerProvider(tpOpts...)
		otel.SetTracerProvider(lt.tracerProvider)
	}

	// MeterProvider — only when primary telemetry is enabled (expOpts requires it).
	if cfg.Enabled && cfg.ExportMetrics {
		metricExp, err := otlpmetrichttp.New(context.Background(), expOpts.metric...)
		if err != nil {
			return nil, fmt.Errorf("telemetry: creating metric exporter: %w", err)
		}
		lt.meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(lt.meterProvider)
	}

	// LoggerProvider — only when primary telemetry is enabled (expOpts requires it).
	if cfg.Enabled && cfg.ExportLogs {
		logExp, err := otlploghttp.New(context.Background(), expOpts.log...)
		if err != nil {
			return nil, fmt.Errorf("telemetry: creating log exporter: %w", err)
		}
		lt.loggerProvider = sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
			sdklog.WithResource(res),
		)
	}

	// Register metric instruments.
	if lt.meterProvider != nil {
		m := lt.meterProvider.Meter("stargate")
		mt, err := initMetrics(m)
		if err != nil {
			return nil, fmt.Errorf("telemetry: registering metrics: %w", err)
		}
		lt.metrics = mt
	}

	// Create tracer and logger instances.
	if lt.tracerProvider != nil {
		lt.tracer = lt.tracerProvider.Tracer("stargate")
	} else {
		lt.tracer = nooptrace.NewTracerProvider().Tracer("stargate")
	}

	if lt.loggerProvider != nil {
		lt.logger = lt.loggerProvider.Logger("stargate")
	} else {
		lt.logger = noop.NewLoggerProvider().Logger("stargate")
	}

	// In-memory tool_use_id → trace_id map.
	lt.traceMap = ttlmap.New[string, string](
		context.Background(),
		ttlmap.Options{MaxEntries: 10_000},
	)

	return lt, nil
}

// Shutdown flushes all providers sequentially:
// TracerProvider → TTLMap → MeterProvider → LoggerProvider.
// Errors are joined, not short-circuited.
func (lt *LiveTelemetry) Shutdown(ctx context.Context) error {
	var errs []error

	if lt.tracerProvider != nil {
		if err := lt.tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
		}
	}

	if lt.traceMap != nil {
		lt.traceMap.Close()
	}

	if lt.meterProvider != nil {
		if err := lt.meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter shutdown: %w", err))
		}
	}

	if lt.loggerProvider != nil {
		if err := lt.loggerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("logger shutdown: %w", err))
		}
	}

	return errors.Join(errs...)
}

// --- LiveTelemetry span, trace, and logging methods ---
// Metrics are in metrics.go, logging in logger.go. Span/trace methods below.

func (lt *LiveTelemetry) StartClassifySpan(ctx context.Context) (context.Context, trace.Span) {
	ctx, span := lt.tracer.Start(ctx, "stargate.classify")
	// WARNING: All span attributes are exported to Latitude (third-party SaaS)
	// when latitude.enabled = true. Do not add attributes containing command
	// content, secrets, or other sensitive data to classification spans.
	if lt.latitudeEnabled {
		span.SetAttributes(attribute.String("latitude.capture.name", lt.latitudeCaptureName))
		if lt.latitudeTagsJSON != "" {
			span.SetAttributes(attribute.String("latitude.tags", lt.latitudeTagsJSON))
		}
	}
	return ctx, span
}

func (lt *LiveTelemetry) StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return lt.tracer.Start(ctx, name)
}

func (lt *LiveTelemetry) StartFeedbackSpan(ctx context.Context, originalTraceID string) (context.Context, trace.Span) {
	var opts []trace.SpanStartOption
	var linked bool
	if originalTraceID != "" {
		tid, err := trace.TraceIDFromHex(originalTraceID)
		if err == nil {
			// SpanID must be non-zero for the link to be considered valid.
			var placeholderSpanID trace.SpanID
			placeholderSpanID[0] = 0x01
			link := trace.Link{
				SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
					TraceID:    tid,
					SpanID:     placeholderSpanID,
					TraceFlags: trace.FlagsSampled,
				}),
			}
			opts = append(opts, trace.WithLinks(link))
			linked = true
		}
	}
	opts = append(opts, trace.WithNewRoot())
	ctx, span := lt.tracer.Start(ctx, "stargate.feedback", opts...)
	if linked {
		span.SetAttributes(attribute.String("stargate.trace_id", originalTraceID))
	}
	return ctx, span
}

func (lt *LiveTelemetry) TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

func (lt *LiveTelemetry) StoreToolUseTrace(toolUseID, traceID string) {
	if lt.traceMap != nil {
		lt.traceMap.Set(toolUseID, traceID, 10*time.Minute)
	}
}

func (lt *LiveTelemetry) LookupToolUseTrace(toolUseID string) string {
	if lt.traceMap != nil {
		v, ok := lt.traceMap.Get(toolUseID)
		if ok {
			return v
		}
	}
	return ""
}

// exportOpts groups exporter options by signal type.
type exportOpts struct {
	trace  []otlptracehttp.Option
	metric []otlpmetrichttp.Option
	log    []otlploghttp.Option
}

// buildExportOpts creates shared exporter options from config.
func buildExportOpts(cfg config.TelemetryConfig) exportOpts {
	var opts exportOpts

	endpoint := cfg.Endpoint

	// Parse endpoint to separate host:port from path.
	// OTel HTTP exporters use WithEndpoint for host:port and WithURLPath for the path.
	u, err := url.Parse(endpoint)
	if err == nil && u.Host != "" {
		endpoint = u.Host

		if u.Scheme == "http" {
			opts.trace = append(opts.trace, otlptracehttp.WithInsecure())
			opts.metric = append(opts.metric, otlpmetrichttp.WithInsecure())
			opts.log = append(opts.log, otlploghttp.WithInsecure())
		}

		// Don't set WithURLPath — OTel exporters append signal-specific
		// paths (/v1/traces, /v1/metrics, /v1/logs) automatically.
	}

	opts.trace = append(opts.trace, otlptracehttp.WithEndpoint(endpoint))
	opts.metric = append(opts.metric, otlpmetrichttp.WithEndpoint(endpoint))
	opts.log = append(opts.log, otlploghttp.WithEndpoint(endpoint))

	if cfg.Username != "" || cfg.Password != "" {
		headers := map[string]string{
			"Authorization": "Basic " + basicAuth(cfg.Username, string(cfg.Password)),
		}
		opts.trace = append(opts.trace, otlptracehttp.WithHeaders(headers))
		opts.metric = append(opts.metric, otlpmetrichttp.WithHeaders(headers))
		opts.log = append(opts.log, otlploghttp.WithHeaders(headers))
	}

	return opts
}

// basicAuth returns the base64-encoded "username:password" for HTTP Basic Auth.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
