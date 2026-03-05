package pipeline

import "strings"

// InputSanitizer детерминированно нормализует пользовательский ввод перед этапом observe.
type InputSanitizer struct{}

// Sanitize обрезает ввод и сводит пробелы к одиночным стабильным разделителям.
func (InputSanitizer) Sanitize(text string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", nil
	}
	return strings.Join(fields, " "), nil
}
