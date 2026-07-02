package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"den-services/mcp/internal/config"
)

type projectsToolArguments struct {
	ID              string          `json:"id"`
	ProjectID       string          `json:"project_id"`
	SpaceID         string          `json:"space_id"`
	Name            *string         `json:"name"`
	Kind            string          `json:"kind"`
	Visibility      string          `json:"visibility"`
	Owner           *string         `json:"owner"`
	RootPath        *string         `json:"root_path"`
	Description     *string         `json:"description"`
	SettingsJSON    json.RawMessage `json:"settings_json"`
	IncludeHidden   bool            `json:"include_hidden"`
	IncludeArchived bool            `json:"include_archived"`
	Force           bool            `json:"force"`
}

type createProjectBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RootPath    string `json:"root_path,omitempty"`
	Description string `json:"description,omitempty"`
}

type updateProjectBody struct {
	Name         *string         `json:"name,omitempty"`
	RootPath     *string         `json:"root_path,omitempty"`
	Description  *string         `json:"description,omitempty"`
	Owner        *string         `json:"owner,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json,omitempty"`
}

type createSpaceBody struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind,omitempty"`
	Visibility   string          `json:"visibility,omitempty"`
	Owner        string          `json:"owner,omitempty"`
	RootPath     string          `json:"root_path,omitempty"`
	Description  string          `json:"description,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json,omitempty"`
}

type updateVisibilityBody struct {
	Visibility string `json:"visibility"`
}

type deleteSpaceBody struct {
	Force bool `json:"force,omitempty"`
}

type mcpToolResult struct {
	Content           []mcpToolContent `json:"content"`
	IsError           bool             `json:"isError"`
	StructuredContent json.RawMessage  `json:"structuredContent,omitempty"`
}

type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (c *Client) callProjectsREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildProjectsRESTRequest(ctx, backend, route, call)
	if err != nil {
		return Result{}, nil, err
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return Result{}, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{}, nil, fmt.Errorf("reading projects backend response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, responseBody), nil
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func buildProjectsRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeProjectsToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := projectsRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := projectsRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building projects backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func decodeProjectsToolArguments(raw json.RawMessage) (projectsToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments projectsToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return projectsToolArguments{}, fmt.Errorf("decoding projects tool arguments: %w", err)
	}
	return arguments, nil
}

func projectsRESTRequestBody(operation string, arguments projectsToolArguments) ([]byte, error) {
	switch operation {
	case "create_project":
		return json.Marshal(createProjectBody{
			ID:          arguments.ID,
			Name:        stringValue(arguments.Name),
			RootPath:    stringValue(arguments.RootPath),
			Description: stringValue(arguments.Description),
		})
	case "update_project":
		return json.Marshal(updateProjectBody{
			Name:         arguments.Name,
			RootPath:     arguments.RootPath,
			Description:  arguments.Description,
			Owner:        arguments.Owner,
			SettingsJSON: arguments.SettingsJSON,
		})
	case "create_space":
		return json.Marshal(createSpaceBody{
			ID:           arguments.ID,
			Name:         stringValue(arguments.Name),
			Kind:         arguments.Kind,
			Visibility:   arguments.Visibility,
			Owner:        stringValue(arguments.Owner),
			RootPath:     stringValue(arguments.RootPath),
			Description:  stringValue(arguments.Description),
			SettingsJSON: arguments.SettingsJSON,
		})
	case "update_space_visibility":
		return json.Marshal(updateVisibilityBody{Visibility: arguments.Visibility})
	case "delete_space":
		return json.Marshal(deleteSpaceBody{Force: arguments.Force})
	case "archive_space", "list_projects", "list_spaces":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: projects operation %s", ErrUnsupportedAdapter, operation)
	}
}

func projectsRESTURL(baseURL string, route Route, arguments projectsToolArguments) (string, error) {
	routePath, err := expandProjectsPath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing projects backend URL: %w", err)
	}
	if route.Operation == "list_spaces" {
		query := parsedURL.Query()
		if strings.TrimSpace(arguments.Kind) != "" {
			query.Set("kind", strings.TrimSpace(arguments.Kind))
		}
		if arguments.IncludeHidden {
			query.Set("include_hidden", "true")
		}
		if arguments.IncludeArchived {
			query.Set("include_archived", "true")
		}
		parsedURL.RawQuery = query.Encode()
	}
	return parsedURL.String(), nil
}

func expandProjectsPath(path string, arguments projectsToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", errorsForMissingPathValue("project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(result, "{space_id}") {
		if strings.TrimSpace(arguments.SpaceID) == "" {
			return "", errorsForMissingPathValue("space_id")
		}
		result = strings.ReplaceAll(result, "{space_id}", url.PathEscape(strings.TrimSpace(arguments.SpaceID)))
	}
	return result, nil
}

func errorsForMissingPathValue(name string) error {
	return fmt.Errorf("projects route requires %s", name)
}

func buildRESTToolResult(responseBody []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(responseBody)
	if len(trimmed) == 0 {
		trimmed = json.RawMessage(`null`)
	}
	var parsed json.RawMessage
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, fmt.Errorf("parsing projects backend JSON response: %w", err)
	}
	result := mcpToolResult{
		Content: []mcpToolContent{{
			Type: "text",
			Text: string(trimmed),
		}},
		IsError:           false,
		StructuredContent: parsed,
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encoding projects MCP tool result: %w", err)
	}
	return data, nil
}

func (c *Client) doRESTRequest(request *http.Request, backend config.BackendConfig) (*http.Response, context.CancelFunc, error) {
	requestCtx, cancel := context.WithTimeout(request.Context(), backend.Timeout)
	request = request.WithContext(requestCtx)
	response, err := c.httpClient.Do(request)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return response, cancel, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
