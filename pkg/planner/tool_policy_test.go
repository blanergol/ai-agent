package planner

import "testing"

// TestStrictToolSelectionPolicyRejectsKVWithoutStateIntent проверяет запрет KV-инструментов без явного намерения.
func TestStrictToolSelectionPolicyRejectsKVWithoutStateIntent(t *testing.T) {
	policy := StrictToolSelectionPolicy{}
	err := policy.ValidateSelection(
		Observation{
			UserInput: "Как звали Пушкина",
			ToolCatalog: []ToolSpec{
				{Name: "kv.get"},
				{Name: "time.now"},
			},
		},
		Action{
			Type:             "tool",
			ToolName:         "kv.get",
			ReasoningSummary: "read state",
			ExpectedOutcome:  "value",
		},
	)
	if err == nil {
		t.Fatalf("expected kv selection to be rejected")
	}
}

// TestStrictToolSelectionPolicyAllowsKVWithStateIntent проверяет разрешение KV-инструментов при явном намерении.
func TestStrictToolSelectionPolicyAllowsKVWithStateIntent(t *testing.T) {
	policy := StrictToolSelectionPolicy{}
	err := policy.ValidateSelection(
		Observation{
			UserInput: "Сохрани ключ answer со значением 42 в state",
			ToolCatalog: []ToolSpec{
				{Name: "kv.put"},
			},
		},
		Action{
			Type:             "tool",
			ToolName:         "kv.put",
			ReasoningSummary: "store state",
			ExpectedOutcome:  "value persisted",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStrictToolSelectionPolicyAllowsNonKVTool проверяет, что не-KV инструменты не блокируются policy.
func TestStrictToolSelectionPolicyAllowsNonKVTool(t *testing.T) {
	policy := StrictToolSelectionPolicy{}
	err := policy.ValidateSelection(
		Observation{
			UserInput: "Как звали Пушкина",
			ToolCatalog: []ToolSpec{
				{Name: "time.now"},
			},
		},
		Action{
			Type:             "tool",
			ToolName:         "time.now",
			ReasoningSummary: "get time",
			ExpectedOutcome:  "timestamp",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
