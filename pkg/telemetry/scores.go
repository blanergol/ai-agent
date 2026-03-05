package telemetry

import "context"

// Score описывает одно событие оценки (score), совместимое с Langfuse.
type Score struct {
	Name          string
	Value         float64
	Comment       string
	TraceID       string
	ObservationID string
	ConfigID      string
	DataType      string
}

// ScoreSink отправляет оценки модели/приложения в telemetry backend.
type ScoreSink interface {
	Save(ctx context.Context, score Score)
}

// NoopScoreSink игнорирует все score-события.
type NoopScoreSink struct{}

// Save в noop-реализации не выполняет никаких действий.
func (NoopScoreSink) Save(_ context.Context, _ Score) {}
