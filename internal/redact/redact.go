package redact

import "regexp"

var (
	// keyValueSecretPattern маскирует пары ключ=значение с потенциальными секретами.
	keyValueSecretPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|token|authorization|password|secret)\b\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	// jsonSecretPattern маскирует значения чувствительных ключей в JSON-фрагментах.
	jsonSecretPattern = regexp.MustCompile(`(?i)"(api[_-]?key|token|access[_-]?token|refresh[_-]?token|authorization|password|secret|client[_-]?secret|private[_-]?key)"\s*:\s*"[^"]*"`)
	// identityPattern маскирует пользовательские идентификаторы и subject-поля.
	identityPattern = regexp.MustCompile(`(?i)\b(user[_-]?sub|subject|sub|user[_-]?id|uid)\b\s*[:=]\s*[^\s,;]+`)
	// bearerPattern маскирует bearer-токены в свободном тексте и заголовках.
	bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	// jwtPattern маскирует JWT-like токены даже без явного ключа.
	jwtPattern = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9._-]+\.[A-Za-z0-9._-]+\b`)
	// providerKeyPattern маскирует распространённые provider-ключи (например, OpenAI).
	providerKeyPattern = regexp.MustCompile(`\b(sk-[A-Za-z0-9]{12,})\b`)
	// emailPattern маскирует e-mail адреса как персональные данные.
	emailPattern = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	// querySecretPattern маскирует токены в query-параметрах URL.
	querySecretPattern = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|token|access_token|key)=)([^&\s]+)`)
	// pemPrivateKeyPattern маскирует целиком PEM-блоки приватных ключей.
	pemPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

// Text редактирует потенциально чувствительные значения в произвольной строке.
func Text(input string) string {
	if input == "" {
		return input
	}
	output := pemPrivateKeyPattern.ReplaceAllString(input, "[REDACTED_PRIVATE_KEY]")
	output = keyValueSecretPattern.ReplaceAllString(output, "$1=[REDACTED]")
	output = jsonSecretPattern.ReplaceAllString(output, `"$1":"[REDACTED]"`)
	output = identityPattern.ReplaceAllString(output, "$1=[REDACTED]")
	output = bearerPattern.ReplaceAllString(output, "Bearer [REDACTED]")
	output = jwtPattern.ReplaceAllString(output, "[REDACTED_JWT]")
	output = providerKeyPattern.ReplaceAllString(output, "[REDACTED_KEY]")
	output = emailPattern.ReplaceAllString(output, "[REDACTED_EMAIL]")
	output = querySecretPattern.ReplaceAllString(output, "$1[REDACTED]")
	return output
}

// Error возвращает безопасное текстовое представление ошибки для логирования.
func Error(err error) string {
	if err == nil {
		return ""
	}
	return Text(err.Error())
}
