package tools

import (
	"context"
	"encoding/json"
	"time"
)

// Tool описывает единый интерфейс исполняемого инструмента агента.
type Tool interface {
	// Name возвращает уникальный идентификатор инструмента.
	Name() string
	// Description кратко объясняет назначение инструмента.
	Description() string
	// InputSchema возвращает JSON Schema допустимых аргументов.
	InputSchema() string
	// OutputSchema возвращает JSON Schema результата инструмента.
	OutputSchema() string
	// Execute выполняет инструмент с переданными JSON-аргументами.
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// ToolResult содержит нормализованный результат выполнения инструмента.
type ToolResult struct {
	// Output содержит основной текстовый результат выполнения инструмента.
	Output string `json:"output"`
	// Metadata опционально передаёт дополнительные структурированные данные.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Spec представляет сериализуемую спецификацию инструмента для планировщика.
type Spec struct {
	// Name - имя инструмента для каталога и вызова.
	Name string `json:"name"`
	// Description - человекочитаемое назначение инструмента.
	Description string `json:"description"`
	// InputSchema - схема валидных входных данных.
	InputSchema string `json:"input_schema"`
	// OutputSchema - схема валидного результата инструмента.
	OutputSchema string `json:"output_schema"`
}

// RetryPolicy позволяет инструменту явно задать безопасную стратегию повторов.
type RetryPolicy interface {
	// IsReadOnly возвращает true для инструментов без побочных эффектов.
	IsReadOnly() bool
	// IsSafeRetry возвращает true, если повтор после частичного сбоя безопасен.
	IsSafeRetry() bool
}

// CachePolicy позволяет read-only инструменту явно объявить TTL кэширования результата.
type CachePolicy interface {
	// CacheTTL возвращает TTL кэша. Значение <= 0 отключает кэширование.
	CacheTTL() time.Duration
}
