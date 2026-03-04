package telemetry

import "context"

// Score describes one Langfuse-compatible score event.
type Score struct {
	Name          string
	Value         float64
	Comment       string
	TraceID       string
	ObservationID string
	ConfigID      string
	DataType      string
}

// ScoreSink writes model/app scores to a telemetry backend.
type ScoreSink interface {
	Save(ctx context.Context, score Score)
}

// NoopScoreSink drops score events.
type NoopScoreSink struct{}

// Save is a no-op implementation for ScoreSink.
func (NoopScoreSink) Save(_ context.Context, _ Score) {}
