package state

import "strings"

// Базовый префикс пространства ключей для хранения данных в рамках сессии.
const (
	// sessionPrefix фиксирует пространство ключей для session-scoped значений.
	sessionPrefix = "session"
)

// NamespacedKey добавляет namespace сессии к пользовательскому ключу хранилища.
func NamespacedKey(sessionID, key string) string {
	cleanSession := strings.TrimSpace(sessionID)
	if cleanSession == "" {
		cleanSession = "default"
	}
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return sessionPrefix + ":" + cleanSession + ":"
	}
	return sessionPrefix + ":" + cleanSession + ":" + cleanKey
}

// SnapshotForSession фильтрует глобальный snapshot и оставляет значения только текущей сессии.
func SnapshotForSession(sessionID string, all map[string]any) map[string]any {
	out := make(map[string]any)
	cleanSession := strings.TrimSpace(sessionID)
	if cleanSession == "" {
		cleanSession = "default"
	}
	prefix := sessionPrefix + ":" + cleanSession + ":"
	for key, value := range all {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out[strings.TrimPrefix(key, prefix)] = value
	}
	return out
}
