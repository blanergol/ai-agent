package skills

import (
	"github.com/blanergol/agent-core/pkg/skills/builtin"
	"github.com/blanergol/agent-core/pkg/tools"
)

// OpsSkill переэкспортирует встроенный ops skill для обратной совместимости API.
type OpsSkill = builtin.OpsSkill

var _ Skill = (*OpsSkill)(nil)
var _ PipelineExtender = (*OpsSkill)(nil)

// NewOpsSkill создает встроенный ops skill (совместимый entrypoint pkg/skills).
func NewOpsSkill(httpCfg tools.HTTPGetConfig) *OpsSkill {
	return builtin.NewOpsSkill(httpCfg)
}
