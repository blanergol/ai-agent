package telemetry

import (
	"context"

	"github.com/blanergol/agent-core/core"
)

// ContextBinder связывает session/meta данные запуска с telemetry-контекстом runtime.
type ContextBinder struct {
	Tracer    Tracer
	Metrics   Metrics
	Artifacts ArtifactSink
	Scores    ScoreSink
}

var _ core.ContextBinder = (*ContextBinder)(nil)

// NewContextBinder создает binder и подставляет noop-реализации для nil-зависимостей.
func NewContextBinder(tracer Tracer, metrics Metrics, artifacts ArtifactSink, scores ScoreSink) *ContextBinder {
	if tracer == nil {
		tracer = NoopTracer{}
	}
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	if artifacts == nil {
		artifacts = NoopArtifactSink{}
	}
	if scores == nil {
		scores = NoopScoreSink{}
	}
	return &ContextBinder{
		Tracer:    tracer,
		Metrics:   metrics,
		Artifacts: artifacts,
		Scores:    scores,
	}
}

// Ensure нормализует RunMeta и гарантирует наличие session/correlation идентификаторов.
func (b *ContextBinder) Ensure(meta core.RunMeta) core.RunMeta {
	session := EnsureSession(SessionInfo{
		SessionID:     meta.SessionID,
		CorrelationID: meta.CorrelationID,
		UserSub:       meta.UserSub,
	})
	return core.RunMeta{
		SessionID:     session.SessionID,
		CorrelationID: session.CorrelationID,
		UserSub:       session.UserSub,
	}
}

// Bind добавляет в context данные сессии и runtime-telemetry зависимости.
func (b *ContextBinder) Bind(ctx context.Context, meta core.RunMeta) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = WithSession(ctx, SessionInfo{
		SessionID:     meta.SessionID,
		CorrelationID: meta.CorrelationID,
		UserSub:       meta.UserSub,
	})
	ctx = BindRuntimeWithArtifacts(ctx, b.Tracer, b.Metrics, b.Artifacts)
	ctx = WithScores(ctx, b.Scores)
	return ctx
}
