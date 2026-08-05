package handoff

import "time"

type SetHandoffRequest struct {
	Label        string `json:"label"`
	BodyMarkdown string `json:"body_markdown"`
	UpdatedBy    string `json:"updated_by,omitempty"`
}

type HandoffResponse struct {
	Label        string    `json:"label"`
	BodyMarkdown string    `json:"body_markdown"`
	Revision     int64     `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedBy    string    `json:"updated_by,omitempty"`
}

type SetHandoffResponse struct {
	Label     string    `json:"label"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

func toHandoffResponse(value *Handoff) HandoffResponse {
	return HandoffResponse{
		Label: value.Label(), BodyMarkdown: value.BodyMarkdown(), Revision: value.Revision(),
		CreatedAt: value.CreatedAt(), UpdatedAt: value.UpdatedAt(), UpdatedBy: value.UpdatedBy(),
	}
}

func toSetHandoffResponse(value *Handoff) SetHandoffResponse {
	return SetHandoffResponse{
		Label: value.Label(), Revision: value.Revision(), CreatedAt: value.CreatedAt(),
		UpdatedAt: value.UpdatedAt(), UpdatedBy: value.UpdatedBy(),
	}
}
