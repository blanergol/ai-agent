package builtin

import (
	"log/slog"
	"testing"

	"github.com/blanergol/agent-core/pkg/tools"
)

// TestOpsSkillRegistersOperationalTools проверяет регистрацию набора инструментов ops-навыка.
func TestOpsSkillRegistersOperationalTools(t *testing.T) {
	registry := tools.NewRegistry(tools.RegistryConfig{}, slog.Default())
	skill := NewOpsSkill(tools.HTTPGetConfig{AllowDomains: []string{"example.com"}})
	if err := skill.Register(registry); err != nil {
		t.Fatalf("register ops skill: %v", err)
	}

	specs := registry.Specs()
	if len(specs) != 2 {
		t.Fatalf("specs len = %d, want 2", len(specs))
	}
	if specs[0].Name != "http.get" || specs[1].Name != "time.now" {
		t.Fatalf("unexpected tool specs order/names: %#v", specs)
	}
}
