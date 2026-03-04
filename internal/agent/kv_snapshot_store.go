package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/blanergol/agent-core/internal/retry"
	"github.com/blanergol/agent-core/internal/state"
)

// runtimeSnapshotPrefix задаёт ключевой префикс для snapshot-значений в state.Store.
const runtimeSnapshotPrefix = "runtime.snapshot:"

// KVSnapshotStore сохраняет runtime snapshot в общем state.Store.
type KVSnapshotStore struct {
	// store предоставляет персистентный key-value backend.
	store state.Store
	// retryPolicy задаёт единый retry/backoff контракт для операций snapshot persistence.
	retryPolicy retry.Policy
}

// NewKVSnapshotStore создаёт snapshot store поверх state.Store.
func NewKVSnapshotStore(store state.Store) *KVSnapshotStore {
	return NewKVSnapshotStoreWithPolicy(store, retry.Policy{
		MaxRetries: 2,
		BaseDelay:  200 * time.Millisecond,
		MaxDelay:   2 * time.Second,
	})
}

// NewKVSnapshotStoreWithPolicy создаёт snapshot store с явной retry/backoff политикой.
func NewKVSnapshotStoreWithPolicy(store state.Store, retryPolicy retry.Policy) *KVSnapshotStore {
	return &KVSnapshotStore{
		store:       store,
		retryPolicy: retryPolicy,
	}
}

// Save сериализует snapshot и пишет его в state store.
func (s *KVSnapshotStore) Save(ctx context.Context, snapshot RuntimeSnapshot) error {
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
		if err := state.PutWithContext(callCtx, s.store, runtimeSnapshotPrefix+snapshot.SessionID, string(raw)); err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "persist runtime snapshot", err, true)
		}
		return nil
	})
}

// Load читает snapshot из state store и декодирует его в typed структуру.
func (s *KVSnapshotStore) Load(ctx context.Context, sessionID string) (RuntimeSnapshot, bool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(sessionID) == "" {
		return RuntimeSnapshot{}, false, nil
	}
	raw := ""
	found := false
	err := retry.Do(ctx, s.retryPolicy, retry.DefaultClassifier, func(callCtx context.Context) error {
		value, ok, err := state.GetWithContext(callCtx, s.store, runtimeSnapshotPrefix+sessionID)
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
		return RuntimeSnapshot{}, false, err
	}
	if !found {
		return RuntimeSnapshot{}, false, nil
	}
	var snapshot RuntimeSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return RuntimeSnapshot{}, false, fmt.Errorf("decode runtime snapshot: %w", err)
	}
	return snapshot, true, nil
}
