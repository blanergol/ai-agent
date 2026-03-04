package tools

import (
	"context"
	"encoding/json"
	"time"
)

// TimeNowTool возвращает текущее UTC-время в формате RFC3339.
type TimeNowTool struct{}

// NewTimeNowTool создаёт инструмент, возвращающий текущее UTC-время.
func NewTimeNowTool() *TimeNowTool {
	return &TimeNowTool{}
}

// Name возвращает идентификатор инструмента для вызова из планировщика.
func (t *TimeNowTool) Name() string {
	return "time.now"
}

// Description кратко объясняет формат результата.
func (t *TimeNowTool) Description() string {
	return "Returns current UTC time in RFC3339 format"
}

// InputSchema указывает, что инструмент не принимает входных полей.
func (t *TimeNowTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false}`
}

// OutputSchema задаёт строковый ответ в формате RFC3339.
func (t *TimeNowTool) OutputSchema() string {
	return `{"type":"string","format":"date-time"}`
}

// IsReadOnly указывает, что инструмент не изменяет внешнее состояние.
func (t *TimeNowTool) IsReadOnly() bool { return true }

// IsSafeRetry указывает, что повтор вызова безопасен.
func (t *TimeNowTool) IsSafeRetry() bool { return true }

// Execute возвращает текущее время в стандартизированном RFC3339-представлении.
func (t *TimeNowTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{Output: time.Now().UTC().Format(time.RFC3339)}, nil
}
