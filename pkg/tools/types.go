package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/blanergol/agent-core/core"
)

// Tool описывает исполняемый инструмент, который может вызываться агентом во время цикла.
type Tool interface {
	Name() string
	Description() string
	InputSchema() string
	OutputSchema() string
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// ToolResult переиспользует общий контракт результата вызова инструмента из core.
type ToolResult = core.ToolResult

// Spec переиспользует контракт описания инструмента, который видит планировщик.
type Spec = core.ToolSpec

// RetryPolicy описывает, можно ли безопасно повторять вызов инструмента при ошибках.
type RetryPolicy interface {
	IsReadOnly() bool
	IsSafeRetry() bool
}

// CachePolicy задает TTL кэша для идемпотентных read-only инструментов.
type CachePolicy interface {
	CacheTTL() time.Duration
}
