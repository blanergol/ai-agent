package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blanergol/agent-core/internal/state"
)

// kvPutArgs описывает входные аргументы инструмента kv.put.
type kvPutArgs struct {
	// Key задаёт имя сохраняемого значения.
	Key string `json:"key"`
	// Value содержит произвольное JSON-значение для записи в state store.
	Value any `json:"value"`
}

// kvGetArgs описывает входные аргументы инструмента kv.get.
type kvGetArgs struct {
	// Key задаёт имя значения для чтения.
	Key string `json:"key"`
}

// KVPutTool записывает значения в state store агента.
type KVPutTool struct {
	// store обеспечивает доступ к постоянному KV-состоянию агента.
	store state.Store
}

// NewKVPutTool создаёт инструмент записи значения по ключу.
func NewKVPutTool(store state.Store) *KVPutTool {
	return &KVPutTool{store: store}
}

// Name возвращает идентификатор инструмента записи.
func (t *KVPutTool) Name() string {
	return "kv.put"
}

// Description кратко объясняет, что инструмент изменяет state store.
func (t *KVPutTool) Description() string {
	return "Stores a key-value pair in agent state"
}

// InputSchema требует непустой key и любое JSON-значение value.
func (t *KVPutTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["key","value"],"properties":{"key":{"type":"string","minLength":1},"value":{}}}`
}

// OutputSchema фиксирует, что успешная запись возвращает константный маркер.
func (t *KVPutTool) OutputSchema() string {
	return `{"type":"string","enum":["ok"]}`
}

// IsReadOnly указывает, что инструмент изменяет состояние.
func (t *KVPutTool) IsReadOnly() bool { return false }

// IsSafeRetry разрешает безопасный повтор одной и той же операции записи.
func (t *KVPutTool) IsSafeRetry() bool { return true }

// Execute валидирует аргументы и сохраняет значение в state store текущей сессии.
func (t *KVPutTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	// in содержит декодированные аргументы вызова инструмента.
	var in kvPutArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("decode args: %w", err)
	}
	if err := state.PutWithContext(ctx, t.store, state.NamespacedKey(ctx, in.Key), in.Value); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Output: "ok"}, nil
}

// KVGetTool читает значения из state store агента.
type KVGetTool struct {
	// store обеспечивает чтение значений из KV-состояния агента.
	store state.Store
}

// NewKVGetTool создаёт инструмент чтения значения по ключу.
func NewKVGetTool(store state.Store) *KVGetTool {
	return &KVGetTool{store: store}
}

// Name возвращает идентификатор инструмента чтения.
func (t *KVGetTool) Name() string {
	return "kv.get"
}

// Description описывает назначение инструмента чтения состояния.
func (t *KVGetTool) Description() string {
	return "Retrieves a value by key from agent state"
}

// InputSchema требует только ключ для чтения.
func (t *KVGetTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["key"],"properties":{"key":{"type":"string","minLength":1}}}`
}

// OutputSchema допускает любой валидный JSON-тип, включая null.
func (t *KVGetTool) OutputSchema() string {
	return `{}`
}

// IsReadOnly указывает, что инструмент выполняет только чтение.
func (t *KVGetTool) IsReadOnly() bool { return true }

// IsSafeRetry указывает, что повтор чтения безопасен.
func (t *KVGetTool) IsSafeRetry() bool { return true }

// Execute возвращает JSON-представление значения из namespace текущей сессии или `null`.
func (t *KVGetTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	// in содержит разобранный ключ запроса.
	var in kvGetArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("decode args: %w", err)
	}
	// v - сырое значение из store до сериализации.
	v, ok, err := state.GetWithContext(ctx, t.store, state.NamespacedKey(ctx, in.Key))
	if err != nil {
		return ToolResult{}, err
	}
	if !ok {
		return ToolResult{Output: "null"}, nil
	}
	// b хранит сериализованное JSON-представление найденного значения.
	b, err := json.Marshal(v)
	if err != nil {
		return ToolResult{}, fmt.Errorf("encode value: %w", err)
	}
	return ToolResult{Output: string(b)}, nil
}
