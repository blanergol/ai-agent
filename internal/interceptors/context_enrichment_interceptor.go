package interceptors

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ContextEnrichmentInterceptor injects deterministic incident-response context.
type ContextEnrichmentInterceptor struct{}

// NewContextEnrichmentInterceptor creates deterministic context enrichment interceptor.
func NewContextEnrichmentInterceptor() core.PhaseInterceptor {
	return &ContextEnrichmentInterceptor{}
}

func (i *ContextEnrichmentInterceptor) Name() string {
	return "ContextEnrichmentInterceptor"
}

// BeforePhase enriches run context with incident intent, memory snapshot and MCP capability hints.
func (i *ContextEnrichmentInterceptor) BeforePhase(ctx context.Context, run *core.RunContext, _ core.Phase) error {
	if run == nil {
		return nil
	}
	if run.State == nil {
		run.State = core.NewAgentState(run.Input.Text)
	}
	if run.State.Context == nil {
		run.State.Context = map[string]any{}
	}

	intent, severityHint, isEscalation := InferIncidentContext(run.Input.Text)
	sessionState := map[string]any{}
	if run.Deps.State != nil {
		sessionState = run.Deps.State.SnapshotForSession(ctx)
	}
	mcpTools := extractMCPToolNames(run)
	flow := recommendedFlow(intent, len(mcpTools) > 0)

	run.State.Context["IncidentContext"] = map[string]any{
		"intent":           intent,
		"severity_hint":    severityHint,
		"is_escalation":    isEscalation,
		"recommended_flow": flow,
		"session_state":    sessionState,
		"mcp_tools":        mcpTools,
	}

	if prevID, ok := sessionState["incident.last_id"].(string); ok && strings.TrimSpace(prevID) != "" {
		run.State.RetrievedDocs = append(run.State.RetrievedDocs, "Session memory indicates previous incident: "+strings.TrimSpace(prevID))
	}
	run.State.RetrievedDocs = append(run.State.RetrievedDocs,
		"Incident playbook: lookup service metadata before create/update operations.",
		"Incident playbook: keep status transitions explicit (investigating -> mitigating -> monitoring -> resolved).",
	)
	if len(mcpTools) > 0 {
		run.State.RetrievedDocs = append(run.State.RetrievedDocs, "MCP observability tools detected; use them as additional evidence before escalation.")
	}
	return nil
}

func (i *ContextEnrichmentInterceptor) AfterPhase(_ context.Context, _ *core.RunContext, _ core.Phase, _ error) error {
	return nil
}

// InferIncidentContext derives deterministic incident intent and severity hints.
func InferIncidentContext(input string) (intent string, severityHint string, isEscalation bool) {
	normalized := strings.ToLower(strings.TrimSpace(input))
	intent = "triage"
	switch {
	case strings.Contains(normalized, "status"), strings.Contains(normalized, "статус"):
		intent = "status_check"
	case strings.Contains(normalized, "runbook"), strings.Contains(normalized, "онкол"), strings.Contains(normalized, "oncall"):
		intent = "response_guidance"
	case strings.Contains(normalized, "create incident"), strings.Contains(normalized, "заведи инцидент"), strings.Contains(normalized, "эскалир"):
		intent = "incident_creation"
	}

	severityHint = "sev3"
	switch {
	case strings.Contains(normalized, "outage"),
		strings.Contains(normalized, "critical"),
		strings.Contains(normalized, "sev1"),
		strings.Contains(normalized, "p1"),
		strings.Contains(normalized, "полностью лежит"):
		severityHint = "sev1"
		isEscalation = true
	case strings.Contains(normalized, "degraded"),
		strings.Contains(normalized, "high latency"),
		strings.Contains(normalized, "sev2"),
		strings.Contains(normalized, "p2"):
		severityHint = "sev2"
		isEscalation = true
	case strings.Contains(normalized, "delayed"),
		strings.Contains(normalized, "minor"),
		strings.Contains(normalized, "sev4"),
		strings.Contains(normalized, "p4"):
		severityHint = "sev4"
	}
	return intent, severityHint, isEscalation
}

func extractMCPToolNames(run *core.RunContext) []string {
	if run == nil || run.Deps.Tools == nil {
		return nil
	}
	specs := run.Deps.Tools.Specs()
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if strings.HasPrefix(name, "mcp.") {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func recommendedFlow(intent string, hasMCP bool) []string {
	flow := []string{
		".service.lookup",
		".incident.create",
		".runbook.lookup",
		".oncall.lookup",
		".incident.update",
		".incident.status",
	}
	if hasMCP {
		flow = append([]string{"mcp.* (observability evidence)"}, flow...)
	}
	if intent == "status_check" {
		return []string{".incident.status", ".incident.update"}
	}
	return flow
}
