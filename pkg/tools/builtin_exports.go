package tools

import (
	"github.com/blanergol/agent-core/pkg/state"
	"github.com/blanergol/agent-core/pkg/tools/builtin"
)

// HTTPGetConfig задает ограничения безопасности и производительности для `http.get`.
type HTTPGetConfig = builtin.HTTPGetConfig

// HTTPGetTool выполняет безопасные GET-запросы только к доменам из allowlist.
type HTTPGetTool = builtin.HTTPGetTool

// NewHTTPGetTool создает инструмент `http.get` с дефолтными лимитами.
func NewHTTPGetTool(cfg HTTPGetConfig) *HTTPGetTool {
	return builtin.NewHTTPGetTool(cfg)
}

// KVPutTool записывает пары ключ-значение в state-хранилище агента.
type KVPutTool = builtin.KVPutTool

// NewKVPutTool создает инструмент `kv.put`.
func NewKVPutTool(store state.Store) *KVPutTool {
	return builtin.NewKVPutTool(store)
}

// KVGetTool читает значения по ключу из state-хранилища агента.
type KVGetTool = builtin.KVGetTool

// NewKVGetTool создает инструмент `kv.get`.
func NewKVGetTool(store state.Store) *KVGetTool {
	return builtin.NewKVGetTool(store)
}

// TimeNowTool возвращает текущее время UTC в формате RFC3339.
type TimeNowTool = builtin.TimeNowTool

// NewTimeNowTool создает инструмент `time.now`.
func NewTimeNowTool() *TimeNowTool {
	return builtin.NewTimeNowTool()
}
