package helpers

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvString возвращает строку из окружения или fallback при отсутствии ключа.
func EnvString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return fallback
}

// EnvCSV разбирает comma-separated значение в массив строк без пустых элементов.
func EnvCSV(key string) ([]string, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return nil, false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, true
	}
	// items содержит сырые элементы списка до trim и фильтрации.
	items := strings.Split(raw, ",")
	// out собирает нормализованный список значений.
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out, true
}

// EnvInt читает обязательный целочисленный override и возвращает флаг наличия.
func EnvInt(key string) (int, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return 0, false, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, true, nil
}

// EnvInt64 аналогичен envInt, но для диапазона int64.
func EnvInt64(key string) (int64, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return 0, false, nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, true, nil
}

// EnvFloat64 читает вещественное значение и валидирует формат.
func EnvFloat64(key string) (float64, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, true, nil
}

// EnvBool читает логическое значение из окружения.
func EnvBool(key string) (bool, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false, nil
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, true, nil
}
