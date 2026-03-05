package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/blanergol/agent-core/pkg/redact"
)

// Span представляет единицу трассировки внутри одного шага выполнения.
type Span interface {
	// AddEvent добавляет локальное событие в span.
	AddEvent(name string, attrs map[string]any)
	// End закрывает span и фиксирует итоговый статус.
	End(err error)
}

// Tracer создаёт spans для ключевых участков пайплайна агента.
type Tracer interface {
	// Start открывает новый span и возвращает контекст для вложенных операций.
	Start(ctx context.Context, name string, attrs map[string]any) (context.Context, Span)
}

// Metrics описывает минимальный API для счётчиков и латентностей.
type Metrics interface {
	// IncCounter увеличивает именованный счётчик на фиксированное значение.
	IncCounter(name string, delta int64, tags map[string]string)
	// ObserveHistogram записывает измерение в гистограмму.
	ObserveHistogram(name string, value float64, tags map[string]string)
}

// NoopTracer предоставляет безопасную пустую реализацию tracer.
type NoopTracer struct{}

// Start возвращает noop-span без побочных эффектов.
func (NoopTracer) Start(ctx context.Context, _ string, _ map[string]any) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, noopSpan{}
}

// NoopMetrics предоставляет пустую реализацию collection API.
type NoopMetrics struct{}

// IncCounter игнорирует инкремент в noop-режиме.
func (NoopMetrics) IncCounter(_ string, _ int64, _ map[string]string) {}

// ObserveHistogram игнорирует наблюдение в noop-режиме.
func (NoopMetrics) ObserveHistogram(_ string, _ float64, _ map[string]string) {}

// noopSpan реализует Span без побочных эффектов для noop-трассировки.
type noopSpan struct{}

// AddEvent в noop-span не выполняет действий.
func (noopSpan) AddEvent(_ string, _ map[string]any) {}

// End в noop-span не выполняет действий.
func (noopSpan) End(_ error) {}

// LoggerTracer пишет span-события в structured logs.
type LoggerTracer struct {
	// log используется для фиксации start/end span событий.
	log *slog.Logger
}

// NewLoggerTracer создаёт tracer, который пишет в slog.
func NewLoggerTracer(logger *slog.Logger) *LoggerTracer {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggerTracer{log: logger}
}

// Start открывает span и пишет событие старта в debug-лог.
func (t *LoggerTracer) Start(ctx context.Context, name string, attrs map[string]any) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	session := SessionFromContext(ctx)
	NewContextLogger(ctx, t.log).Debug(
		"span_start",
		slog.String("span", name),
		slog.Any("attrs", attrs),
	)
	return ctx, &loggerSpan{
		name:       name,
		start:      time.Now(),
		log:        t.log,
		session:    session,
		startAttrs: attrs,
	}
}

// loggerSpan хранит состояние span, который логируется через slog.
type loggerSpan struct {
	// name хранит техническое имя span для трассировки.
	name string
	// start фиксирует время начала span.
	start time.Time
	// log пишет события span в structured-формате.
	log *slog.Logger
	// session связывает span с контекстом сессии/запроса.
	session SessionInfo
	// startAttrs хранит атрибуты старта span.
	startAttrs map[string]any
}

// AddEvent фиксирует событие внутри span.
func (s *loggerSpan) AddEvent(name string, attrs map[string]any) {
	ctx := WithSession(context.Background(), s.session)
	NewContextLogger(ctx, s.log).Debug(
		"span_event",
		slog.String("span", s.name),
		slog.String("event", name),
		slog.Any("attrs", attrs),
	)
}

// End завершает span и пишет итоговую длительность/статус.
func (s *loggerSpan) End(err error) {
	level := slog.LevelDebug
	if err != nil {
		level = slog.LevelWarn
	}
	ctx := WithSession(context.Background(), s.session)
	NewContextLogger(ctx, s.log).LogAttrs(
		level,
		"span_end",
		slog.String("span", s.name),
		slog.Duration("duration", time.Since(s.start)),
		slog.Bool("success", err == nil),
		slog.Any("start_attrs", s.startAttrs),
		slog.String("error", errorText(err)),
	)
}

// errorText возвращает безопасное текстовое представление ошибки для логов span.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return redact.Error(err)
}
