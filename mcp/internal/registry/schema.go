package registry

import "encoding/json"

type Schema json.RawMessage

func (s Schema) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	return []byte(s), nil
}

func (s *Schema) UnmarshalJSON(data []byte) error {
	*s = append((*s)[:0], data...)
	return nil
}

func ObjectSchema(properties map[string]Schema, required ...string) Schema {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return mustSchema(schema)
}

func AnySchema(description string) Schema {
	if description == "" {
		return mustSchema(map[string]any{})
	}
	return mustSchema(map[string]any{"description": description})
}

func StringSchema(description string) Schema {
	return typedSchema("string", description)
}

func NullableStringSchema(description string) Schema {
	return typedSchema([]string{"string", "null"}, description)
}

func IntegerSchema(description string) Schema {
	return typedSchema("integer", description)
}

func NullableIntegerSchema(description string) Schema {
	return typedSchema([]string{"integer", "null"}, description)
}

func BoundedIntegerSchema(description string, minimum int, maximum int) Schema {
	return mustSchema(map[string]any{
		"type":        "integer",
		"description": description,
		"minimum":     minimum,
		"maximum":     maximum,
	})
}

func NullableBooleanSchema(description string) Schema {
	return typedSchema([]string{"boolean", "null"}, description)
}

func BooleanSchema(description string) Schema {
	return typedSchema("boolean", description)
}

func typedSchema(schemaType any, description string) Schema {
	return mustSchema(map[string]any{
		"type":        schemaType,
		"description": description,
	})
}

func mustSchema(value any) Schema {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return Schema(data)
}
