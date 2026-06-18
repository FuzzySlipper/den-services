package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Values struct {
	values map[string]string
}

func Load(paths ...string) (Values, error) {
	values := fromEnvironment()
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		fileValues, err := readEnvFile(path)
		if err != nil {
			return Values{}, err
		}
		for key, value := range fileValues {
			values[key] = value
		}
	}
	return Values{values: values}, nil
}

func FromMap(values map[string]string) Values {
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return Values{values: copyValues}
}

func (v Values) String(name string, defaultValue string) string {
	if value, ok := v.values[name]; ok {
		return value
	}
	return defaultValue
}

func (v Values) RequiredString(name string) (string, error) {
	value := strings.TrimSpace(v.String(name, ""))
	if value == "" {
		return "", fmt.Errorf("%w: %s", ErrMissingValue, name)
	}
	return value, nil
}

func (v Values) Bool(name string, defaultValue bool) (bool, error) {
	value, ok := v.values[name]
	if !ok || strings.TrimSpace(value) == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%w: %s: %w", ErrInvalidValue, name, err)
	}
	return parsed, nil
}

func (v Values) Int(name string, defaultValue int) (int, error) {
	value, ok := v.values[name]
	if !ok || strings.TrimSpace(value) == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrInvalidValue, name, err)
	}
	return parsed, nil
}

func (v Values) Duration(name string, defaultValue time.Duration) (time.Duration, error) {
	value, ok := v.values[name]
	if !ok || strings.TrimSpace(value) == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrInvalidValue, name, err)
	}
	return parsed, nil
}

func (v Values) Expand(value string) (string, error) {
	var missing []string
	expanded := os.Expand(value, func(name string) string {
		if envValue, ok := v.values[name]; ok {
			return envValue
		}
		missing = append(missing, name)
		return ""
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%w: %s", ErrMissingValue, strings.Join(missing, ","))
	}
	return expanded, nil
}

func fromEnvironment() map[string]string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening env file %s: %w", path, err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%w: %s:%d", ErrInvalidEnvFileLine, path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%w: %s:%d", ErrInvalidEnvFileLine, path, lineNumber)
		}
		value, err := parseEnvFileValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("%w: %s:%d: %w", ErrInvalidEnvFileLine, path, lineNumber, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading env file %s: %w", path, err)
	}
	return values, nil
}

func parseEnvFileValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, `"`) || strings.HasPrefix(raw, `'`) {
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", err
		}
		return value, nil
	}
	if beforeComment, _, ok := strings.Cut(raw, " #"); ok {
		return strings.TrimSpace(beforeComment), nil
	}
	return raw, nil
}

var (
	ErrMissingValue       = errors.New("missing config value")  //nolint:gochecknoglobals
	ErrInvalidValue       = errors.New("invalid config value")  //nolint:gochecknoglobals
	ErrInvalidEnvFileLine = errors.New("invalid env file line") //nolint:gochecknoglobals
)
