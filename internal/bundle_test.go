package internal

import (
	"log/slog"
	"testing"

	skillspkg "github.com/blanergol/agent-core/pkg/skills"
	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

func TestBundleRegistersInternalSkills(t *testing.T) {
	bundle := NewBundle()
	skillRegistry := skillspkg.NewRegistry()
	if err := bundle.RegisterSkills(skillRegistry); err != nil {
		t.Fatalf("register bundle skills: %v", err)
	}

	toolRegistry := toolkit.NewRegistry(toolkit.RegistryConfig{}, slog.Default())
	result, err := skillRegistry.Apply([]string{"incident_ops"}, toolRegistry)
	if err != nil {
		t.Fatalf("apply incident_ops: %v", err)
	}
	if len(result.PromptAdditions) == 0 {
		t.Fatalf("prompt additions should not be empty")
	}
}
