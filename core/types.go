package core

import (
	"strings"
	"time"
)

// APIVersion is the public run-result API contract version.
const APIVersion = "v1"

// RunMeta carries stable correlation identifiers for a run.
type RunMeta struct {
	SessionID     string
	CorrelationID string
	UserSub       string
}

// RunInput is the typed agent-run input.
type RunInput struct {
	Text          string
	SessionID     string
	CorrelationID string
	UserSub       string
}

// RunResult is the typed agent-run output.
type RunResult struct {
	FinalResponse string
	Steps         int
	ToolCalls     int
	StopReason    string
	SessionID     string
	CorrelationID string
	APIVersion    string
	PlanningSteps []PlanningStep
	CalledTools   []string
	MCPTools      []string
	Skills        []string
}

// PlanningStep describes one planner step in a run.
type PlanningStep struct {
	Step             int    `json:"step"`
	ActionType       string `json:"action_type"`
	ToolName         string `json:"tool_name,omitempty"`
	ReasoningSummary string `json:"reasoning_summary,omitempty"`
	ExpectedOutcome  string `json:"expected_outcome,omitempty"`
	Done             bool   `json:"done"`
}

// MessageRole defines message role in model context.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// Message is a generic model-context message.
type Message struct {
	Role    MessageRole
	Content string
	Name    string
}

// GuardrailsSnapshot is serializable runtime guardrail state.
type GuardrailsSnapshot struct {
	Steps     int
	ToolCalls int
	Elapsed   time.Duration
}

// RuntimeSnapshot is serializable run snapshot for session resume.
type RuntimeSnapshot struct {
	APIVersion        string
	SessionID         string
	ShortTermMessages []Message
	Guardrails        GuardrailsSnapshot
	UpdatedAt         time.Time
}

// RunContext is mutable state shared between pipeline stages.
type RunContext struct {
	Input  RunInput
	Meta   RunMeta
	Config RuntimeConfig
	Deps   RuntimeDeps

	Memory     Memory
	Guardrails Guardrails
	State      *AgentState

	Observation Observation
	NextAction  NextAction

	CurrentStep int

	ActionResult string
	ActionDone   bool
	ToolInvoked  string

	PendingStop          bool
	PendingStopReason    string
	PendingFinalResponse string

	OutputValidationAttempts int

	PlanningSteps []PlanningStep
	CalledTools   []string
	ActionRepeats map[string]int

	FinalResponse string
	StopReason    string
}

// BuildResult materializes a public RunResult from mutable run context.
func (r *RunContext) BuildResult() RunResult {
	steps := 0
	toolCalls := 0
	if r.Guardrails != nil {
		steps, toolCalls, _ = r.Guardrails.Stats()
	}
	stopReason := strings.TrimSpace(r.StopReason)
	if stopReason == "" {
		stopReason = strings.TrimSpace(r.PendingStopReason)
	}
	return RunResult{
		FinalResponse: strings.TrimSpace(r.FinalResponse),
		Steps:         steps,
		ToolCalls:     toolCalls,
		StopReason:    stopReason,
		SessionID:     r.Meta.SessionID,
		CorrelationID: r.Meta.CorrelationID,
		APIVersion:    APIVersion,
		PlanningSteps: copyPlanningSteps(r.PlanningSteps),
		CalledTools:   copyStrings(r.CalledTools),
		MCPTools:      mcpToolsFrom(r.CalledTools),
		Skills:        copyStrings(r.Config.EnabledSkills),
	}
}

// AppendCalledTool appends a tool name once.
func (r *RunContext) AppendCalledTool(toolName string) {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return
	}
	for _, existing := range r.CalledTools {
		if existing == name {
			return
		}
	}
	r.CalledTools = append(r.CalledTools, name)
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func copyPlanningSteps(values []PlanningStep) []PlanningStep {
	if len(values) == 0 {
		return nil
	}
	out := make([]PlanningStep, len(values))
	copy(out, values)
	return out
}

func copyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func mcpToolsFrom(calledTools []string) []string {
	out := make([]string, 0, len(calledTools))
	for _, toolName := range calledTools {
		if strings.HasPrefix(toolName, "mcp.") {
			out = append(out, toolName)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
