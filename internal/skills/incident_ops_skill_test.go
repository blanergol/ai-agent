package skills

import (
	"log/slog"
	"testing"

	"github.com/blanergol/agent-core/pkg/tools"
)

func TestIncidentOpsSkillRegister(t *testing.T) {
	registry := tools.NewRegistry(tools.RegistryConfig{}, slog.Default())
	skill := NewIncidentOpsSkill()
	if err := skill.Register(registry); err != nil {
		t.Fatalf("register skill: %v", err)
	}
}

func TestIncidentOpsSkillPromptAdditions(t *testing.T) {
	skill := NewIncidentOpsSkill()
	prompts := skill.PromptAdditions()
	if len(prompts) == 0 {
		t.Fatalf("prompt additions should not be empty")
	}
}
