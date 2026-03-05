package core

// Phase is a deterministic intervention point inside the agent pipeline.
type Phase string

const (
	PhaseInput               Phase = "INPUT"
	PhaseNormalize           Phase = "NORMALIZE"
	PhaseGuardrails          Phase = "GUARDRAILS"
	PhaseEnrichContext       Phase = "ENRICH_CONTEXT"
	PhaseDecideAction        Phase = "DECIDE_ACTION"
	PhaseBeforeToolExecution Phase = "BEFORE_TOOL_EXECUTION"
	PhaseToolExecution       Phase = "TOOL_EXECUTION"
	PhaseAfterToolExecution  Phase = "AFTER_TOOL_EXECUTION"
	PhaseEvaluate            Phase = "EVALUATE"
	PhaseStopCheck           Phase = "STOP_CHECK"
	PhaseFinalize            Phase = "FINALIZE"
)

func (p Phase) String() string { return string(p) }
