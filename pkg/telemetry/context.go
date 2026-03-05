package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
)

// sessionKey служит ключом context.Context для SessionInfo.
type sessionKey struct{}

// tracerKey служит ключом context.Context для Tracer.
type tracerKey struct{}

// metricsKey служит ключом context.Context для Metrics.
type metricsKey struct{}

// artifactKey служит ключом context.Context для ArtifactSink.
type artifactKey struct{}

// scoreKey служит ключом context.Context для ScoreSink.
type scoreKey struct{}

// fallbackSessionScopeSeq генерирует уникальные fallback scope при отсутствии session_id.
var fallbackSessionScopeSeq uint64

// SessionInfo хранит минимальный контекст корреляции для одного запуска агента.
type SessionInfo struct {
	// SessionID связывает все события одной логической сессии/диалога.
	SessionID string
	// CorrelationID связывает события конкретного запроса внутри сессии.
	CorrelationID string
	// UserSub хранит идентификатор субъекта аутентификации.
	UserSub string
}

// EnsureSession гарантирует, что у сессии есть стабильные идентификаторы.
func EnsureSession(info SessionInfo) SessionInfo {
	if strings.TrimSpace(info.SessionID) == "" {
		info.SessionID = newID()
	}
	if strings.TrimSpace(info.CorrelationID) == "" {
		info.CorrelationID = newID()
	}
	info.UserSub = strings.TrimSpace(info.UserSub)
	return info
}

// WithSession добавляет session/correlation контекст в стандартный context.Context.
func WithSession(ctx context.Context, info SessionInfo) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionKey{}, EnsureSession(info))
}

// SessionFromContext возвращает session/correlation контекст или безопасные дефолты.
func SessionFromContext(ctx context.Context) SessionInfo {
	if ctx != nil {
		if v, ok := ctx.Value(sessionKey{}).(SessionInfo); ok {
			return EnsureSession(v)
		}
	}
	return EnsureSession(SessionInfo{})
}

// SessionScopeFromContext возвращает стабильный scope для session-sensitive операций.
// Если session не задана явно, возвращается "global", чтобы не генерировать новый ID
// на каждом вызове в пределах одного и того же контекста.
func SessionScopeFromContext(ctx context.Context) string {
	if ctx != nil {
		if v, ok := ctx.Value(sessionKey{}).(SessionInfo); ok {
			if id := strings.TrimSpace(v.SessionID); id != "" {
				return id
			}
		}
	}
	return fmt.Sprintf("missing-session-%d", atomic.AddUint64(&fallbackSessionScopeSeq, 1))
}

// WithTracer связывает tracer с контекстом выполнения.
func WithTracer(ctx context.Context, tracer Tracer) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracer == nil {
		tracer = NoopTracer{}
	}
	return context.WithValue(ctx, tracerKey{}, tracer)
}

// TracerFromContext возвращает tracer из контекста или noop-реализацию.
func TracerFromContext(ctx context.Context) Tracer {
	if ctx != nil {
		if v, ok := ctx.Value(tracerKey{}).(Tracer); ok && v != nil {
			return v
		}
	}
	return NoopTracer{}
}

// WithMetrics связывает metrics-коллектор с контекстом выполнения.
func WithMetrics(ctx context.Context, metrics Metrics) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	return context.WithValue(ctx, metricsKey{}, metrics)
}

// MetricsFromContext возвращает metrics-коллектор из контекста или noop-реализацию.
func MetricsFromContext(ctx context.Context) Metrics {
	if ctx != nil {
		if v, ok := ctx.Value(metricsKey{}).(Metrics); ok && v != nil {
			return v
		}
	}
	return NoopMetrics{}
}

// WithArtifacts связывает debug artifact sink с контекстом выполнения.
func WithArtifacts(ctx context.Context, sink ArtifactSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil {
		sink = NoopArtifactSink{}
	}
	return context.WithValue(ctx, artifactKey{}, sink)
}

// ArtifactsFromContext возвращает artifact sink из контекста или noop-реализацию.
func ArtifactsFromContext(ctx context.Context) ArtifactSink {
	if ctx != nil {
		if v, ok := ctx.Value(artifactKey{}).(ArtifactSink); ok && v != nil {
			return v
		}
	}
	return NoopArtifactSink{}
}

// BindRuntime добавляет tracer/metrics в контекст единым вызовом.
func BindRuntime(ctx context.Context, tracer Tracer, metrics Metrics) context.Context {
	return BindRuntimeWithArtifacts(ctx, tracer, metrics, NoopArtifactSink{})
}

// BindRuntimeWithArtifacts добавляет tracer/metrics/artifact sink в контекст единым вызовом.
func BindRuntimeWithArtifacts(ctx context.Context, tracer Tracer, metrics Metrics, artifacts ArtifactSink) context.Context {
	ctx = WithTracer(ctx, tracer)
	ctx = WithMetrics(ctx, metrics)
	ctx = WithArtifacts(ctx, artifacts)
	return ctx
}

// WithScores связывает score sink с контекстом выполнения.
func WithScores(ctx context.Context, sink ScoreSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil {
		sink = NoopScoreSink{}
	}
	return context.WithValue(ctx, scoreKey{}, sink)
}

// ScoresFromContext возвращает score sink из контекста или noop-реализацию.
func ScoresFromContext(ctx context.Context) ScoreSink {
	if ctx != nil {
		if v, ok := ctx.Value(scoreKey{}).(ScoreSink); ok && v != nil {
			return v
		}
	}
	return NoopScoreSink{}
}

// newID генерирует короткий идентификатор для session/correlation полей.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "fallback-session-id"
	}
	return hex.EncodeToString(b[:])
}
