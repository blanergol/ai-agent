package telemetry

import (
	"context"
	"errors"
)

// CombineTracers объединяет несколько tracer-реализаций в один общий tracer.
func CombineTracers(tracers ...Tracer) Tracer {
	filtered := make([]Tracer, 0, len(tracers))
	for _, tracer := range tracers {
		if tracer == nil {
			continue
		}
		filtered = append(filtered, tracer)
	}
	switch len(filtered) {
	case 0:
		return NoopTracer{}
	case 1:
		return filtered[0]
	default:
		return multiTracer{items: filtered}
	}
}

// multiTracer последовательно запускает каждый tracer и объединяет полученные span-ы.
type multiTracer struct {
	items []Tracer
}

// Start запускает одноименный span во всех дочерних tracer-ах.
func (t multiTracer) Start(ctx context.Context, name string, attrs map[string]any) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	spans := make([]Span, 0, len(t.items))
	for _, item := range t.items {
		var span Span
		ctx, span = item.Start(ctx, name, attrs)
		if span == nil {
			continue
		}
		spans = append(spans, span)
	}
	switch len(spans) {
	case 0:
		return ctx, noopSpan{}
	case 1:
		return ctx, spans[0]
	default:
		return ctx, multiSpan{items: spans}
	}
}

// multiSpan проксирует события и завершение сразу в несколько span-ов.
type multiSpan struct {
	items []Span
}

// AddEvent добавляет событие во все span-ы композиции.
func (s multiSpan) AddEvent(name string, attrs map[string]any) {
	for _, item := range s.items {
		item.AddEvent(name, attrs)
	}
}

// End завершает все span-ы композиции, передавая общую ошибку.
func (s multiSpan) End(err error) {
	for _, item := range s.items {
		item.End(err)
	}
}

// CombineArtifactSinks объединяет несколько sink-ов артефактов в один.
func CombineArtifactSinks(sinks ...ArtifactSink) ArtifactSink {
	filtered := make([]ArtifactSink, 0, len(sinks))
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		filtered = append(filtered, sink)
	}
	switch len(filtered) {
	case 0:
		return NoopArtifactSink{}
	case 1:
		return filtered[0]
	default:
		return multiArtifactSink{items: filtered}
	}
}

// multiArtifactSink проксирует сохранение артефактов в несколько целевых sink-ов.
type multiArtifactSink struct {
	items []ArtifactSink
}

// Save сохраняет артефакт во все sink-и композиции.
func (s multiArtifactSink) Save(ctx context.Context, artifact Artifact) {
	for _, item := range s.items {
		item.Save(ctx, artifact)
	}
}

// CombineScoreSinks объединяет несколько score-sink-ов в один агрегированный sink.
func CombineScoreSinks(sinks ...ScoreSink) ScoreSink {
	filtered := make([]ScoreSink, 0, len(sinks))
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		filtered = append(filtered, sink)
	}
	switch len(filtered) {
	case 0:
		return NoopScoreSink{}
	case 1:
		return filtered[0]
	default:
		return multiScoreSink{items: filtered}
	}
}

// multiScoreSink проксирует запись score-событий в несколько sink-ов.
type multiScoreSink struct {
	items []ScoreSink
}

// Save записывает score во все дочерние sink-и.
func (s multiScoreSink) Save(ctx context.Context, score Score) {
	for _, item := range s.items {
		item.Save(ctx, score)
	}
}

// JoinShutdownFuncs объединяет несколько shutdown-функций в одну с накоплением ошибок.
func JoinShutdownFuncs(funcs ...func(context.Context) error) func(context.Context) error {
	filtered := make([]func(context.Context) error, 0, len(funcs))
	for _, fn := range funcs {
		if fn == nil {
			continue
		}
		filtered = append(filtered, fn)
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(ctx context.Context) error {
		var joined error
		for _, fn := range filtered {
			if err := fn(ctx); err != nil {
				joined = errors.Join(joined, err)
			}
		}
		return joined
	}
}
