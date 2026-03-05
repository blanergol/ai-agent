package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/state"
	"github.com/blanergol/agent-core/pkg/telemetry"
)

// kvPutArgs описывает входные аргументы для инструмента `kv.put`.
type kvPutArgs struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// kvGetArgs описывает входные аргументы для инструмента `kv.get`.
type kvGetArgs struct {
	Key string `json:"key"`
}

// KVPutTool записывает пары ключ-значение в state-хранилище агента.
type KVPutTool struct {
	store state.Store
}

// NewKVPutTool создает инструмент `kv.put`.
func NewKVPutTool(store state.Store) *KVPutTool {
	return &KVPutTool{store: store}
}

// Name возвращает идентификатор инструмента.
func (t *KVPutTool) Name() string {
	return "kv.put"
}

// Description объясняет назначение инструмента для планировщика.
func (t *KVPutTool) Description() string {
	return "Stores a key-value pair in agent state"
}

// InputSchema задает JSON-схему входного payload для `kv.put`.
func (t *KVPutTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["key","value"],"properties":{"key":{"type":"string","minLength":1},"value":{}}}`
}

// OutputSchema задает JSON-схему результата инструмента.
func (t *KVPutTool) OutputSchema() string {
	return `{"type":"string","enum":["ok"]}`
}

// IsReadOnly отмечает, что `kv.put` изменяет состояние.
func (t *KVPutTool) IsReadOnly() bool { return false }

// IsSafeRetry отмечает, что повторный вызов допустим для этого инструмента.
func (t *KVPutTool) IsSafeRetry() bool { return true }

// Execute валидирует аргументы и сохраняет значение в session-scoped state.
func (t *KVPutTool) Execute(ctx context.Context, args json.RawMessage) (core.ToolResult, error) {
	var in kvPutArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return core.ToolResult{}, fmt.Errorf("decode args: %w", err)
	}
	sessionID := telemetry.SessionFromContext(ctx).SessionID
	if err := state.PutWithContext(ctx, t.store, state.NamespacedKey(sessionID, in.Key), in.Value); err != nil {
		return core.ToolResult{}, err
	}
	return core.ToolResult{Output: "ok"}, nil
}

// KVGetTool читает значения по ключу из state-хранилища агента.
type KVGetTool struct {
	store state.Store
}

// NewKVGetTool создает инструмент `kv.get`.
func NewKVGetTool(store state.Store) *KVGetTool {
	return &KVGetTool{store: store}
}

// Name возвращает идентификатор инструмента.
func (t *KVGetTool) Name() string {
	return "kv.get"
}

// Description объясняет назначение инструмента для планировщика.
func (t *KVGetTool) Description() string {
	return "Retrieves a value by key from agent state"
}

// InputSchema задает JSON-схему входного payload для `kv.get`.
func (t *KVGetTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["key"],"properties":{"key":{"type":"string","minLength":1}}}`
}

// OutputSchema задает JSON-схему результата инструмента.
func (t *KVGetTool) OutputSchema() string {
	return `{}`
}

// IsReadOnly отмечает, что `kv.get` не изменяет состояние.
func (t *KVGetTool) IsReadOnly() bool { return true }

// IsSafeRetry отмечает, что повторный вызов допустим для этого инструмента.
func (t *KVGetTool) IsSafeRetry() bool { return true }

// Execute читает значение из session-scoped state и возвращает его в JSON-формате.
func (t *KVGetTool) Execute(ctx context.Context, args json.RawMessage) (core.ToolResult, error) {
	var in kvGetArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return core.ToolResult{}, fmt.Errorf("decode args: %w", err)
	}
	sessionID := telemetry.SessionFromContext(ctx).SessionID
	v, ok, err := state.GetWithContext(ctx, t.store, state.NamespacedKey(sessionID, in.Key))
	if err != nil {
		return core.ToolResult{}, err
	}
	if !ok {
		return core.ToolResult{Output: "null"}, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("encode value: %w", err)
	}
	return core.ToolResult{Output: string(b)}, nil
}
