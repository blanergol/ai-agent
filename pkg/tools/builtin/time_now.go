package builtin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/blanergol/agent-core/core"
)

// TimeNowTool возвращает текущее UTC-время в формате RFC3339.
type TimeNowTool struct{}

// NewTimeNowTool создает инструмент `time.now` для получения текущего времени.
func NewTimeNowTool() *TimeNowTool {
	return &TimeNowTool{}
}

// Name возвращает идентификатор инструмента для planner/runtime.
func (t *TimeNowTool) Name() string {
	return "time.now"
}

// Description кратко описывает формат результата инструмента.
func (t *TimeNowTool) Description() string {
	return "Returns current UTC time in RFC3339 format"
}

// InputSchema объявляет, что инструмент не принимает входных полей.
func (t *TimeNowTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false}`
}

// OutputSchema задает формат выходной строки с датой/временем RFC3339.
func (t *TimeNowTool) OutputSchema() string {
	return `{"type":"string","format":"date-time"}`
}

// IsReadOnly помечает инструмент как read-only.
func (t *TimeNowTool) IsReadOnly() bool { return true }

// IsSafeRetry показывает, что повторный вызов безопасен.
func (t *TimeNowTool) IsSafeRetry() bool { return true }

// Execute возвращает текущее UTC-время в формате RFC3339.
func (t *TimeNowTool) Execute(_ context.Context, _ json.RawMessage) (core.ToolResult, error) {
	return core.ToolResult{Output: time.Now().UTC().Format(time.RFC3339)}, nil
}
