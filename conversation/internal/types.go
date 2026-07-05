package conversation

import (
	"encoding/json"
	"time"

	"den-services/shared/identity"
)

type Channel struct {
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

type ChannelMessage struct {
	ID                  int64
	ChannelID           int64
	SenderType          string
	SenderIdentity      string
	Body                string
	MessageKind         string
	SourceKind          string
	SourceID            *string
	SourceProjectID     *string
	TargetProjectID     *string
	TargetTaskID        *int64
	AssignmentID        *string
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
	DedupeKey           *string
	CreatedAt           time.Time
	EditedAt            *time.Time
	DeletedAt           *time.Time
}

type ChannelMembership struct {
	ID                int64
	ChannelID         int64
	MemberType        string
	MemberIdentity    string
	ProfileIdentity   *string
	MembershipStatus  string
	WakePolicy        string
	CanSend           bool
	CanReact          bool
	CanInvite         bool
	MembershipPurpose string
	Settings          json.RawMessage
	WakeTarget        *identity.AgentIdentity
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LeftAt            *time.Time
}

type ChannelReaction struct {
	ID              int64
	MessageID       int64
	ChannelID       int64
	ReactorType     string
	ReactorIdentity string
	Reaction        string
	CreatedAt       time.Time
	DeletedAt       *time.Time
}

type ChannelReadCursor struct {
	ChannelID         int64
	ReaderType        string
	ReaderIdentity    string
	InstanceID        *string
	LastReadMessageID *int64
	LastReadAt        time.Time
}

type ChannelProjectLink struct {
	ID        int64
	ChannelID int64
	ProjectID string
	LinkKind  string
	CreatedBy string
	CreatedAt time.Time
	DeletedAt *time.Time
}

func defaultJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
