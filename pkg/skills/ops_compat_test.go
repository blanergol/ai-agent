package skills

import (
	"log/slog"
	"testing"

	"github.com/blanergol/agent-core/pkg/tools"
)

// TestNewOpsSkillCompatibility проверяет совместимость re-export конструктора ops-навыка.
func TestNewOpsSkillCompatibility(t *testing.T) {
	registry := tools.NewRegistry(tools.RegistryConfig{}, slog.Default())
	skill := NewOpsSkill(tools.HTTPGetConfig{AllowDomains: []string{"example.com"}})
	if err := skill.Register(registry); err != nil {
		t.Fatalf("register ops skill: %v", err)
	}

	specs := registry.Specs()
	if len(specs) != 2 {
		t.Fatalf("specs len = %d, want 2", len(specs))
	}
}
