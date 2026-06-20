package timeline

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"den-services/shared/api"
)

type ScopeKind string

const (
	ScopeKindChannel ScopeKind = "channel"
	ScopeKindProject ScopeKind = "project"
)

type SourceDomain string

const (
	SourceDomainConversation SourceDomain = "conversation"
	SourceDomainObservation  SourceDomain = "observation"
)

type RenderKind string

const (
	RenderKindMessage    RenderKind = "message"
	RenderKindBreadcrumb RenderKind = "breadcrumb"
	RenderKindProgress   RenderKind = "progress"
	RenderKindEvidence   RenderKind = "evidence"
	RenderKindSystem     RenderKind = "system"
	RenderKindDiagnostic RenderKind = "diagnostic"
)

type CursorSource string

const (
	CursorSourceMessage     CursorSource = "msg"
	CursorSourceObservation CursorSource = "obs"
)

type TimelineScope struct {
	Kind      ScopeKind
	ChannelID *int64
	ProjectID *string
}

func NewChannelScope(channelID int64) (TimelineScope, error) {
	if channelID <= 0 {
		return TimelineScope{}, ErrInvalidScope
	}
	return TimelineScope{
		Kind:      ScopeKindChannel,
		ChannelID: &channelID,
	}, nil
}

func NewProjectScope(projectID string) (TimelineScope, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return TimelineScope{}, ErrInvalidScope
	}
	return TimelineScope{
		Kind:      ScopeKindProject,
		ProjectID: &projectID,
	}, nil
}

func (s TimelineScope) Validate() error {
	switch s.Kind {
	case ScopeKindChannel:
		if s.ChannelID == nil || *s.ChannelID <= 0 {
			return ErrInvalidScope
		}
	case ScopeKindProject:
		if s.ProjectID == nil || strings.TrimSpace(*s.ProjectID) == "" {
			return ErrInvalidScope
		}
	default:
		return ErrInvalidScope
	}
	return nil
}

type TimelineCursor struct {
	Version    int          `json:"v"`
	OccurredAt time.Time    `json:"t"`
	Source     CursorSource `json:"s"`
	ID         int64        `json:"id"`
}

func NewTimelineCursor(occurredAt time.Time, source CursorSource, id int64) (TimelineCursor, error) {
	cursor := TimelineCursor{
		Version:    1,
		OccurredAt: occurredAt.UTC(),
		Source:     source,
		ID:         id,
	}
	if err := cursor.Validate(); err != nil {
		return TimelineCursor{}, err
	}
	return cursor, nil
}

func DecodeCursor(raw string) (*TimelineCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decoding cursor: %w", ErrInvalidCursor, err)
	}
	var cursor TimelineCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fmt.Errorf("%w: parsing cursor: %w", ErrInvalidCursor, err)
	}
	if err := cursor.Validate(); err != nil {
		return nil, err
	}
	return &cursor, nil
}

func (c TimelineCursor) Encode() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encoding cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (c TimelineCursor) Validate() error {
	if c.Version != 1 || c.OccurredAt.IsZero() || c.ID <= 0 {
		return ErrInvalidCursor
	}
	if c.Source != CursorSourceMessage && c.Source != CursorSourceObservation {
		return ErrInvalidCursor
	}
	return nil
}

func (c TimelineCursor) SourceRank() int {
	return sourceRank(c.Source)
}

type TimelineItem struct {
	TimelineID      string
	OccurredAt      time.Time
	SourceDomain    SourceDomain
	SourceID        string
	SourceCursor    CursorSource
	SourceNumericID int64
	EventKind       string
	RenderKind      RenderKind
	DisplayOnly     bool
	ChannelID       *int64
	ProjectID       *string
	TaskID          *int64
	Actor           TimelineActor
	Body            *string
	Summary         *string
	Severity        string
	Metadata        json.RawMessage
	SourceRef       TimelineSourceRef
}

func (i TimelineItem) Cursor() (TimelineCursor, error) {
	return NewTimelineCursor(i.OccurredAt, i.SourceCursor, i.SourceNumericID)
}

type TimelineActor struct {
	Type            string
	Identity        string
	ProfileIdentity *string
	AgentInstanceID *string
}

type TimelineSourceRef struct {
	Domain string
	Table  string
	ID     string
}

type ListItemsQuery struct {
	Scope        TimelineScope
	After        *TimelineCursor
	Limit        int
	IncludeDebug bool
}

func badRequest(err error) error {
	return fmt.Errorf("%w: %w", api.ErrBadRequest, err)
}

func codedBadRequest(code string, message string) error {
	return timelineStatusError{
		code:    code,
		message: message,
		status:  http.StatusBadRequest,
	}
}

type timelineStatusError struct {
	code    string
	message string
	status  int
}

func (e timelineStatusError) Error() string {
	return e.message
}

func (e timelineStatusError) Code() string {
	return e.code
}

func (e timelineStatusError) HTTPStatus() int {
	return e.status
}

func sourceRank(source CursorSource) int {
	switch source {
	case CursorSourceMessage:
		return 1
	case CursorSourceObservation:
		return 2
	}
	return 99
}

var (
	ErrInvalidScope  = errors.New("invalid timeline scope")  //nolint:gochecknoglobals
	ErrInvalidCursor = errors.New("invalid timeline cursor") //nolint:gochecknoglobals
	ErrInvalidLimit  = errors.New("invalid timeline limit")  //nolint:gochecknoglobals
)
