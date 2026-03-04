package skills

import (
	"fmt"

	"github.com/blanergol/agent-core/internal/tools"
)

// OpsSkill регистрирует операционные инструменты (время и безопасный HTTP GET).
type OpsSkill struct {
	// HTTPConfig передаёт ограничения безопасности и тайм-ауты инструмента http.get.
	HTTPConfig tools.HTTPGetConfig
}

// NewOpsSkill создаёт набор инструментов для операционных задач (время и безопасный HTTP GET).
func NewOpsSkill(httpCfg tools.HTTPGetConfig) *OpsSkill {
	return &OpsSkill{HTTPConfig: httpCfg}
}

// Name возвращает стабильный идентификатор навыка для конфигурации.
func (s *OpsSkill) Name() string {
	return "ops"
}

// Register подключает инструменты навыка в общий registry.
func (s *OpsSkill) Register(registry *tools.Registry) error {
	if registry == nil {
		return fmt.Errorf("tool registry is nil")
	}
	if err := registry.Register(tools.NewTimeNowTool()); err != nil {
		return err
	}
	if err := registry.Register(tools.NewHTTPGetTool(s.HTTPConfig)); err != nil {
		return err
	}
	return nil
}

// PromptAdditions добавляет подсказки для планировщика о назначении подключённых инструментов.
func (s *OpsSkill) PromptAdditions() []string {
	return []string{
		"Skill ops enabled: use time.now for current UTC time and http.get for safe read-only web fetches.",
	}
}
