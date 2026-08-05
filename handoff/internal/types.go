package handoff

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	MaxLabelBytes = 128
	MaxBodyBytes  = 64 * 1024
)

var (
	ErrHandoffNotFound = errors.New("handoff not found")          //nolint:gochecknoglobals
	ErrInvalidLabel    = errors.New("invalid handoff label")      //nolint:gochecknoglobals
	ErrMissingBody     = errors.New("body_markdown is required")  //nolint:gochecknoglobals
	ErrBodyTooLarge    = errors.New("body_markdown is too large") //nolint:gochecknoglobals
	labelPattern       = regexp.MustCompile(`^[A-Za-z0-9._/:\-]+$`)
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func NewServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func (e *ServiceError) Error() string   { return e.err.Error() }
func (e *ServiceError) Unwrap() error   { return e.err }
func (e *ServiceError) Code() string    { return e.code }
func (e *ServiceError) HTTPStatus() int { return e.status }

func validationFailed(err error) error {
	return NewServiceError(err, "validation_failed", http.StatusBadRequest)
}

func handoffNotFound(label string) error {
	return NewServiceError(fmt.Errorf("%w: %s", ErrHandoffNotFound, label), "handoff_not_found", http.StatusNotFound)
}

type Handoff struct {
	label        string
	bodyMarkdown string
	revision     int64
	createdAt    time.Time
	updatedAt    time.Time
	updatedBy    string
}

type NewHandoffParams struct {
	Label        string
	BodyMarkdown string
	Revision     int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	UpdatedBy    string
}

func NewHandoff(params NewHandoffParams) (*Handoff, error) {
	label, err := normalizeLabel(params.Label)
	if err != nil {
		return nil, err
	}
	if params.BodyMarkdown == "" {
		return nil, ErrMissingBody
	}
	if len(params.BodyMarkdown) > MaxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	createdAt := params.CreatedAt.UTC()
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	return &Handoff{
		label:        label,
		bodyMarkdown: params.BodyMarkdown,
		revision:     params.Revision,
		createdAt:    createdAt,
		updatedAt:    updatedAt,
		updatedBy:    strings.TrimSpace(params.UpdatedBy),
	}, nil
}

func normalizeLabel(raw string) (string, error) {
	label := strings.TrimSpace(raw)
	if label == "" || len(label) > MaxLabelBytes || !labelPattern.MatchString(label) {
		return "", fmt.Errorf("%w: use 1-%d ASCII letters, digits, dot, underscore, slash, colon, or hyphen", ErrInvalidLabel, MaxLabelBytes)
	}
	return label, nil
}

func (h *Handoff) Label() string        { return h.label }
func (h *Handoff) BodyMarkdown() string { return h.bodyMarkdown }
func (h *Handoff) Revision() int64      { return h.revision }
func (h *Handoff) CreatedAt() time.Time { return h.createdAt }
func (h *Handoff) UpdatedAt() time.Time { return h.updatedAt }
func (h *Handoff) UpdatedBy() string    { return h.updatedBy }
