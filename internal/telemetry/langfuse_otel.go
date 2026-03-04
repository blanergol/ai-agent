package telemetry

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/blanergol/agent-core/internal/redact"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// LangfuseOTLPConfig configures OTLP exporter wiring for Langfuse tracing.
type LangfuseOTLPConfig struct {
	Host             string
	PublicKey        string
	SecretKey        string
	ServiceName      string
	ServiceVersion   string
	Environment      string
	RequestTimeout   time.Duration
	MaxArtifactChars int
}

// LangfuseBackend bundles telemetry interfaces backed by Langfuse OTLP.
type LangfuseBackend struct {
	Tracer    Tracer
	Artifacts ArtifactSink
	Scores    ScoreSink
	Shutdown  func(context.Context) error
}

// NewLangfuseBackend initializes OTLP exporter + tracer for Langfuse.
func NewLangfuseBackend(cfg LangfuseOTLPConfig, logger *slog.Logger) (*LangfuseBackend, error) {
	host := strings.TrimSpace(cfg.Host)
	publicKey := strings.TrimSpace(cfg.PublicKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if host == "" {
		return nil, fmt.Errorf("langfuse host is empty")
	}
	if publicKey == "" || secretKey == "" {
		return nil, fmt.Errorf("langfuse credentials are empty")
	}

	parsedURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse langfuse host: %w", err)
	}
	if strings.TrimSpace(parsedURL.Scheme) == "" || strings.TrimSpace(parsedURL.Host) == "" {
		return nil, fmt.Errorf("invalid langfuse host: %s", host)
	}

	otlpPath := "/api/public/otel/v1/traces"
	if strings.TrimSpace(parsedURL.Path) != "" && parsedURL.Path != "/" {
		otlpPath = strings.TrimRight(parsedURL.Path, "/") + otlpPath
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(parsedURL.Host),
		otlptracehttp.WithURLPath(otlpPath),
		otlptracehttp.WithHeaders(map[string]string{
			"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(publicKey+":"+secretKey)),
		}),
	}
	if strings.EqualFold(parsedURL.Scheme, "http") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if cfg.RequestTimeout > 0 {
		opts = append(opts, otlptracehttp.WithTimeout(cfg.RequestTimeout))
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("init langfuse otlp exporter: %w", err)
	}

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "agent-core"
	}
	environment := strings.TrimSpace(cfg.Environment)
	if environment == "" {
		environment = "dev"
	}
	attrs := []attribute.KeyValue{
		attribute.String("service.name", serviceName),
		attribute.String("deployment.environment", environment),
	}
	if v := strings.TrimSpace(cfg.ServiceVersion); v != "" {
		attrs = append(attrs, attribute.String("service.version", v))
	}

	resource, err := sdkresource.Merge(
		sdkresource.Default(),
		sdkresource.NewWithAttributes("", attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("init telemetry resource: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource),
		sdktrace.WithBatcher(exporter),
	)
	otelTracer := tracerProvider.Tracer("agent-core/runtime")

	maxArtifactChars := cfg.MaxArtifactChars
	if maxArtifactChars <= 0 {
		maxArtifactChars = 2000
	}
	scoreSink, err := newLangfuseScoreSink(
		host,
		publicKey,
		secretKey,
		cfg.RequestTimeout,
		logger,
	)
	if err != nil {
		return nil, err
	}
	return &LangfuseBackend{
		Tracer:    &OTelTracer{tracer: otelTracer, logger: logger},
		Artifacts: NewOTelArtifactSink(maxArtifactChars),
		Scores:    scoreSink,
		Shutdown:  tracerProvider.Shutdown,
	}, nil
}

// OTelTracer adapts OpenTelemetry tracer to the local Tracer contract.
type OTelTracer struct {
	tracer oteltrace.Tracer
	logger *slog.Logger
}

func (t *OTelTracer) Start(ctx context.Context, name string, attrs map[string]any) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil || t.tracer == nil {
		return ctx, noopSpan{}
	}

	session := SessionFromContext(ctx)
	otelAttrs := append(otelAttrsFromMap(attrs), sessionOTelAttrs(session)...)

	ctx, span := t.tracer.Start(ctx, name, oteltrace.WithAttributes(otelAttrs...))
	return ctx, &otelSpan{span: span}
}

func sessionOTelAttrs(session SessionInfo) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("agent.session_id", session.SessionID),
		attribute.String("agent.correlation_id", session.CorrelationID),
		attribute.String("langfuse.session.id", session.SessionID),
	}
	userSub := strings.TrimSpace(session.UserSub)
	if userSub != "" {
		attrs = append(attrs, attribute.String("langfuse.user.id", userSub))
		if userSubHash := UserSubForLogs(userSub); userSubHash != "" {
			attrs = append(attrs, attribute.String("agent.user_sub_hash", userSubHash))
		}
	}
	return attrs
}

type otelSpan struct {
	span oteltrace.Span
}

func (s *otelSpan) AddEvent(name string, attrs map[string]any) {
	if s == nil || s.span == nil {
		return
	}
	s.span.AddEvent(name, oteltrace.WithAttributes(otelAttrsFromMap(attrs)...))
}

func (s *otelSpan) End(err error) {
	if s == nil || s.span == nil {
		return
	}
	if err != nil {
		s.span.RecordError(err)
		s.span.SetStatus(codes.Error, redact.Error(err))
	} else {
		s.span.SetStatus(codes.Ok, "")
	}
	s.span.End()
}

// OTelArtifactSink records prompt/response/state artifacts as span events.
type OTelArtifactSink struct {
	maxPayloadChars int
}

// NewOTelArtifactSink initializes an artifact sink backed by active OTel span.
func NewOTelArtifactSink(maxPayloadChars int) *OTelArtifactSink {
	if maxPayloadChars <= 0 {
		maxPayloadChars = 2000
	}
	return &OTelArtifactSink{maxPayloadChars: maxPayloadChars}
}

func (s *OTelArtifactSink) Save(ctx context.Context, artifact Artifact) {
	if s == nil {
		return
	}
	span := oteltrace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}

	session := SessionFromContext(ctx)
	if strings.TrimSpace(artifact.SessionID) == "" {
		artifact.SessionID = session.SessionID
	}
	if strings.TrimSpace(artifact.CorrelationID) == "" {
		artifact.CorrelationID = session.CorrelationID
	}
	if artifact.Timestamp.IsZero() {
		artifact.Timestamp = time.Now().UTC()
	}

	payload := redact.Text(artifact.Payload)
	if len(payload) > s.maxPayloadChars {
		payload = payload[:s.maxPayloadChars]
	}
	s.applyLangfuseSemanticAttrs(span, artifact, payload)

	span.AddEvent(
		"artifact."+string(artifact.Kind),
		oteltrace.WithTimestamp(artifact.Timestamp),
		oteltrace.WithAttributes(
			attribute.String("artifact.kind", string(artifact.Kind)),
			attribute.String("artifact.name", artifact.Name),
			attribute.String("artifact.payload", payload),
			attribute.String("artifact.session_id", artifact.SessionID),
			attribute.String("artifact.correlation_id", artifact.CorrelationID),
		),
	)
}

func (s *OTelArtifactSink) applyLangfuseSemanticAttrs(span oteltrace.Span, artifact Artifact, payload string) {
	if span == nil || !span.IsRecording() || strings.TrimSpace(payload) == "" {
		return
	}
	switch artifact.Kind {
	case ArtifactPrompt:
		span.SetAttributes(attribute.String("langfuse.observation.input", payload))
		if artifact.Name == "agent.user_input" {
			span.SetAttributes(attribute.String("langfuse.trace.input", payload))
		}
	case ArtifactResponse:
		span.SetAttributes(attribute.String("langfuse.observation.output", payload))
		if artifact.Name == "agent.final_response" {
			span.SetAttributes(attribute.String("langfuse.trace.output", payload))
		}
	case ArtifactState:
		span.SetAttributes(attribute.String("langfuse.observation.output", payload))
		if artifact.Name == "agent.run_result" {
			span.SetAttributes(attribute.String("langfuse.trace.output", payload))
		}
	}
}

func otelAttrsFromMap(values map[string]any) []attribute.KeyValue {
	if len(values) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		switch v := value.(type) {
		case string:
			attrs = append(attrs, attribute.String(key, v))
		case bool:
			attrs = append(attrs, attribute.Bool(key, v))
		case int:
			attrs = append(attrs, attribute.Int(key, v))
		case int8:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case int16:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case int32:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case int64:
			attrs = append(attrs, attribute.Int64(key, v))
		case uint:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case uint8:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case uint16:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case uint32:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case uint64:
			if v > uint64(^uint64(0)>>1) {
				attrs = append(attrs, attribute.String(key, fmt.Sprintf("%d", v)))
			} else {
				attrs = append(attrs, attribute.Int64(key, int64(v)))
			}
		case float32:
			attrs = append(attrs, attribute.Float64(key, float64(v)))
		case float64:
			attrs = append(attrs, attribute.Float64(key, v))
		case time.Time:
			attrs = append(attrs, attribute.String(key, v.UTC().Format(time.RFC3339Nano)))
		case time.Duration:
			attrs = append(attrs, attribute.String(key, v.String()))
		case error:
			attrs = append(attrs, attribute.String(key, redact.Error(v)))
		default:
			attrs = append(attrs, attribute.String(key, redact.Text(fmt.Sprintf("%v", v))))
		}
	}
	return attrs
}
