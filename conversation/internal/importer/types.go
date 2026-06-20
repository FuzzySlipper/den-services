package importer

import (
	"context"
	"encoding/json"
	"time"
)

const SourceLegacyDenChannels = "legacy_den_channels_sqlite"

type Options struct {
	SourcePath  string
	DatabaseURL string
	SourceName  string
	DryRun      bool
	Limit       int
}

type Report struct {
	SourcePath  string            `json:"source_path"`
	SourceName  string            `json:"source_name"`
	DryRun      bool              `json:"dry_run"`
	Counts      ImportCounts      `json:"counts"`
	Exclusions  ExclusionCounts   `json:"exclusions"`
	Destination DestinationCounts `json:"destination,omitempty"`
}

type ImportCounts struct {
	Channels     int `json:"channels"`
	Messages     int `json:"messages"`
	Memberships  int `json:"memberships"`
	Reactions    int `json:"reactions"`
	ReadCursors  int `json:"read_cursors"`
	ProjectLinks int `json:"project_links"`
}

type ExclusionCounts struct {
	NonHumanReadCursors  int `json:"non_human_read_cursors"`
	UnmappedReactions    int `json:"unmapped_reactions"`
	UnmappedReadCursors  int `json:"unmapped_read_cursors"`
	UnmappedProjectLinks int `json:"unmapped_project_links"`
}

type DestinationCounts struct {
	Channels     int `json:"channels"`
	Messages     int `json:"messages"`
	Memberships  int `json:"memberships"`
	Reactions    int `json:"reactions"`
	ReadCursors  int `json:"read_cursors"`
	ProjectLinks int `json:"project_links"`
	ChatHistory  int `json:"chat_history"`
}

type SourceData struct {
	Channels     []LegacyChannel
	Messages     []LegacyMessage
	Memberships  []LegacyMembership
	Reactions    []LegacyReaction
	ReadCursors  []LegacyReadCursor
	ProjectLinks []LegacyProjectLink
}

type LegacyChannel struct {
	ID          int64
	Slug        string
	DisplayName string
	Kind        string
	ProjectID   *string
	SpaceID     *string
	CreatedBy   string
	Visibility  string
	Settings    json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

type LegacyMessage struct {
	ID                  int64
	ChannelID           int64
	SenderType          string
	SenderIdentity      string
	Body                string
	MessageKind         string
	LegacySourceKind    *string
	LegacySourceID      *string
	SourceProjectID     *string
	TargetProjectID     *string
	TargetTaskID        *int64
	AssignmentID        *string
	CheckpointType      *string
	CheckpointHandle    *string
	WorkerRunID         *string
	WorkerRole          *string
	ProfileIdentity     *string
	AgentInstanceID     *string
	PoolMemberID        *string
	SessionOwnerID      *string
	SessionID           *string
	Summary             *string
	DeepLink            *string
	ThreadRootMessageID *int64
	ReplyToMessageID    *int64
	Metadata            json.RawMessage
	DeliveryRequestID   *string
	LegacyDedupeKey     *string
	CreatedAt           time.Time
	EditedAt            *time.Time
	DeletedAt           *time.Time
}

type LegacyMembership struct {
	ID                int64
	ChannelID         int64
	MemberType        string
	MemberIdentity    string
	MembershipStatus  string
	WakePolicy        string
	CanSend           bool
	CanReact          bool
	CanInvite         bool
	MembershipPurpose string
	Settings          json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type LegacyReaction struct {
	ID              int64
	MessageID       int64
	ReactorType     string
	ReactorIdentity string
	Reaction        string
	CreatedAt       time.Time
}

type LegacyReadCursor struct {
	ID                int64
	ChannelID         int64
	ReaderType        string
	ReaderIdentity    string
	InstanceID        *string
	LastReadMessageID *int64
	LastReadAt        time.Time
}

type LegacyProjectLink struct {
	ID           int64
	ChannelID    int64
	ProjectID    string
	RelationKind string
	IsPrimary    bool
	Settings     json.RawMessage
	CreatedAt    time.Time
}

type Destination interface {
	UpsertChannel(ctx context.Context, source string, channel LegacyChannel) (int64, error)
	UpsertMessage(ctx context.Context, source string, message LegacyMessage, channelID int64) (int64, error)
	UpdateMessageReferences(ctx context.Context, source string, message LegacyMessage) error
	UpsertMembership(ctx context.Context, source string, membership LegacyMembership, channelID int64) (int64, error)
	UpsertReaction(ctx context.Context, source string, reaction LegacyReaction, messageID int64, channelID int64) (int64, error)
	UpsertReadCursor(ctx context.Context, source string, cursor LegacyReadCursor, channelID int64, messageID *int64) error
	UpsertProjectLink(ctx context.Context, source string, link LegacyProjectLink, channelID int64) (int64, error)
	Counts(ctx context.Context) (DestinationCounts, error)
}
