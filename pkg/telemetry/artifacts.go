package telemetry

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/redact"
)

// ArtifactKind классифицирует тип debug-артефакта выполнения.
type ArtifactKind string

// Поддерживаемые типы артефактов, которые записываются в telemetry.
const (
	// ArtifactPrompt фиксирует входной prompt/наблюдение перед обращением к модели.
	ArtifactPrompt ArtifactKind = "prompt"
	// ArtifactResponse фиксирует ответ модели/инструмента.
	ArtifactResponse ArtifactKind = "response"
	// ArtifactState фиксирует важные изменения состояния/плана.
	ArtifactState ArtifactKind = "state"
)

// Artifact представляет безопасный debug-артефакт исполнения.
type Artifact struct {
	// Timestamp фиксирует время записи артефакта.
	Timestamp time.Time
	// SessionID связывает артефакт с диалоговой сессией.
	SessionID string
	// CorrelationID связывает артефакт с конкретным запросом.
	CorrelationID string
	// Kind задаёт класс артефакта.
	Kind ArtifactKind
	// Name задаёт техническое имя/этап.
	Name string
	// Payload содержит полезные данные, подлежащие redaction и усечению.
	Payload string
}

// ArtifactSink описывает канал сохранения debug-артефактов.
type ArtifactSink interface {
	// Save сохраняет артефакт в выбранный backend.
	Save(ctx context.Context, artifact Artifact)
}

// NoopArtifactSink игнорирует запись артефактов.
type NoopArtifactSink struct{}

// Save в noop-режиме не делает действий.
func (NoopArtifactSink) Save(_ context.Context, _ Artifact) {}

// LoggerArtifactSink пишет артефакты в structured debug-лог.
type LoggerArtifactSink struct {
	// log выводит артефакты в JSON/structured формат.
	log *slog.Logger
	// maxPayloadChars ограничивает размер payload.
	maxPayloadChars int
}

// NewLoggerArtifactSink создаёт логирующий sink c безопасными лимитами.
func NewLoggerArtifactSink(logger *slog.Logger, maxPayloadChars int) *LoggerArtifactSink {
	if logger == nil {
		logger = slog.Default()
	}
	if maxPayloadChars <= 0 {
		maxPayloadChars = 2000
	}
	return &LoggerArtifactSink{log: logger, maxPayloadChars: maxPayloadChars}
}

// Save пишет артефакт в debug-лог после redaction и усечения payload.
func (s *LoggerArtifactSink) Save(ctx context.Context, artifact Artifact) {
	if s == nil || s.log == nil {
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
	NewContextLogger(WithSession(ctx, SessionInfo{
		SessionID:     artifact.SessionID,
		CorrelationID: artifact.CorrelationID,
	}), s.log).Debug("debug_artifact",
		slog.String("kind", string(artifact.Kind)),
		slog.String("name", artifact.Name),
		slog.Time("ts", artifact.Timestamp),
		slog.String("payload", payload),
	)
}
