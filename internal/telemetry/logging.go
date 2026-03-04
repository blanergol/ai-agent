package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
)

// ContextAttrs возвращает стандартный набор полей корреляции из context.
func ContextAttrs(ctx context.Context) []slog.Attr {
	session := SessionFromContext(ctx)
	attrs := []slog.Attr{
		slog.String("session_id", session.SessionID),
		slog.String("correlation_id", session.CorrelationID),
	}
	if userSubHash := UserSubForLogs(session.UserSub); userSubHash != "" {
		attrs = append(attrs, slog.String("user_sub_hash", userSubHash))
	}
	return attrs
}

// ContextLogger гарантирует сквозное обогащение логов correlation-полями из context.
type ContextLogger struct {
	ctx context.Context
	log *slog.Logger
}

// NewContextLogger создаёт логгер-обёртку, автоматически добавляющую session/correlation поля.
func NewContextLogger(ctx context.Context, logger *slog.Logger) ContextLogger {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return ContextLogger{ctx: ctx, log: logger}
}

// LogAttrs пишет сообщение с обязательными correlation attrs из context.
func (l ContextLogger) LogAttrs(level slog.Level, msg string, attrs ...slog.Attr) {
	base := ContextAttrs(l.ctx)
	merged := make([]slog.Attr, 0, len(base)+len(attrs))
	merged = append(merged, base...)
	merged = append(merged, attrs...)
	l.log.LogAttrs(l.ctx, level, msg, merged...)
}

// Debug пишет debug-сообщение с обязательными correlation attrs.
func (l ContextLogger) Debug(msg string, attrs ...slog.Attr) {
	l.LogAttrs(slog.LevelDebug, msg, attrs...)
}

// Info пишет info-сообщение с обязательными correlation attrs.
func (l ContextLogger) Info(msg string, attrs ...slog.Attr) {
	l.LogAttrs(slog.LevelInfo, msg, attrs...)
}

// Warn пишет warn-сообщение с обязательными correlation attrs.
func (l ContextLogger) Warn(msg string, attrs ...slog.Attr) {
	l.LogAttrs(slog.LevelWarn, msg, attrs...)
}

// Error пишет error-сообщение с обязательными correlation attrs.
func (l ContextLogger) Error(msg string, attrs ...slog.Attr) {
	l.LogAttrs(slog.LevelError, msg, attrs...)
}

// UserSubForLogs возвращает необратимый хэш user-sub для безопасного логирования.
func UserSubForLogs(userSub string) string {
	userSub = strings.TrimSpace(userSub)
	if userSub == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(userSub))
	return "sha256:" + hex.EncodeToString(sum[:8])
}
