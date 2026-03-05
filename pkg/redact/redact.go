package redact

import "regexp"

// Набор регулярных выражений для маскирования чувствительных данных в логах и артефактах.
var (
	// keyValueSecretPattern находит секреты в формате key=value и маскирует значение.
	keyValueSecretPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|token|authorization|password|secret)\b\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	// jsonSecretPattern находит секретные поля в JSON-пейлоадах.
	jsonSecretPattern = regexp.MustCompile(`(?i)"(api[_-]?key|token|access[_-]?token|refresh[_-]?token|authorization|password|secret|client[_-]?secret|private[_-]?key)"\s*:\s*"[^"]*"`)
	// identityPattern находит идентификаторы пользователей в диагностических сообщениях.
	identityPattern = regexp.MustCompile(`(?i)\b(user[_-]?sub|subject|sub|user[_-]?id|uid)\b\s*[:=]\s*[^\s,;]+`)
	// bearerPattern находит bearer-токены в текстовых строках.
	bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	// jwtPattern находит JWT-токены по сигнатуре из трех частей.
	jwtPattern = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9._-]+\.[A-Za-z0-9._-]+\b`)
	// providerKeyPattern находит ключи провайдеров в формате sk-...
	providerKeyPattern = regexp.MustCompile(`\b(sk-[A-Za-z0-9]{12,})\b`)
	// emailPattern находит email-адреса для обезличивания логов.
	emailPattern = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	// querySecretPattern находит секреты в query-параметрах URL.
	querySecretPattern = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|token|access_token|key)=)([^&\s]+)`)
	// pemPrivateKeyPattern находит приватные PEM-ключи в многострочных строках.
	pemPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

// Text маскирует потенциально чувствительные данные в произвольном тексте.
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

// Error возвращает безопасное для логирования строковое представление ошибки.
func Error(err error) string {
	if err == nil {
		return ""
	}
	return Text(err.Error())
}
