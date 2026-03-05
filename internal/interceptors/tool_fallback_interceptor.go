package interceptors

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ToolFallbackInterceptor returns deterministic fallback payload for selected read-only tools.
type ToolFallbackInterceptor struct{}

// NewToolFallbackInterceptor creates deterministic fallback interceptor.
func NewToolFallbackInterceptor() core.ToolExecutionInterceptor {
	return &ToolFallbackInterceptor{}
}

func (i *ToolFallbackInterceptor) Name() string { return "ToolFallbackInterceptor" }

func (i *ToolFallbackInterceptor) AroundToolExecution(
	ctx context.Context,
	run *core.RunContext,
	call core.ToolCall,
	next core.ToolExecutionFunc,
) (core.ToolResult, error) {
	result, err := next(ctx, run, call)
	if err == nil {
		return result, nil
	}

	switch strings.TrimSpace(call.Name) {
	case ".service.lookup":
		service := "unknown-service"
		var input struct {
			Service string `json:"service"`
		}
		if len(call.Args) > 0 && json.Unmarshal(call.Args, &input) == nil {
			if strings.TrimSpace(input.Service) != "" {
				service = strings.TrimSpace(input.Service)
			}
		}
		payload := map[string]any{
			"service":           service,
			"tier":              "tier-3",
			"owner_team":        "noc",
			"pagerduty_service": "pd-noc-fallback",
			"runbook_id":        "rb-generic-investigation",
			"business_impact":   "Impact unknown; start generic investigation.",
			"dependencies":      []string{},
			"default_severity":  "sev3",
		}
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return core.ToolResult{}, marshalErr
		}
		return core.ToolResult{
			Output: string(raw),
			Metadata: map[string]any{
				"fallback": true,
				"reason":   "service_lookup_fallback",
			},
		}, nil

	case ".oncall.lookup":
		team := "noc"
		var input struct {
			Team string `json:"team"`
		}
		if len(call.Args) > 0 && json.Unmarshal(call.Args, &input) == nil {
			if strings.TrimSpace(input.Team) != "" {
				team = strings.TrimSpace(input.Team)
			}
		}
		payload := map[string]any{
			"team":        team,
			"primary":     "noc-primary",
			"secondary":   "noc-secondary",
			"timezone":    "UTC",
			"handoff_utc": "00:00",
		}
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return core.ToolResult{}, marshalErr
		}
		return core.ToolResult{
			Output: string(raw),
			Metadata: map[string]any{
				"fallback": true,
				"reason":   "oncall_lookup_fallback",
			},
		}, nil
	default:
		return core.ToolResult{}, err
	}
}
