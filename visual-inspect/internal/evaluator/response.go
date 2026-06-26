package evaluator

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"den-services/visual-inspect/internal/schema"
)

type schemaMetadata struct {
	provider string
	model    string
	version  string
}

func decodeModelResponse(raw string) (schema.EvaluateResponse, []string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "```") {
		return schema.EvaluateResponse{}, []string{"model_output_not_json"}, false
	}
	var response schema.EvaluateResponse
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return schema.EvaluateResponse{}, []string{"model_output_json_decode_failed"}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return schema.EvaluateResponse{}, []string{"model_output_extra_json"}, false
	}
	return response, nil, true
}

func loadSchemaMetadata(path string) (schemaMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schemaMetadata{}, fmt.Errorf("reading response schema file %s: %w", path, err)
	}
	var decoded struct {
		ID      string `json:"$id"`
		Version string `json:"version"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return schemaMetadata{}, fmt.Errorf("parsing response schema file %s: %w", path, err)
	}
	version := decoded.Version
	if version == "" {
		version = decoded.ID
	}
	if version == "" {
		version = decoded.Title
	}
	if version == "" {
		version = filepath.Base(path)
	}
	return schemaMetadata{version: version}, nil
}
