package state

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/internal/telemetry"
)

const (
	// sessionPrefix отделяет пространство ключей сессионного состояния.
	sessionPrefix = "session"
)

// NamespacedKey добавляет session namespace к пользовательскому ключу state.
func NamespacedKey(ctx context.Context, key string) string {
	session := telemetry.SessionFromContext(ctx)
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return sessionPrefix + ":" + session.SessionID + ":"
	}
	return sessionPrefix + ":" + session.SessionID + ":" + cleanKey
}

// SnapshotForSession фильтрует общий snapshot и оставляет только ключи текущей сессии.
func SnapshotForSession(ctx context.Context, all map[string]any) map[string]any {
	out := make(map[string]any)
	session := telemetry.SessionFromContext(ctx)
	prefix := sessionPrefix + ":" + session.SessionID + ":"
	for key, value := range all {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out[strings.TrimPrefix(key, prefix)] = value
	}
	return out
}
