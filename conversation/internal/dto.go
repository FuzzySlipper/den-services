package conversation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"den-services/shared/api"
	"den-services/shared/identity"
)

const idempotencyHeader = "Idempotency-Key"

type CreateChannelRequest struct {
	Slug        string          `json:"slug"`
	DisplayName string          `json:"display_name"`
	Kind        string          `json:"kind"`
	ProjectID   *string         `json:"project_id,omitempty"`
	SpaceID     *string         `json:"space_id,omitempty"`
	CreatedBy   string          `json:"created_by"`
	Visibility  string          `json:"visibility"`
	Settings    json.RawMessage `json:"settings,omitempty"`
}

func (r CreateChannelRequest) Validate() error {
	if strings.TrimSpace(r.Slug) == "" ||
		strings.TrimSpace(r.DisplayName) == "" ||
		strings.TrimSpace(r.Kind) == "" ||
		strings.TrimSpace(r.CreatedBy) == "" ||
		strings.TrimSpace(r.Visibility) == "" {
		return ErrInvalidChannel
	}
	if !validOptionalJSON(r.Settings) {
		return ErrInvalidChannel
	}
	return nil
}

type ListChannelsQuery struct {
	ProjectID *string
	Kind      *string
	Limit     int
}

type PutDefaultChannelRequest struct {
	ChannelID   *int64          `json:"channel_id,omitempty"`
	Slug        string          `json:"slug,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	CreatedBy   string          `json:"created_by"`
	Settings    json.RawMessage `json:"settings,omitempty"`
}

func (r PutDefaultChannelRequest) Validate(projectID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(r.CreatedBy) == "" {
		return ErrInvalidChannel
	}
	if r.ChannelID == nil && (strings.TrimSpace(r.Slug) == "" || strings.TrimSpace(r.DisplayName) == "") {
		return ErrInvalidChannel
	}
	if r.ChannelID != nil && *r.ChannelID <= 0 {
		return ErrInvalidChannel
	}
	if !validOptionalJSON(r.Settings) {
		return ErrInvalidChannel
	}
	return nil
}

type AppendMessageRequest struct {
	SenderType          string          `json:"sender_type"`
	SenderIdentity      string          `json:"sender_identity"`
	Body                string          `json:"body"`
	MessageKind         string          `json:"message_kind"`
	SourceKind          string          `json:"source_kind"`
	SourceID            *string         `json:"source_id,omitempty"`
	SourceProjectID     *string         `json:"source_project_id,omitempty"`
	TargetProjectID     *string         `json:"target_project_id,omitempty"`
	TargetTaskID        *int64          `json:"target_task_id,omitempty"`
	AssignmentID        *string         `json:"assignment_id,omitempty"`
	WorkerRunID         *string         `json:"worker_run_id,omitempty"`
	WorkerRole          *string         `json:"worker_role,omitempty"`
	ProfileIdentity     *string         `json:"profile_identity,omitempty"`
	AgentInstanceID     *string         `json:"agent_instance_id,omitempty"`
	PoolMemberID        *string         `json:"pool_member_id,omitempty"`
	SessionOwnerID      *string         `json:"session_owner_id,omitempty"`
	SessionID           *string         `json:"session_id,omitempty"`
	Summary             *string         `json:"summary,omitempty"`
	DeepLink            *string         `json:"deep_link,omitempty"`
	ThreadRootMessageID *int64          `json:"thread_root_message_id,omitempty"`
	ReplyToMessageID    *int64          `json:"reply_to_message_id,omitempty"`
	Metadata            json.RawMessage `json:"metadata,omitempty"`
	DedupeKey           *string         `json:"dedupe_key,omitempty"`
}

func (r AppendMessageRequest) Validate(channelID int64, dedupeKey string) error {
	if channelID <= 0 ||
		strings.TrimSpace(r.SenderType) == "" ||
		strings.TrimSpace(r.SenderIdentity) == "" ||
		strings.TrimSpace(r.Body) == "" ||
		strings.TrimSpace(r.MessageKind) == "" ||
		strings.TrimSpace(r.SourceKind) == "" {
		return ErrInvalidMessage
	}
	if strings.TrimSpace(dedupeKey) == "" {
		return ErrMissingDedupeKey
	}
	if !validOptionalJSON(r.Metadata) {
		return ErrInvalidMessage
	}
	return nil
}

type ListMessagesQuery struct {
	ChannelID    int64
	AfterID      *int64
	AssignmentID *string
	Limit        int
}

type PutMembershipRequest struct {
	MemberType        string          `json:"member_type"`
	MemberIdentity    string          `json:"member_identity"`
	ProfileIdentity   *string         `json:"profile_identity,omitempty"`
	MembershipStatus  string          `json:"membership_status"`
	WakePolicy        string          `json:"wake_policy"`
	CanSend           *bool           `json:"can_send,omitempty"`
	CanReact          *bool           `json:"can_react,omitempty"`
	CanInvite         *bool           `json:"can_invite,omitempty"`
	MembershipPurpose string          `json:"membership_purpose"`
	Settings          json.RawMessage `json:"settings,omitempty"`
}

func (r PutMembershipRequest) Validate(channelID int64) error {
	if channelID <= 0 ||
		strings.TrimSpace(r.MemberType) == "" ||
		strings.TrimSpace(r.MemberIdentity) == "" ||
		strings.TrimSpace(r.MembershipStatus) == "" ||
		strings.TrimSpace(r.WakePolicy) == "" ||
		strings.TrimSpace(r.MembershipPurpose) == "" {
		return ErrInvalidMembership
	}
	if !validOptionalJSON(r.Settings) {
		return ErrInvalidMembership
	}
	return nil
}

type ListMembershipsQuery struct {
	MemberIdentity    *string
	MembershipPurpose *string
	ProjectID         *string
	ChannelID         *int64
	IncludeLeft       bool
	Limit             int
}

type AddReactionRequest struct {
	ReactorType     string `json:"reactor_type"`
	ReactorIdentity string `json:"reactor_identity"`
	Reaction        string `json:"reaction"`
}

func (r AddReactionRequest) Validate(messageID int64) error {
	if messageID <= 0 ||
		strings.TrimSpace(r.ReactorType) == "" ||
		strings.TrimSpace(r.ReactorIdentity) == "" ||
		strings.TrimSpace(r.Reaction) == "" {
		return ErrInvalidReaction
	}
	return nil
}

type PutReadCursorRequest struct {
	ReaderType        string  `json:"reader_type"`
	ReaderIdentity    string  `json:"reader_identity"`
	InstanceID        *string `json:"instance_id,omitempty"`
	LastReadMessageID *int64  `json:"last_read_message_id,omitempty"`
}

func (r PutReadCursorRequest) Validate(channelID int64) error {
	if channelID <= 0 ||
		strings.TrimSpace(r.ReaderType) == "" ||
		strings.TrimSpace(r.ReaderIdentity) == "" {
		return ErrInvalidReadCursor
	}
	if r.ReaderType != "human" {
		return ErrInvalidReadCursor
	}
	if r.LastReadMessageID != nil && *r.LastReadMessageID <= 0 {
		return ErrInvalidReadCursor
	}
	return nil
}

type ChannelResponse struct {
	ID          int64           `json:"id"`
	Slug        string          `json:"slug"`
	DisplayName string          `json:"display_name"`
	Kind        string          `json:"kind"`
	ProjectID   *string         `json:"project_id"`
	SpaceID     *string         `json:"space_id"`
	CreatedBy   string          `json:"created_by"`
	Visibility  string          `json:"visibility"`
	Settings    json.RawMessage `json:"settings"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	ArchivedAt  *time.Time      `json:"archived_at"`
}

type MessageResponse struct {
	ID                  int64           `json:"id"`
	ChannelID           int64           `json:"channel_id"`
	SenderType          string          `json:"sender_type"`
	SenderIdentity      string          `json:"sender_identity"`
	Body                string          `json:"body"`
	MessageKind         string          `json:"message_kind"`
	SourceKind          string          `json:"source_kind"`
	SourceID            *string         `json:"source_id"`
	SourceProjectID     *string         `json:"source_project_id"`
	TargetProjectID     *string         `json:"target_project_id"`
	TargetTaskID        *int64          `json:"target_task_id"`
	AssignmentID        *string         `json:"assignment_id"`
	WorkerRunID         *string         `json:"worker_run_id"`
	WorkerRole          *string         `json:"worker_role"`
	ProfileIdentity     *string         `json:"profile_identity"`
	AgentInstanceID     *string         `json:"agent_instance_id"`
	PoolMemberID        *string         `json:"pool_member_id"`
	SessionOwnerID      *string         `json:"session_owner_id"`
	SessionID           *string         `json:"session_id"`
	Summary             *string         `json:"summary"`
	DeepLink            *string         `json:"deep_link"`
	ThreadRootMessageID *int64          `json:"thread_root_message_id"`
	ReplyToMessageID    *int64          `json:"reply_to_message_id"`
	Metadata            json.RawMessage `json:"metadata"`
	DedupeKey           *string         `json:"dedupe_key"`
	CreatedAt           time.Time       `json:"created_at"`
	EditedAt            *time.Time      `json:"edited_at"`
	DeletedAt           *time.Time      `json:"deleted_at"`
}

type MembershipResponse struct {
	ID                int64                   `json:"id"`
	ChannelID         int64                   `json:"channel_id"`
	MemberType        string                  `json:"member_type"`
	MemberIdentity    string                  `json:"member_identity"`
	ProfileIdentity   *string                 `json:"profile_identity"`
	MembershipStatus  string                  `json:"membership_status"`
	WakePolicy        string                  `json:"wake_policy"`
	CanSend           bool                    `json:"can_send"`
	CanReact          bool                    `json:"can_react"`
	CanInvite         bool                    `json:"can_invite"`
	MembershipPurpose string                  `json:"membership_purpose"`
	Settings          json.RawMessage         `json:"settings"`
	WakeTarget        *identity.AgentIdentity `json:"wake_target,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
	LeftAt            *time.Time              `json:"left_at"`
}

type ReactionResponse struct {
	ID              int64      `json:"id"`
	MessageID       int64      `json:"message_id"`
	ChannelID       int64      `json:"channel_id"`
	ReactorType     string     `json:"reactor_type"`
	ReactorIdentity string     `json:"reactor_identity"`
	Reaction        string     `json:"reaction"`
	CreatedAt       time.Time  `json:"created_at"`
	DeletedAt       *time.Time `json:"deleted_at"`
}

type ReadCursorResponse struct {
	ChannelID         int64     `json:"channel_id"`
	ReaderType        string    `json:"reader_type"`
	ReaderIdentity    string    `json:"reader_identity"`
	InstanceID        *string   `json:"instance_id"`
	LastReadMessageID *int64    `json:"last_read_message_id"`
	LastReadAt        time.Time `json:"last_read_at"`
}

func toChannelResponse(channel *Channel) ChannelResponse {
	return ChannelResponse{
		ID:          channel.ID,
		Slug:        channel.Slug,
		DisplayName: channel.DisplayName,
		Kind:        channel.Kind,
		ProjectID:   channel.ProjectID,
		SpaceID:     channel.SpaceID,
		CreatedBy:   channel.CreatedBy,
		Visibility:  channel.Visibility,
		Settings:    defaultJSON(channel.Settings),
		CreatedAt:   channel.CreatedAt,
		UpdatedAt:   channel.UpdatedAt,
		ArchivedAt:  channel.ArchivedAt,
	}
}

func toMessageResponse(message *ChannelMessage) MessageResponse {
	return MessageResponse{
		ID:                  message.ID,
		ChannelID:           message.ChannelID,
		SenderType:          message.SenderType,
		SenderIdentity:      message.SenderIdentity,
		Body:                message.Body,
		MessageKind:         message.MessageKind,
		SourceKind:          message.SourceKind,
		SourceID:            message.SourceID,
		SourceProjectID:     message.SourceProjectID,
		TargetProjectID:     message.TargetProjectID,
		TargetTaskID:        message.TargetTaskID,
		AssignmentID:        message.AssignmentID,
		WorkerRunID:         message.WorkerRunID,
		WorkerRole:          message.WorkerRole,
		ProfileIdentity:     message.ProfileIdentity,
		AgentInstanceID:     message.AgentInstanceID,
		PoolMemberID:        message.PoolMemberID,
		SessionOwnerID:      message.SessionOwnerID,
		SessionID:           message.SessionID,
		Summary:             message.Summary,
		DeepLink:            message.DeepLink,
		ThreadRootMessageID: message.ThreadRootMessageID,
		ReplyToMessageID:    message.ReplyToMessageID,
		Metadata:            defaultJSON(message.Metadata),
		DedupeKey:           message.DedupeKey,
		CreatedAt:           message.CreatedAt,
		EditedAt:            message.EditedAt,
		DeletedAt:           message.DeletedAt,
	}
}

func toMembershipResponse(membership *ChannelMembership) MembershipResponse {
	return MembershipResponse{
		ID:                membership.ID,
		ChannelID:         membership.ChannelID,
		MemberType:        membership.MemberType,
		MemberIdentity:    membership.MemberIdentity,
		ProfileIdentity:   membership.ProfileIdentity,
		MembershipStatus:  membership.MembershipStatus,
		WakePolicy:        membership.WakePolicy,
		CanSend:           membership.CanSend,
		CanReact:          membership.CanReact,
		CanInvite:         membership.CanInvite,
		MembershipPurpose: membership.MembershipPurpose,
		Settings:          defaultJSON(membership.Settings),
		WakeTarget:        membership.WakeTarget,
		CreatedAt:         membership.CreatedAt,
		UpdatedAt:         membership.UpdatedAt,
		LeftAt:            membership.LeftAt,
	}
}

func toReactionResponse(reaction *ChannelReaction) ReactionResponse {
	return ReactionResponse{
		ID:              reaction.ID,
		MessageID:       reaction.MessageID,
		ChannelID:       reaction.ChannelID,
		ReactorType:     reaction.ReactorType,
		ReactorIdentity: reaction.ReactorIdentity,
		Reaction:        reaction.Reaction,
		CreatedAt:       reaction.CreatedAt,
		DeletedAt:       reaction.DeletedAt,
	}
}

func toReadCursorResponse(cursor *ChannelReadCursor) ReadCursorResponse {
	return ReadCursorResponse{
		ChannelID:         cursor.ChannelID,
		ReaderType:        cursor.ReaderType,
		ReaderIdentity:    cursor.ReaderIdentity,
		InstanceID:        cursor.InstanceID,
		LastReadMessageID: cursor.LastReadMessageID,
		LastReadAt:        cursor.LastReadAt,
	}
}

func channelResponses(channels []*Channel) []ChannelResponse {
	responses := make([]ChannelResponse, 0, len(channels))
	for _, channel := range channels {
		responses = append(responses, toChannelResponse(channel))
	}
	return responses
}

func messageResponses(messages []*ChannelMessage) []MessageResponse {
	responses := make([]MessageResponse, 0, len(messages))
	for _, message := range messages {
		responses = append(responses, toMessageResponse(message))
	}
	return responses
}

func membershipResponses(memberships []*ChannelMembership) []MembershipResponse {
	responses := make([]MembershipResponse, 0, len(memberships))
	for _, membership := range memberships {
		responses = append(responses, toMembershipResponse(membership))
	}
	return responses
}

func cursorResponses(cursors []*ChannelReadCursor) []ReadCursorResponse {
	responses := make([]ReadCursorResponse, 0, len(cursors))
	for _, cursor := range cursors {
		responses = append(responses, toReadCursorResponse(cursor))
	}
	return responses
}

func parseRequiredInt64(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("value is required")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("value must be a positive integer")
	}
	return parsed, nil
}

func parseOptionalInt64(value string) (*int64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseRequiredInt64(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseLimit(raw string, cfg *Config) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return cfg.DefaultLimit, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 || parsed > cfg.MaxLimit {
		return 0, ErrInvalidLimit
	}
	return parsed, nil
}

func stringPtrFromQuery(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func dedupeKeyFromRequest(r *http.Request, bodyKey *string) string {
	if headerKey := strings.TrimSpace(r.Header.Get(idempotencyHeader)); headerKey != "" {
		return headerKey
	}
	if bodyKey == nil {
		return ""
	}
	return strings.TrimSpace(*bodyKey)
}

func validOptionalJSON(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	return json.Valid(raw)
}

func badRequest(err error) error {
	return errors.Join(api.ErrBadRequest, err)
}

func notFound(err error) error {
	return errors.Join(api.ErrNotFound, err)
}
