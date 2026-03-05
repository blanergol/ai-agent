package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MCPEnrichmentSource configures one deterministic MCP enrichment call.
type MCPEnrichmentSource struct {
	Name     string          `json:"name"`
	ToolName string          `json:"tool_name"`
	Args     json.RawMessage `json:"args,omitempty"`
	Required bool            `json:"required,omitempty"`
}

// MCPContextEnrichment deterministically fetches context from configured MCP tools.
type MCPContextEnrichment struct {
	sources []MCPEnrichmentSource
}

// NewMCPContextEnrichment creates deterministic MCP enrichment interceptor.
func NewMCPContextEnrichment(sources []MCPEnrichmentSource) *MCPContextEnrichment {
	if len(sources) == 0 {
		return &MCPContextEnrichment{}
	}
	out := make([]MCPEnrichmentSource, 0, len(sources))
	for _, source := range sources {
		toolName := strings.TrimSpace(source.ToolName)
		if toolName == "" {
			continue
		}
		args := append(json.RawMessage(nil), source.Args...)
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		out = append(out, MCPEnrichmentSource{
			Name:     strings.TrimSpace(source.Name),
			ToolName: toolName,
			Args:     args,
			Required: source.Required,
		})
	}
	return &MCPContextEnrichment{sources: out}
}

// Name returns interceptor stable name.
func (m *MCPContextEnrichment) Name() string { return "MCPContextEnrichment" }

// BeforePhase executes deterministic MCP enrichment during ENRICH_CONTEXT.
func (m *MCPContextEnrichment) BeforePhase(ctx context.Context, run *RunContext, phase Phase) error {
	if m == nil || run == nil || phase != PhaseEnrichContext || len(m.sources) == 0 {
		return nil
	}
	if run.Deps.ToolExecutor == nil {
		return fmt.Errorf("tool executor is not initialized for mcp enrichment")
	}
	run.ensureState()
	enriched := map[string]any{}
	for _, source := range m.sources {
		call := ToolCall{
			Name:      source.ToolName,
			Args:      append(json.RawMessage(nil), source.Args...),
			Source:    "mcp_enrichment",
			Iteration: run.State.Iteration,
		}
		result, err := run.Deps.ToolExecutor.Execute(ctx, run, call)
		if err != nil {
			run.State.appendErrorText(fmt.Sprintf("mcp enrichment %s: %v", source.ToolName, err))
			if source.Required {
				return err
			}
			continue
		}
		key := strings.TrimSpace(source.Name)
		if key == "" {
			key = source.ToolName
		}
		enriched[key] = result.Output
		if out := strings.TrimSpace(result.Output); out != "" {
			run.State.RetrievedDocs = append(run.State.RetrievedDocs, out)
		}
	}
	if len(enriched) == 0 {
		return nil
	}
	current, _ := run.State.Context["mcp_enrichment"].(map[string]any)
	if current == nil {
		current = map[string]any{}
	}
	for k, v := range enriched {
		current[k] = v
	}
	run.State.Context["mcp_enrichment"] = current
	return nil
}

// AfterPhase is a no-op for enrichment interceptor.
func (m *MCPContextEnrichment) AfterPhase(_ context.Context, _ *RunContext, _ Phase, _ error) error {
	return nil
}
