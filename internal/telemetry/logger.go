package telemetry

import (
	"context"
	"strings"

	otellog "go.opentelemetry.io/otel/log"
)

// severityFromDecision maps classification decisions to OTel log severity.
// GREEN → Info, YELLOW → Warn, RED → Error.
func severityFromDecision(decision string) otellog.Severity {
	switch strings.ToLower(decision) {
	case "green":
		return otellog.SeverityInfo
	case "yellow":
		return otellog.SeverityWarn
	case "red":
		return otellog.SeverityError
	default:
		return otellog.SeverityInfo
	}
}

// truncateBytes truncates s to at most maxBytes without splitting a
// multi-byte UTF-8 rune. If the cut point falls in the middle of a
// rune, it backs up to the previous rune boundary.
func truncateBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Back up from maxBytes to avoid splitting a multi-byte rune.
	for maxBytes > 0 && maxBytes < len(s) && s[maxBytes]>>6 == 0b10 {
		maxBytes--
	}
	return s[:maxBytes]
}

// LogClassification emits a structured OTel log record with classification attributes.
func (lt *LiveTelemetry) LogClassification(ctx context.Context, result ClassifyResult) {
	if lt.logger == nil {
		return
	}

	var record otellog.Record
	record.SetSeverity(severityFromDecision(result.Decision))
	record.SetBody(otellog.StringValue("classification"))

	attrs := []otellog.KeyValue{
		otellog.String("stargate.decision", result.Decision),
		otellog.String("stargate.action", result.Action),
		otellog.String("stargate.rule.level", result.RuleLevel),
		otellog.String("stargate.rule.reason", result.RuleReason),
		otellog.Float64("stargate.total_ms", result.TotalMs),
		otellog.Bool("stargate.llm.called", result.LLMCalled),
		otellog.Int("stargate.corpus.precedents", result.CorpusPrecedents),
		otellog.String("stargate.session_id", result.SessionID),
	}

	// Conditional LLM attributes.
	if result.LLMCalled {
		attrs = append(attrs,
			otellog.String("stargate.llm.decision", result.LLMDecision),
			otellog.Float64("stargate.llm.duration_ms", result.LLMDurationMs),
		)
	}

	// Scope resolver output — always included, truncated to 256 bytes.
	if result.ScopeResolved != "" {
		attrs = append(attrs,
			otellog.String("stargate.scope.resolved", truncateBytes(result.ScopeResolved, 256)),
		)
	}

	// Scrubbed command and CWD — only when include_scrubbed_command is true.
	if lt.cfg.IncludeScrubCommand {
		if result.ScrubCommand != "" {
			attrs = append(attrs, otellog.String("stargate.scrubbed_command", result.ScrubCommand))
		}
		if result.RequestCWD != "" {
			attrs = append(attrs, otellog.String("stargate.request_cwd", result.RequestCWD))
		}
	}

	record.AddAttributes(attrs...)
	lt.logger.Emit(ctx, record)
}
