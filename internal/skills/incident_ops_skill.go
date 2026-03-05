package skills

import (
	"fmt"

	skillspkg "github.com/blanergol/agent-core/pkg/skills"
	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

// IncidentOpsSkill is an internal business skill for incident-response procedures.
type IncidentOpsSkill struct{}

var _ skillspkg.Skill = (*IncidentOpsSkill)(nil)

// NewIncidentOpsSkill creates internal incident skill.
func NewIncidentOpsSkill() *IncidentOpsSkill {
	return &IncidentOpsSkill{}
}

// Name returns skill identifier configured via AGENT_CORE_SKILLS.
func (s *IncidentOpsSkill) Name() string {
	return "incident_ops"
}

// Register validates registry availability. The skill is guidance-focused and does not add tools.
func (s *IncidentOpsSkill) Register(registry *toolkit.Registry) error {
	if registry == nil {
		return fmt.Errorf("tool registry is nil")
	}
	return nil
}

// PromptAdditions returns deterministic planning guidance for incident workflows.
func (s *IncidentOpsSkill) PromptAdditions() []string {
	return []string{
		"Skill incident_ops enabled: follow incident workflow service.lookup -> incident.create/update -> incident.status.",
		"For explicit cross-request persistence, use kv.put/kv.get with incident.last_id and incident.last_service keys.",
		"If mcp.* observability tools are available, gather evidence before escalation or paging on-call.",
	}
}
