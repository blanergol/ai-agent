package output

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/xeipuuv/gojsonschema"
)

// SchemaValidator проверяет финальный ответ по строгой JSON Schema.
type SchemaValidator struct {
	// schema хранит скомпилированную схему для повторной проверки без лишних аллокаций.
	schema *gojsonschema.Schema
}

// NewSchemaValidator компилирует схему; пустая схема отключает структурную проверку.
func NewSchemaValidator(schema string) (*SchemaValidator, error) {
	if strings.TrimSpace(schema) == "" {
		return nil, nil
	}
	compiled, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(schema))
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "compile output json schema", err, false)
	}
	return &SchemaValidator{schema: compiled}, nil
}

// Validate проверяет, что ответ соответствует заданной JSON Schema.
func (v *SchemaValidator) Validate(_ context.Context, text string) error {
	if v == nil || v.schema == nil {
		return nil
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return apperrors.New(apperrors.CodeValidation, "empty final response", false)
	}

	// Сначала пробуем как JSON-структуру; при неуспехе валидируем как строку.
	var decoded any
	var result *gojsonschema.Result
	var err error
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &decoded); unmarshalErr == nil {
		result, err = v.schema.Validate(gojsonschema.NewGoLoader(decoded))
	} else {
		result, err = v.schema.Validate(gojsonschema.NewGoLoader(trimmed))
	}
	if err != nil {
		return apperrors.Wrap(apperrors.CodeValidation, "validate output json schema", err, false)
	}
	if result.Valid() {
		return nil
	}
	return apperrors.New(apperrors.CodeValidation, "final response does not match output schema", false)
}

// CompositeValidator объединяет несколько независимых валидаторов результата.
type CompositeValidator struct {
	// validators выполняются последовательно и прерываются на первой ошибке.
	validators []Validator
}

// Compose создаёт единый validator из набора независимых проверок.
func Compose(validators ...Validator) Validator {
	out := make([]Validator, 0, len(validators))
	for _, validator := range validators {
		if validator == nil {
			continue
		}
		out = append(out, validator)
	}
	if len(out) == 0 {
		return NewPolicyValidator(Policy{})
	}
	return &CompositeValidator{validators: out}
}

// Validate выполняет цепочку валидаторов до первой ошибки.
func (v *CompositeValidator) Validate(ctx context.Context, text string) error {
	if v == nil {
		return nil
	}
	for _, validator := range v.validators {
		if err := validator.Validate(ctx, text); err != nil {
			return err
		}
	}
	return nil
}
