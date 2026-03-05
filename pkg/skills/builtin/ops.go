package builtin

import (
	"fmt"

	"github.com/blanergol/agent-core/pkg/stages"
	"github.com/blanergol/agent-core/pkg/tools"
)

// OpsSkill подключает операционные инструменты (time.now + безопасный http.get).
type OpsSkill struct {
	HTTPConfig tools.HTTPGetConfig
}

// NewOpsSkill создает конфигурацию встроенного навыка ops.
func NewOpsSkill(httpCfg tools.HTTPGetConfig) *OpsSkill {
	return &OpsSkill{HTTPConfig: httpCfg}
}

// Name возвращает идентификатор навыка для выбора через конфигурацию runtime.
func (s *OpsSkill) Name() string {
	return "ops"
}

// Register подключает встроенные операционные инструменты в общий tool registry.
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

// PromptAdditions возвращает дополнительные инструкции planner-у о доступных инструментах навыка.
func (s *OpsSkill) PromptAdditions() []string {
	return []string{
		"Skill ops enabled: use time.now for current UTC time and http.get for safe read-only web fetches.",
	}
}

// PipelineMutations возвращает изменения pipeline; для ops сейчас изменений нет.
func (s *OpsSkill) PipelineMutations() []stages.PipelineMutation {
	return nil
}
