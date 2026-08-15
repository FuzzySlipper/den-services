package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type commandSchema struct {
	Type                 json.RawMessage           `json:"type"`
	Properties           map[string]propertySchema `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties any                       `json:"additionalProperties"`
}

type propertySchema struct {
	Type json.RawMessage `json:"type"`
}

func handleDen(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeDenUsageError(stderr, "an operation is required")
		return 2
	}
	catalog, err := cliCatalog()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	tool, found := catalog.Find("den." + args[0])
	if !found || len(tool.InputSchema) == 0 {
		writeDenUsageError(stderr, fmt.Sprintf("unknown Den operation %q", args[0]))
		return 2
	}
	arguments, err := parseToolArguments(tool.InputSchema, args[1:])
	if err != nil {
		writeDenUsageError(stderr, err.Error())
		return 2
	}
	client, err := MCPClientFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	result, isError, err := client.Call(context.Background(), tool.Operation, arguments)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if code := writeRawJSON(stdout, result); code != 0 {
		return code
	}
	if isError {
		_, _ = fmt.Fprintf(stderr, "den-tool: Den operation %s returned an error\n", tool.Operation)
		return 1
	}
	return 0
}

func parseToolArguments(rawSchema json.RawMessage, args []string) (json.RawMessage, error) {
	var schema commandSchema
	decoder := json.NewDecoder(bytes.NewReader(rawSchema))
	if err := decoder.Decode(&schema); err != nil {
		return nil, fmt.Errorf("invalid embedded input schema: %w", err)
	}
	if len(args) == 2 && args[0] == "--args-json" {
		return validateArgumentsJSON(schema, []byte(args[1]))
	}
	if len(args) == 1 && strings.HasPrefix(args[0], "--args-json=") {
		return validateArgumentsJSON(schema, []byte(strings.TrimPrefix(args[0], "--args-json=")))
	}
	for _, argument := range args {
		if argument == "--args-json" || strings.HasPrefix(argument, "--args-json=") {
			return nil, fmt.Errorf("--args-json cannot be combined with field flags")
		}
	}

	values := make(map[string]any)
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "--") || argument == "--" {
			return nil, fmt.Errorf("expected --field value, got %q", argument)
		}
		nameValue := strings.TrimPrefix(argument, "--")
		name, value, hasValue := strings.Cut(nameValue, "=")
		name = canonicalFieldName(name, schema.Properties)
		property, exists := schema.Properties[name]
		if !exists {
			return nil, fmt.Errorf("unknown argument --%s", strings.Split(nameValue, "=")[0])
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("--%s may only be provided once", name)
		}
		if !hasValue {
			index++
			if index >= len(args) {
				return nil, fmt.Errorf("--%s requires a value", name)
			}
			value = args[index]
		}
		parsed, err := parsePropertyValue(property.Type, value)
		if err != nil {
			return nil, fmt.Errorf("--%s: %w", name, err)
		}
		values[name] = parsed
	}
	return validateAndMarshalArguments(schema, values)
}

func canonicalFieldName(name string, properties map[string]propertySchema) string {
	if _, exists := properties[name]; exists {
		return name
	}
	underscored := strings.ReplaceAll(name, "-", "_")
	if _, exists := properties[underscored]; exists {
		return underscored
	}
	return name
}

func parsePropertyValue(rawType json.RawMessage, value string) (any, error) {
	types := schemaTypes(rawType)
	if value == "null" && containsSchemaType(types, "null") {
		return nil, nil
	}
	if containsSchemaType(types, "string") {
		return value, nil
	}
	if containsSchemaType(types, "integer") {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("must be an integer")
		}
		return parsed, nil
	}
	if containsSchemaType(types, "number") {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("must be a number")
		}
		return parsed, nil
	}
	if containsSchemaType(types, "boolean") {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("must be true or false")
		}
		return parsed, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil, fmt.Errorf("must be valid JSON")
	}
	return parsed, nil
}

func schemaTypes(raw json.RawMessage) []string {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return []string{single}
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) == nil {
		return multiple
	}
	return nil
}

func containsSchemaType(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateArgumentsJSON(schema commandSchema, data []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("--args-json must be a JSON object: %w", err)
	}
	if values == nil {
		return nil, fmt.Errorf("--args-json must be a JSON object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("--args-json contains trailing data")
	}
	for name, value := range values {
		property, exists := schema.Properties[name]
		if !exists {
			return nil, fmt.Errorf("unknown argument %q", name)
		}
		if err := validateJSONType(property.Type, value); err != nil {
			return nil, fmt.Errorf("argument %q %w", name, err)
		}
	}
	return validateAndMarshalArguments(schema, values)
}

func validateJSONType(rawType json.RawMessage, value any) error {
	types := schemaTypes(rawType)
	if len(types) == 0 {
		return nil
	}
	if value == nil && containsSchemaType(types, "null") {
		return nil
	}
	switch typedValue := value.(type) {
	case string:
		if containsSchemaType(types, "string") {
			return nil
		}
	case bool:
		if containsSchemaType(types, "boolean") {
			return nil
		}
	case []any:
		if containsSchemaType(types, "array") {
			return nil
		}
	case map[string]any:
		if containsSchemaType(types, "object") {
			return nil
		}
	case json.Number:
		if containsSchemaType(types, "number") {
			return nil
		}
		if containsSchemaType(types, "integer") && !strings.ContainsAny(typedValue.String(), ".eE") {
			return nil
		}
	}
	return fmt.Errorf("has the wrong JSON type")
}

func validateAndMarshalArguments(schema commandSchema, values map[string]any) (json.RawMessage, error) {
	missing := make([]string, 0)
	for _, required := range schema.Required {
		if _, exists := values[required]; !exists {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("missing required arguments: %s", strings.Join(missing, ", "))
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode arguments: %w", err)
	}
	return data, nil
}

func writeDenUsageError(writer io.Writer, message string) {
	_, _ = fmt.Fprintf(writer, "den-tool: %s\nusage: den-tool den <operation> [--field value ... | --args-json '{...}']\n", message)
}

func writeRawJSON(writer io.Writer, value json.RawMessage) int {
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		return 1
	}
	_, err := fmt.Fprintln(writer, compact.String())
	if err != nil {
		return 1
	}
	return 0
}
