package state

import (
	"context"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/telemetry"
)

// SessionSnapshotter адаптирует state.Store к контракту core.StateSnapshotter.
type SessionSnapshotter struct {
	Store Store
}

var _ core.StateSnapshotter = SessionSnapshotter{}

// NewSessionSnapshotter создает адаптер, возвращающий snapshot только текущей сессии.
func NewSessionSnapshotter(store Store) SessionSnapshotter {
	return SessionSnapshotter{Store: store}
}

// SnapshotForSession читает полный snapshot и фильтрует его по session-id из контекста.
func (s SessionSnapshotter) SnapshotForSession(ctx context.Context) map[string]any {
	if s.Store == nil {
		return nil
	}
	sessionID := telemetry.SessionFromContext(ctx).SessionID
	return SnapshotForSession(sessionID, s.Store.Snapshot())
}
