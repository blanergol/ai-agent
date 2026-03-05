package internal

import (
	"fmt"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/internal/interceptors"
	"github.com/blanergol/agent-core/internal/pipeline"
	internalSkills "github.com/blanergol/agent-core/internal/skills"
	"github.com/blanergol/agent-core/internal/tools"
	skillspkg "github.com/blanergol/agent-core/pkg/skills"
	"github.com/blanergol/agent-core/pkg/stages"
	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

// Bundle groups optional runtime extensions for the incident-response domain.
type Bundle struct {
	Tools             []toolkit.Tool
	toolNames         []string
	Skills            []skillspkg.Skill
	skillNames        []string
	promptAdditions   []string
	pipelineMutations []stages.PipelineMutation
	phaseInterceptors []PhaseRegistration
	toolInterceptors  []core.ToolExecutionInterceptor
}

type PhaseRegistration struct {
	phase       core.Phase
	interceptor core.PhaseInterceptor
}

// NewBundle builds a practical incident-response bundle with private tools/interceptors.
func NewBundle() *Bundle {
	store := tools.NewIncidentStore()

	serviceLookupTool := tools.NewServiceLookupTool(store)
	runbookLookupTool := tools.NewRunbookLookupTool(store)
	onCallLookupTool := tools.NewOnCallLookupTool(store)
	incidentCreateTool := tools.NewIncidentCreateTool(store)
	incidentUpdateTool := tools.NewIncidentUpdateTool(store)
	incidentStatusTool := tools.NewIncidentStatusTool(store)
	incidentSkill := internalSkills.NewIncidentOpsSkill()

	return &Bundle{
		Tools: []toolkit.Tool{
			serviceLookupTool,
			runbookLookupTool,
			onCallLookupTool,
			incidentCreateTool,
			incidentUpdateTool,
			incidentStatusTool,
		},
		toolNames: []string{
			serviceLookupTool.Name(),
			runbookLookupTool.Name(),
			onCallLookupTool.Name(),
			incidentCreateTool.Name(),
			incidentUpdateTool.Name(),
			incidentStatusTool.Name(),
		},
		Skills: []skillspkg.Skill{
			incidentSkill,
		},
		skillNames: []string{
			incidentSkill.Name(),
		},
		promptAdditions: []string{
			"Incident-response bundle enabled: prefer .service.lookup before any incident mutation.",
			"When an incident is created, continue with .runbook.lookup and .oncall.lookup for response orchestration.",
			"Use .incident.update to record timeline notes and status transitions before finalizing.",
		},
		pipelineMutations: pipeline.PipelineMutations(),
		phaseInterceptors: []PhaseRegistration{
			{phase: core.PhaseNormalize, interceptor: interceptors.NewNormalizeTraceInterceptor()},
			{phase: core.PhaseEnrichContext, interceptor: interceptors.NewContextEnrichmentInterceptor()},
			{phase: core.PhaseAfterToolExecution, interceptor: interceptors.NewAfterToolExecutionStateInterceptor()},
			{phase: core.PhaseStopCheck, interceptor: interceptors.NewStopPolicyInterceptor(8)},
			{phase: core.PhaseFinalize, interceptor: interceptors.NewFinalizeTraceInterceptor()},
		},
		toolInterceptors: []core.ToolExecutionInterceptor{
			interceptors.NewToolPolicyInterceptor([]string{
				".incident.delete",
				".runbook.write",
				".service.catalog.write",
				".dangerous.write",
			}),
			interceptors.NewToolRewriteInterceptor(),
			interceptors.NewToolFallbackInterceptor(),
		},
	}
}

// ToolNames returns a copy of private tool names.
func (b *Bundle) ToolNames() []string {
	if b == nil || len(b.toolNames) == 0 {
		return nil
	}
	out := make([]string, len(b.toolNames))
	copy(out, b.toolNames)
	return out
}

// PromptAdditions returns planner prompt additions for this bundle.
func (b *Bundle) PromptAdditions() []string {
	if b == nil || len(b.promptAdditions) == 0 {
		return nil
	}
	out := make([]string, len(b.promptAdditions))
	copy(out, b.promptAdditions)
	return out
}

// SkillNames returns private skill names available when this bundle is enabled.
func (b *Bundle) SkillNames() []string {
	if b == nil || len(b.skillNames) == 0 {
		return nil
	}
	out := make([]string, len(b.skillNames))
	copy(out, b.skillNames)
	return out
}

// PipelineMutations returns deterministic pipeline mutations used by this bundle.
func (b *Bundle) PipelineMutations() []stages.PipelineMutation {
	if b == nil || len(b.pipelineMutations) == 0 {
		return nil
	}
	out := make([]stages.PipelineMutation, len(b.pipelineMutations))
	copy(out, b.pipelineMutations)
	return out
}

// RegisterTools registers all private tools in one place.
func (b *Bundle) RegisterTools(registry *toolkit.Registry) error {
	if b == nil {
		return nil
	}
	if registry == nil {
		return fmt.Errorf("tool registry is nil")
	}
	for _, tool := range b.Tools {
		if err := registry.Register(tool); err != nil {
			return fmt.Errorf("register private tool %s: %w", tool.Name(), err)
		}
	}
	return nil
}

// RegisterSkills registers private skills in the shared skill registry.
func (b *Bundle) RegisterSkills(registry *skillspkg.Registry) error {
	if b == nil {
		return nil
	}
	if registry == nil {
		return fmt.Errorf("skill registry is nil")
	}
	for _, skill := range b.Skills {
		if err := registry.Register(skill); err != nil {
			return fmt.Errorf("register private skill %s: %w", skill.Name(), err)
		}
	}
	return nil
}

// RegisterInterceptors registers all phase/tool interceptors.
func (b *Bundle) RegisterInterceptors(registry *core.InterceptorRegistry) {
	if b == nil || registry == nil {
		return
	}
	for _, item := range b.phaseInterceptors {
		registry.RegisterInterceptor(item.phase, item.interceptor)
	}
	for _, interceptor := range b.toolInterceptors {
		registry.RegisterToolInterceptor(interceptor)
	}
}
