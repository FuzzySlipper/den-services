package registry

import "encoding/json"

type SchemaType []string

func (t SchemaType) MarshalJSON() ([]byte, error) {
	if len(t) == 1 {
		return json.Marshal(t[0])
	}
	return json.Marshal([]string(t))
}

type Schema struct {
	Type                 SchemaType        `json:"type,omitempty"`
	Description          string            `json:"description,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	AdditionalProperties *bool             `json:"additionalProperties,omitempty"`
	Minimum              *int              `json:"minimum,omitempty"`
	Maximum              *int              `json:"maximum,omitempty"`
}

func ObjectSchema(properties map[string]Schema, required ...string) Schema {
	additionalProperties := false
	return Schema{
		Type:                 SchemaType{"object"},
		Properties:           properties,
		Required:             required,
		AdditionalProperties: &additionalProperties,
	}
}

func AnySchema(description string) Schema {
	return Schema{Description: description}
}

func StringSchema(description string) Schema {
	return Schema{
		Type:        SchemaType{"string"},
		Description: description,
	}
}

func NullableStringSchema(description string) Schema {
	return Schema{
		Type:        SchemaType{"string", "null"},
		Description: description,
	}
}

func IntegerSchema(description string) Schema {
	return Schema{
		Type:        SchemaType{"integer"},
		Description: description,
	}
}

func NullableIntegerSchema(description string) Schema {
	return Schema{
		Type:        SchemaType{"integer", "null"},
		Description: description,
	}
}

func BoundedIntegerSchema(description string, minimum int, maximum int) Schema {
	return Schema{
		Type:        SchemaType{"integer"},
		Description: description,
		Minimum:     &minimum,
		Maximum:     &maximum,
	}
}

func NullableBooleanSchema(description string) Schema {
	return Schema{
		Type:        SchemaType{"boolean", "null"},
		Description: description,
	}
}

func BooleanSchema(description string) Schema {
	return Schema{
		Type:        SchemaType{"boolean"},
		Description: description,
	}
}
