package redact

import (
	"strings"
	"testing"
)

// TestTextMasksCommonSecrets проверяет, что распространённые секреты редактируются в логах.
func TestTextMasksCommonSecrets(t *testing.T) {
	input := "token=abc123 Authorization: Bearer SECRET123 api_key=KEY123"
	output := Text(input)
	if output == input {
		t.Fatalf("secret redaction was not applied")
	}
	if containsAny(output, []string{"abc123", "SECRET123", "KEY123"}) {
		t.Fatalf("raw secrets leaked in output: %s", output)
	}
}

// TestTextMasksQuerySecrets проверяет редактирование токенов в URL query-параметрах.
func TestTextMasksQuerySecrets(t *testing.T) {
	input := "https://service.local/path?access_token=XYZ&ok=1"
	output := Text(input)
	if output == input {
		t.Fatalf("query token redaction was not applied")
	}
	if containsAny(output, []string{"XYZ"}) {
		t.Fatalf("raw query token leaked in output: %s", output)
	}
}

// TestTextMasksIdentityAndJWT проверяет редактирование user identifiers и JWT-like токенов.
func TestTextMasksIdentityAndJWT(t *testing.T) {
	input := "user_sub=alice@example.com token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig"
	output := Text(input)
	if output == input {
		t.Fatalf("identity/jwt redaction was not applied")
	}
	if containsAny(output, []string{"alice@example.com", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"}) {
		t.Fatalf("raw identity/jwt leaked in output: %s", output)
	}
}

// TestTextMasksJSONSecretsAndPrivateKeys проверяет редактирование нетиповых секретов в JSON и PEM-блоках.
func TestTextMasksJSONSecretsAndPrivateKeys(t *testing.T) {
	input := `{"client_secret":"s3cr3t","token":"abc","nested":{"private_key":"-----BEGIN PRIVATE KEY-----ABC-----END PRIVATE KEY-----"}}`
	output := Text(input)
	if output == input {
		t.Fatalf("json/pem redaction was not applied")
	}
	if containsAny(output, []string{"s3cr3t", `"token":"abc"`, "BEGIN PRIVATE KEY", "END PRIVATE KEY"}) {
		t.Fatalf("raw json/pem secrets leaked in output: %s", output)
	}
}

func containsAny(text string, fragments []string) bool {
	for _, fragment := range fragments {
		if fragment != "" && strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}
