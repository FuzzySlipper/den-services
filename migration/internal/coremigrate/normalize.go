package coremigrate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

func normalizeForPostgres(value any, column targetColumn) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch column.DataType {
	case "boolean":
		return normalizeBool(value)
	case "timestamp with time zone":
		return normalizeTime(value)
	case "jsonb", "json":
		return normalizeJSON(value)
	default:
		return normalizeScalar(value), nil
	}
}

func canonicalValue(value any, column targetColumn) (string, error) {
	if value == nil {
		return "null", nil
	}
	normalized, err := normalizeForPostgres(value, column)
	if err != nil {
		return "", err
	}
	switch typed := normalized.(type) {
	case nil:
		return "null", nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano), nil
	case string:
		if column.DataType == "jsonb" || column.DataType == "json" {
			return canonicalJSON(typed)
		}
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return fmt.Sprint(typed), nil
	}
}

func normalizeScalar(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	default:
		return typed
	}
}

func normalizeBool(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case int64:
		if typed == 0 {
			return false, nil
		}
		if typed == 1 {
			return true, nil
		}
	case int:
		if typed == 0 {
			return false, nil
		}
		if typed == 1 {
			return true, nil
		}
	case float64:
		if typed == 0 {
			return false, nil
		}
		if typed == 1 {
			return true, nil
		}
	case []byte:
		return normalizeBoolString(string(typed))
	case string:
		return normalizeBoolString(typed)
	}
	return false, fmt.Errorf("invalid boolean value %v", value)
}

func normalizeBoolString(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y":
		return true, nil
	case "0", "false", "f", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

func normalizeTime(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case []byte:
		return parseCoreTime(string(typed))
	case string:
		return parseCoreTime(typed)
	default:
		return time.Time{}, fmt.Errorf("invalid timestamp value %v", value)
	}
}

func parseCoreTime(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	formatsWithZone := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05-07",
	}
	for _, layout := range formatsWithZone {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC(), nil
		}
	}
	formatsUTC := []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range formatsUTC {
		if parsed, err := time.ParseInLocation(layout, trimmed, time.UTC); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}

func normalizeJSON(value any) (string, error) {
	switch typed := value.(type) {
	case []byte:
		return canonicalJSON(string(typed))
	case string:
		return canonicalJSON(typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", fmt.Errorf("marshaling json value: %w", err)
		}
		return canonicalJSON(string(data))
	}
}

func canonicalJSON(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("empty json value")
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return "", fmt.Errorf("parsing json: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("json value has trailing data")
	}
	normalized := normalizeJSONNumbers(decoded)
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("canonicalizing json: %w", err)
	}
	return string(data), nil
}

func normalizeJSONNumbers(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			typed[key] = normalizeJSONNumbers(child)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = normalizeJSONNumbers(child)
		}
		return typed
	case json.Number:
		if strings.ContainsAny(typed.String(), ".eE") {
			floatValue, err := typed.Float64()
			if err == nil && !math.IsInf(floatValue, 0) && !math.IsNaN(floatValue) {
				return floatValue
			}
			return typed.String()
		}
		intValue, err := strconv.ParseInt(typed.String(), 10, 64)
		if err == nil {
			return intValue
		}
		return typed.String()
	default:
		return typed
	}
}

func scanValuePointers(values []any) []any {
	pointers := make([]any, len(values))
	for index := range values {
		pointers[index] = &values[index]
	}
	return pointers
}

func nullableString(value any) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	switch typed := value.(type) {
	case string:
		return sql.NullString{String: typed, Valid: true}
	case []byte:
		return sql.NullString{String: string(typed), Valid: true}
	default:
		return sql.NullString{String: fmt.Sprint(typed), Valid: true}
	}
}
