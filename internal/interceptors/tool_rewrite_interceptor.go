package interceptors

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ToolRewriteInterceptor normalizes incident tool arguments with deterministic defaults.
type ToolRewriteInterceptor struct{}

// NewToolRewriteInterceptor creates argument normalization interceptor.
func NewToolRewriteInterceptor() core.ToolExecutionInterceptor {
	return &ToolRewriteInterceptor{}
}

func (i *ToolRewriteInterceptor) Name() string { return "ToolRewriteInterceptor" }

func (i *ToolRewriteInterceptor) AroundToolExecution(
	ctx context.Context,
	run *core.RunContext,
	call core.ToolCall,
	next core.ToolExecutionFunc,
) (core.ToolResult, error) {
	switch strings.TrimSpace(call.Name) {
	case ".incident.create":
		normalizedArgs := map[string]any{}
		if len(call.Args) > 0 {
			_ = json.Unmarshal(call.Args, &normalizedArgs)
		}
		if strings.TrimSpace(stringValue(normalizedArgs["severity"])) == "" {
			normalizedArgs["severity"] = inferSeverityFromContext(run, "sev3")
		}
		if strings.TrimSpace(stringValue(normalizedArgs["source"])) == "" {
			normalizedArgs["source"] = "internal.bundle"
		}
		if strings.TrimSpace(stringValue(normalizedArgs["summary"])) != "" {
			normalizedArgs["summary"] = truncate(strings.TrimSpace(stringValue(normalizedArgs["summary"])), 220)
		}
		raw, err := json.Marshal(normalizedArgs)
		if err != nil {
			return core.ToolResult{}, err
		}
		call.Args = raw

	case ".incident.update":
		normalizedArgs := map[string]any{}
		if len(call.Args) > 0 {
			_ = json.Unmarshal(call.Args, &normalizedArgs)
		}
		status := strings.TrimSpace(stringValue(normalizedArgs["status"]))
		note := strings.ToLower(strings.TrimSpace(stringValue(normalizedArgs["note"])))
		if status == "" {
			switch {
			case strings.Contains(note, "resolved"), strings.Contains(note, "fixed"), strings.Contains(note, "восстанов"):
				normalizedArgs["status"] = "resolved"
			case strings.Contains(note, "mitigat"), strings.Contains(note, "rollback"), strings.Contains(note, "смягч"):
				normalizedArgs["status"] = "mitigating"
			}
		}
		raw, err := json.Marshal(normalizedArgs)
		if err != nil {
			return core.ToolResult{}, err
		}
		call.Args = raw
	}
	return next(ctx, run, call)
}

func inferSeverityFromContext(run *core.RunContext, fallback string) string {
	if run == nil || run.State == nil || run.State.Context == nil {
		return fallback
	}
	contextData, ok := run.State.Context["IncidentContext"].(map[string]any)
	if !ok {
		return fallback
	}
	switch value := contextData["severity_hint"].(type) {
	case string:
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "sev1" || value == "sev2" || value == "sev3" || value == "sev4" {
			return value
		}
	}
	return fallback
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func truncate(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	return value[:maxChars]
}
