package output

import (
	"context"
	"testing"

	"github.com/blanergol/agent-core/pkg/apperrors"
)

// TestPolicyValidatorRejectsEmptyResponse проверяет отказ на пустом финальном ответе.
func TestPolicyValidatorRejectsEmptyResponse(t *testing.T) {
	v := NewPolicyValidator(Policy{})
	err := v.Validate(context.Background(), "   ")
	if err == nil {
		t.Fatalf("expected validation error for empty response")
	}
	if apperrors.CodeOf(err) != apperrors.CodeValidation {
		t.Fatalf("error code = %s, want %s", apperrors.CodeOf(err), apperrors.CodeValidation)
	}
}

// TestPolicyValidatorRejectsForbiddenSubstring проверяет блокировку запрещённой подстроки.
func TestPolicyValidatorRejectsForbiddenSubstring(t *testing.T) {
	v := NewPolicyValidator(Policy{ForbiddenSubstrings: []string{"internal-only"}})
	err := v.Validate(context.Background(), "This is INTERNAL-only data")
	if err == nil {
		t.Fatalf("expected forbidden content error")
	}
	if apperrors.CodeOf(err) != apperrors.CodeForbidden {
		t.Fatalf("error code = %s, want %s", apperrors.CodeOf(err), apperrors.CodeForbidden)
	}
}

// TestPolicyValidatorRejectsSecretLeakagePattern проверяет детектирование утечки секретов.
func TestPolicyValidatorRejectsSecretLeakagePattern(t *testing.T) {
	v := NewPolicyValidator(Policy{})
	err := v.Validate(context.Background(), "authorization: Bearer SECRET123")
	if err == nil {
		t.Fatalf("expected secret leakage error")
	}
	if apperrors.CodeOf(err) != apperrors.CodeForbidden {
		t.Fatalf("error code = %s, want %s", apperrors.CodeOf(err), apperrors.CodeForbidden)
	}
}

// TestPolicyValidatorAcceptsSafeResponse проверяет прохождение безопасного ответа.
func TestPolicyValidatorAcceptsSafeResponse(t *testing.T) {
	v := NewPolicyValidator(Policy{MaxChars: 100, ForbiddenSubstrings: []string{"forbidden"}})
	if err := v.Validate(context.Background(), "Safe answer for user."); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestSchemaValidatorRejectsMismatchedShape проверяет отказ при несовпадении JSON-формы.
func TestSchemaValidatorRejectsMismatchedShape(t *testing.T) {
	v, err := NewSchemaValidator(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"}}}`)
	if err != nil {
		t.Fatalf("new schema validator: %v", err)
	}
	if err := v.Validate(context.Background(), "plain text"); err == nil {
		t.Fatalf("expected schema validation error")
	}
}

// TestSchemaValidatorAcceptsJSONStringForStringSchema проверяет, что JSON-подобный текст
// не отвергается, когда схема требует строку.
func TestSchemaValidatorAcceptsJSONStringForStringSchema(t *testing.T) {
	v, err := NewSchemaValidator(`{"type":"string","minLength":1}`)
	if err != nil {
		t.Fatalf("new schema validator: %v", err)
	}
	if err := v.Validate(context.Background(), `{"answer":"ok"}`); err != nil {
		t.Fatalf("unexpected schema validation error: %v", err)
	}
}

// TestSchemaValidatorAcceptsObjectJSON проверяет позитивный кейс для object-схемы.
func TestSchemaValidatorAcceptsObjectJSON(t *testing.T) {
	v, err := NewSchemaValidator(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"}}}`)
	if err != nil {
		t.Fatalf("new schema validator: %v", err)
	}
	if err := v.Validate(context.Background(), `{"answer":"ok"}`); err != nil {
		t.Fatalf("unexpected schema validation error: %v", err)
	}
}

// TestComposeRunsPolicyAndSchema проверяет совместную работу policy и schema валидаторов.
func TestComposeRunsPolicyAndSchema(t *testing.T) {
	policy := NewPolicyValidator(Policy{ForbiddenSubstrings: []string{"forbidden"}})
	schema, err := NewSchemaValidator(`{"type":"string","minLength":1}`)
	if err != nil {
		t.Fatalf("new schema validator: %v", err)
	}
	validator := Compose(policy, schema)
	if err := validator.Validate(context.Background(), "forbidden output"); err == nil {
		t.Fatalf("expected policy rejection")
	}
}
