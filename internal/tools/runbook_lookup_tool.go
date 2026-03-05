package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

const (
	RunbookLookupToolName = ".runbook.lookup"
)

// RunbookLookupTool returns deterministic runbook steps for a scenario.
type RunbookLookupTool struct {
	Store *IncidentStore
}

var _ toolkit.Tool = (*RunbookLookupTool)(nil)
var _ toolkit.RetryPolicy = (*RunbookLookupTool)(nil)
var _ toolkit.CachePolicy = (*RunbookLookupTool)(nil)

func NewRunbookLookupTool(store *IncidentStore) *RunbookLookupTool {
	if store == nil {
		store = NewIncidentStore()
	}
	return &RunbookLookupTool{Store: store}
}

func (t *RunbookLookupTool) Name() string { return RunbookLookupToolName }

func (t *RunbookLookupTool) Description() string {
	return "Returns scenario-specific operational runbook steps for incident response."
}

func (t *RunbookLookupTool) InputSchema() string {
	return `{"type":"object","required":["runbook_id"],"properties":{"runbook_id":{"type":"string","minLength":1},"scenario":{"type":"string"}},"additionalProperties":false}`
}

func (t *RunbookLookupTool) OutputSchema() string {
	return `{"type":"object","required":["runbook_id","title","scenario","steps","verification"],"properties":{"runbook_id":{"type":"string"},"title":{"type":"string"},"scenario":{"type":"string"},"steps":{"type":"array","items":{"type":"string"}},"verification":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`
}

func (t *RunbookLookupTool) IsReadOnly() bool { return true }

func (t *RunbookLookupTool) IsSafeRetry() bool { return true }

func (t *RunbookLookupTool) CacheTTL() time.Duration { return 5 * time.Minute }

func (t *RunbookLookupTool) Execute(_ context.Context, args json.RawMessage) (toolkit.ToolResult, error) {
	var input struct {
		RunbookID string `json:"runbook_id"`
		Scenario  string `json:"scenario"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return toolkit.ToolResult{}, err
	}
	runbookID := strings.TrimSpace(input.RunbookID)
	if runbookID == "" {
		return toolkit.ToolResult{}, fmt.Errorf("runbook_id is required")
	}
	runbook, scenario, steps, err := t.Store.LookupRunbook(runbookID, input.Scenario)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	payload := map[string]any{
		"runbook_id":   runbook.ID,
		"title":        runbook.Title,
		"scenario":     scenario,
		"steps":        steps,
		"verification": runbook.Verification,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	return toolkit.ToolResult{Output: string(raw)}, nil
}
