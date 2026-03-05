package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ActionTypeTool  = "tool"
	ActionTypeFinal = "final"
	ActionTypeNoop  = "noop"
)

// Observation is planner input built from current run context.
type Observation struct {
	UserInput      string
	StateSnapshot  map[string]any
	Context        map[string]any
	RetrievedDocs  []string
	MemorySnippets []string
	ToolCatalog    []ToolSpec
}

// ToolSpec is planner-visible tool contract.
type ToolSpec struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	InputSchema  string `json:"input_schema"`
	OutputSchema string `json:"output_schema"`
}

// NextAction is planner output for the next loop step.
type NextAction struct {
	Done   bool   `json:"done"`
	Action Action `json:"action"`
}

// Action is one concrete action for the agent loop.
type Action struct {
	Type             string          `json:"type"`
	ToolName         string          `json:"tool_name,omitempty"`
	ToolArgs         json.RawMessage `json:"tool_args,omitempty"`
	ReasoningSummary string          `json:"reasoning_summary"`
	ExpectedOutcome  string          `json:"expected_outcome"`
	FinalResponse    string          `json:"final_response,omitempty"`
}

// ToolResult is normalized tool execution output.
type ToolResult struct {
	Output   string         `json:"output"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// FallbackFinalResponse creates a safe fallback output if no final answer exists.
func FallbackFinalResponse(stopReason string) string {
	reason := strings.TrimSpace(stopReason)
	if reason == "" {
		reason = "unknown_stop_reason"
	}
	return fmt.Sprintf("Agent stopped before producing a final response (%s).", reason)
}

// ActionFingerprint computes a compact hash used to detect repeated actions.
func ActionFingerprint(action Action) string {
	payload := map[string]any{
		"type": action.Type,
	}
	switch action.Type {
	case ActionTypeTool:
		payload["tool_name"] = strings.TrimSpace(action.ToolName)
		var args any = map[string]any{}
		if len(action.ToolArgs) > 0 {
			if err := json.Unmarshal(action.ToolArgs, &args); err != nil {
				args = strings.TrimSpace(string(action.ToolArgs))
			}
		}
		payload["tool_args"] = args
	case ActionTypeFinal:
		payload["final_response"] = strings.TrimSpace(action.FinalResponse)
	case ActionTypeNoop:
		// noop fingerprint only needs the action type.
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}
