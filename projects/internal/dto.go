package projects

import (
	"encoding/json"
	"strings"
	"time"
)

type CreateProjectRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RootPath    string `json:"root_path,omitempty"`
	Description string `json:"description,omitempty"`
}

type CreateSpaceRequest struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind,omitempty"`
	Visibility   string          `json:"visibility,omitempty"`
	Owner        string          `json:"owner,omitempty"`
	RootPath     string          `json:"root_path,omitempty"`
	Description  string          `json:"description,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json,omitempty"`
}

type UpdateProjectRequest struct {
	Name         *string         `json:"name,omitempty"`
	RootPath     *string         `json:"root_path,omitempty"`
	Description  *string         `json:"description,omitempty"`
	Owner        *string         `json:"owner,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json,omitempty"`
}

func (r UpdateProjectRequest) HasChanges() bool {
	return r.Name != nil ||
		r.RootPath != nil ||
		r.Description != nil ||
		r.Owner != nil ||
		r.SettingsJSON != nil
}

type UpdateVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

type AssertWritableRequest struct {
	AllowArchivedScope bool `json:"allow_archived_scope,omitempty"`
}

type ScopeResponse struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Visibility   string          `json:"visibility"`
	Owner        string          `json:"owner,omitempty"`
	RootPath     string          `json:"root_path,omitempty"`
	Description  string          `json:"description,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Writable     bool            `json:"writable"`
}

type AssertWritableResponse struct {
	ID         string `json:"id"`
	Writable   bool   `json:"writable"`
	Visibility string `json:"visibility"`
}

func toScopeResponse(scope *Scope) ScopeResponse {
	response := ScopeResponse{
		ID:          scope.ID(),
		Name:        scope.Name(),
		Kind:        scope.Kind(),
		Visibility:  scope.Visibility(),
		Owner:       scope.Owner(),
		RootPath:    scope.RootPath(),
		Description: scope.Description(),
		CreatedAt:   scope.CreatedAt(),
		UpdatedAt:   scope.UpdatedAt(),
		Writable:    scope.Writable(),
	}
	if settings := scope.SettingsJSON(); len(settings) > 0 {
		response.SettingsJSON = json.RawMessage(settings)
	}
	return response
}

func toScopeResponses(scopes []*Scope) []ScopeResponse {
	response := make([]ScopeResponse, 0, len(scopes))
	for _, scope := range scopes {
		response = append(response, toScopeResponse(scope))
	}
	return response
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}
