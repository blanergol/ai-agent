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
	OnCallLookupToolName = ".oncall.lookup"
)

// OnCallLookupTool returns deterministic on-call contacts for one team.
type OnCallLookupTool struct {
	Store *IncidentStore
}

var _ toolkit.Tool = (*OnCallLookupTool)(nil)
var _ toolkit.RetryPolicy = (*OnCallLookupTool)(nil)
var _ toolkit.CachePolicy = (*OnCallLookupTool)(nil)

func NewOnCallLookupTool(store *IncidentStore) *OnCallLookupTool {
	if store == nil {
		store = NewIncidentStore()
	}
	return &OnCallLookupTool{Store: store}
}

func (t *OnCallLookupTool) Name() string { return OnCallLookupToolName }

func (t *OnCallLookupTool) Description() string {
	return "Returns deterministic primary/secondary on-call contacts for escalation."
}

func (t *OnCallLookupTool) InputSchema() string {
	return `{"type":"object","required":["team"],"properties":{"team":{"type":"string","minLength":1}},"additionalProperties":false}`
}

func (t *OnCallLookupTool) OutputSchema() string {
	return `{"type":"object","required":["team","primary","secondary","timezone","handoff_utc"],"properties":{"team":{"type":"string"},"primary":{"type":"string"},"secondary":{"type":"string"},"timezone":{"type":"string"},"handoff_utc":{"type":"string"}},"additionalProperties":false}`
}

func (t *OnCallLookupTool) IsReadOnly() bool { return true }

func (t *OnCallLookupTool) IsSafeRetry() bool { return true }

func (t *OnCallLookupTool) CacheTTL() time.Duration { return 3 * time.Minute }

func (t *OnCallLookupTool) Execute(_ context.Context, args json.RawMessage) (toolkit.ToolResult, error) {
	var input struct {
		Team string `json:"team"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return toolkit.ToolResult{}, err
	}
	team := strings.TrimSpace(input.Team)
	if team == "" {
		return toolkit.ToolResult{}, fmt.Errorf("team is required")
	}
	shift, err := t.Store.LookupOnCall(team)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	payload := map[string]any{
		"team":        shift.Team,
		"primary":     shift.Primary,
		"secondary":   shift.Secondary,
		"timezone":    shift.Timezone,
		"handoff_utc": shift.HandoffUTC,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	return toolkit.ToolResult{Output: string(raw)}, nil
}
