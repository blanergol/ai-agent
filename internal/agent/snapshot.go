package agent

import (
	"context"
	"sync"
	"time"

	"github.com/blanergol/agent-core/internal/guardrails"
	"github.com/blanergol/agent-core/internal/llm"
)

// APIVersion фиксирует публичную версию контракта запуска агента.
const APIVersion = "v1"

// RuntimeSnapshot содержит минимальное состояние для продолжения обработки сессии.
type RuntimeSnapshot struct {
	// APIVersion позволяет безопасно эволюционировать формат snapshot.
	APIVersion string `json:"api_version"`
	// SessionID определяет сессию, к которой относится снимок.
	SessionID string `json:"session_id"`
	// ShortTermMessages хранит кратковременный контекст диалога.
	ShortTermMessages []llm.Message `json:"short_term_messages"`
	// Guardrails хранит счётчики и elapsed-время лимитов выполнения.
	Guardrails guardrails.RuntimeSnapshot `json:"guardrails"`
	// UpdatedAt фиксирует момент последнего сохранения снимка.
	UpdatedAt time.Time `json:"updated_at"`
}

// RunnerV1 описывает публичный контракт запуска агента версии v1.
type RunnerV1 interface {
	RunWithInput(ctx context.Context, in RunInput) (RunResult, error)
}

// SnapshotStore описывает персистентный контракт snapshot/restore состояния рантайма.
type SnapshotStore interface {
	// Save сохраняет состояние по SessionID.
	Save(ctx context.Context, snapshot RuntimeSnapshot) error
	// Load возвращает ранее сохранённое состояние для сессии.
	Load(ctx context.Context, sessionID string) (RuntimeSnapshot, bool, error)
}

// NoopSnapshotStore отключает сохранение runtime-снимков.
type NoopSnapshotStore struct{}

// Save в noop-режиме игнорирует snapshot.
func (NoopSnapshotStore) Save(_ context.Context, _ RuntimeSnapshot) error { return nil }

// Load в noop-режиме всегда возвращает отсутствие snapshot.
func (NoopSnapshotStore) Load(_ context.Context, _ string) (RuntimeSnapshot, bool, error) {
	return RuntimeSnapshot{}, false, nil
}

// InMemorySnapshotStore хранит snapshot'ы в памяти процесса для reference-режима.
type InMemorySnapshotStore struct {
	// mu защищает map snapshot'ов от гонок.
	mu sync.RWMutex
	// items хранит последний snapshot на SessionID.
	items map[string]RuntimeSnapshot
}

// NewInMemorySnapshotStore создаёт in-memory store runtime-снимков.
func NewInMemorySnapshotStore() *InMemorySnapshotStore {
	return &InMemorySnapshotStore{items: make(map[string]RuntimeSnapshot)}
}

// Save сохраняет снимок в map по ключу сессии.
func (s *InMemorySnapshotStore) Save(_ context.Context, snapshot RuntimeSnapshot) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot.ShortTermMessages = cloneMessages(snapshot.ShortTermMessages)
	s.items[snapshot.SessionID] = snapshot
	return nil
}

// Load возвращает копию snapshot для запрошенной сессии.
func (s *InMemorySnapshotStore) Load(_ context.Context, sessionID string) (RuntimeSnapshot, bool, error) {
	if s == nil {
		return RuntimeSnapshot{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.items[sessionID]
	if !ok {
		return RuntimeSnapshot{}, false, nil
	}
	snapshot.ShortTermMessages = cloneMessages(snapshot.ShortTermMessages)
	return snapshot, true, nil
}

// cloneMessages возвращает независимую копию среза сообщений для защиты от внешней мутации.
func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(messages))
	copy(out, messages)
	return out
}
