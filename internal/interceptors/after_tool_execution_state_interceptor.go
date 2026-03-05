package interceptors

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// AfterToolExecutionStateInterceptor derives compact incident memory after tool execution.
type AfterToolExecutionStateInterceptor struct{}

// NewAfterToolExecutionStateInterceptor creates post-tool state sync interceptor.
func NewAfterToolExecutionStateInterceptor() core.PhaseInterceptor {
	return &AfterToolExecutionStateInterceptor{}
}

func (i *AfterToolExecutionStateInterceptor) Name() string {
	return "AfterToolExecutionStateInterceptor"
}

func (i *AfterToolExecutionStateInterceptor) BeforePhase(_ context.Context, _ *core.RunContext, _ core.Phase) error {
	return nil
}

func (i *AfterToolExecutionStateInterceptor) AfterPhase(
	_ context.Context,
	run *core.RunContext,
	_ core.Phase,
	_ error,
) error {
	if run == nil || run.State == nil {
		return nil
	}
	if run.State.Context == nil {
		run.State.Context = map[string]any{}
	}

	lastTool := ""
	lastToolError := ""
	if size := len(run.State.ToolCallsHistory); size > 0 {
		last := run.State.ToolCallsHistory[size-1]
		lastTool = strings.TrimSpace(last.ToolName)
		lastToolError = strings.TrimSpace(last.Error)
	}

	incidentMemory := map[string]any{}
	if size := len(run.State.ToolResults); size > 0 {
		lastResult := run.State.ToolResults[size-1]
		values := map[string]any{}
		if err := json.Unmarshal([]byte(lastResult.Output), &values); err == nil {
			for _, key := range []string{"incident_id", "service", "status", "severity", "assignee_team"} {
				if value, ok := values[key]; ok {
					incidentMemory[key] = value
				}
			}
		}
	}
	if len(incidentMemory) > 0 {
		run.State.Context["IncidentMemory"] = incidentMemory
		if rawID, ok := incidentMemory["incident_id"].(string); ok && strings.TrimSpace(rawID) != "" {
			run.State.RetrievedDocs = append(run.State.RetrievedDocs, "Latest incident context in memory: "+strings.TrimSpace(rawID))
		}
	}

	run.State.Context["AfterToolExecutionState"] = map[string]any{
		"tool_calls_total": len(run.State.ToolCallsHistory),
		"tool_results":     len(run.State.ToolResults),
		"last_tool":        lastTool,
		"last_tool_error":  lastToolError,
	}
	return nil
}
