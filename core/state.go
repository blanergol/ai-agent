package core

import (
	"encoding/json"
	"strings"
	"time"
)

// GuardrailResults captures latest guardrail counters and violations.
type GuardrailResults struct {
	Violations []string      `json:"violations,omitempty"`
	Steps      int           `json:"steps"`
	ToolCalls  int           `json:"tool_calls"`
	Elapsed    time.Duration `json:"elapsed"`
}

// BudgetState captures token/time/cost budgets for the run.
type BudgetState struct {
	TokenLimit   int           `json:"token_limit"`
	TokenUsed    int           `json:"token_used"`
	TimeLimit    time.Duration `json:"time_limit"`
	TimeUsed     time.Duration `json:"time_used"`
	CostLimitUSD float64       `json:"cost_limit_usd"`
	CostUsedUSD  float64       `json:"cost_used_usd"`
}

// ToolCallTrace stores one tool invocation trace.
type ToolCallTrace struct {
	Iteration int             `json:"iteration"`
	ToolName  string          `json:"tool_name"`
	ToolArgs  json.RawMessage `json:"tool_args,omitempty"`
	Source    string          `json:"source,omitempty"`
	Duration  time.Duration   `json:"duration"`
	Timestamp time.Time       `json:"timestamp"`
	Error     string          `json:"error,omitempty"`
}

// ToolResultTrace stores one tool execution result trace.
type ToolResultTrace struct {
	Iteration int            `json:"iteration"`
	ToolName  string         `json:"tool_name"`
	Output    string         `json:"output"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// PhaseTrace stores one phase execution trace.
type PhaseTrace struct {
	Iteration int           `json:"iteration"`
	Phase     Phase         `json:"phase"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
}

// IterationMetric stores one completed runtime loop metric snapshot.
type IterationMetric struct {
	Iteration int           `json:"iteration"`
	Steps     int           `json:"steps"`
	ToolCalls int           `json:"tool_calls"`
	Elapsed   time.Duration `json:"elapsed"`
	Control   StageControl  `json:"control"`
	Timestamp time.Time     `json:"timestamp"`
}

// TraceState stores trace and debug details for observability.
type TraceState struct {
	RunStartedAt time.Time         `json:"run_started_at"`
	Phases       []PhaseTrace      `json:"phases,omitempty"`
	Iterations   []IterationMetric `json:"iterations,omitempty"`
	Debug        map[string]any    `json:"debug,omitempty"`
}

// AgentState is central mutable state for one run iteration pipeline.
type AgentState struct {
	RawInput         string            `json:"raw_input"`
	NormalizedInput  string            `json:"normalized_input"`
	Guardrails       GuardrailResults  `json:"guardrail_results"`
	Context          map[string]any    `json:"context,omitempty"`
	RetrievedDocs    []string          `json:"retrieved_docs,omitempty"`
	Memory           []Message         `json:"memory,omitempty"`
	ToolCallsHistory []ToolCallTrace   `json:"tool_calls_history,omitempty"`
	ToolResults      []ToolResultTrace `json:"tool_results,omitempty"`
	Errors           []string          `json:"errors,omitempty"`
	Budgets          BudgetState       `json:"budgets"`
	Trace            TraceState        `json:"trace"`
	Iteration        int               `json:"iteration"`
}

// NewAgentState creates a default run state from raw input.
func NewAgentState(rawInput string) *AgentState {
	now := time.Now().UTC()
	trimmed := strings.TrimSpace(rawInput)
	return &AgentState{
		RawInput:        rawInput,
		NormalizedInput: trimmed,
		Context:         map[string]any{},
		Trace: TraceState{
			RunStartedAt: now,
			Debug:        map[string]any{},
		},
	}
}

func (s *AgentState) appendErrorText(text string) {
	if s == nil {
		return
	}
	clean := strings.TrimSpace(text)
	if clean == "" {
		return
	}
	s.Errors = append(s.Errors, clean)
}
