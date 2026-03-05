package state

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/retry"
)

// runtimeSnapshotPrefix задает общий префикс ключей для сохранения runtime snapshot-ов.
const runtimeSnapshotPrefix = "runtime.snapshot:"

// SnapshotStore сохраняет runtime snapshot-ы core в абстрактном key-value хранилище.
type SnapshotStore struct {
	store       Store
	retryPolicy retry.Policy
}

// NewSnapshotStore создает SnapshotStore со стандартной retry-политикой.
func NewSnapshotStore(store Store) *SnapshotStore {
	return NewSnapshotStoreWithPolicy(store, retry.Policy{
		MaxRetries: 2,
		BaseDelay:  200 * time.Millisecond,
		MaxDelay:   2 * time.Second,
	})
}

// NewSnapshotStoreWithPolicy создает SnapshotStore с явно заданной retry-политикой.
func NewSnapshotStoreWithPolicy(store Store, retryPolicy retry.Policy) *SnapshotStore {
	return &SnapshotStore{store: store, retryPolicy: retryPolicy}
}

// Save сериализует и сохраняет snapshot текущей сессии в state-хранилище.
func (s *SnapshotStore) Save(ctx context.Context, snapshot core.RuntimeSnapshot) error {
	if s == nil || s.store == nil {
		return nil
	}
	if strings.TrimSpace(snapshot.SessionID) == "" {
		return nil
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal runtime snapshot: %w", err)
	}
	return retry.Do(ctx, s.retryPolicy, retry.DefaultClassifier, func(callCtx context.Context) error {
		if err := PutWithContext(callCtx, s.store, runtimeSnapshotPrefix+snapshot.SessionID, string(raw)); err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "persist runtime snapshot", err, true)
		}
		return nil
	})
}

// Load загружает runtime snapshot сессии и декодирует его в тип core.RuntimeSnapshot.
func (s *SnapshotStore) Load(ctx context.Context, sessionID string) (core.RuntimeSnapshot, bool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(sessionID) == "" {
		return core.RuntimeSnapshot{}, false, nil
	}
	raw := ""
	found := false
	err := retry.Do(ctx, s.retryPolicy, retry.DefaultClassifier, func(callCtx context.Context) error {
		value, ok, err := GetWithContext(callCtx, s.store, runtimeSnapshotPrefix+sessionID)
		if err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "load runtime snapshot", err, true)
		}
		if !ok || value == nil {
			found = false
			raw = ""
			return nil
		}
		str, ok := value.(string)
		if !ok {
			return apperrors.New(apperrors.CodeValidation, "runtime snapshot value is not string", false)
		}
		raw = str
		found = strings.TrimSpace(raw) != ""
		return nil
	})
	if err != nil {
		return core.RuntimeSnapshot{}, false, err
	}
	if !found {
		return core.RuntimeSnapshot{}, false, nil
	}
	var snapshot core.RuntimeSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return core.RuntimeSnapshot{}, false, fmt.Errorf("decode runtime snapshot: %w", err)
	}
	return snapshot, true, nil
}
