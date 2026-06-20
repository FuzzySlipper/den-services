package conversation

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

type memoryConversationStore struct {
	t *testing.T

	mu          sync.Mutex
	nextChannel int64
	nextMessage int64
	nextMember  int64
	nextReact   int64

	channels    map[int64]*Channel
	messages    map[int64]*ChannelMessage
	memberships map[int64]*ChannelMembership
	reactions   map[int64]*ChannelReaction
	cursors     map[string]*ChannelReadCursor
}

func newMemoryConversationStore(t *testing.T) *memoryConversationStore {
	t.Helper()
	return &memoryConversationStore{
		t:           t,
		nextChannel: 1,
		nextMessage: 1,
		nextMember:  1,
		nextReact:   1,
		channels:    make(map[int64]*Channel),
		messages:    make(map[int64]*ChannelMessage),
		memberships: make(map[int64]*ChannelMembership),
		reactions:   make(map[int64]*ChannelReaction),
		cursors:     make(map[string]*ChannelReadCursor),
	}
}

func (s *memoryConversationStore) Ping(context.Context) error {
	return nil
}

func (s *memoryConversationStore) CreateChannel(_ context.Context, channel *Channel) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	created := cloneChannel(channel)
	created.ID = s.nextChannel
	s.nextChannel++
	s.channels[created.ID] = created
	return cloneChannel(created), nil
}

func (s *memoryConversationStore) ListChannels(_ context.Context, query ListChannelsQuery) ([]*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	channels := make([]*Channel, 0, len(s.channels))
	for _, channel := range s.channels {
		if query.ProjectID != nil && channel.ProjectID != nil && *channel.ProjectID != *query.ProjectID {
			continue
		}
		if query.ProjectID != nil && channel.ProjectID == nil {
			continue
		}
		if query.Kind != nil && channel.Kind != *query.Kind {
			continue
		}
		channels = append(channels, cloneChannel(channel))
	}
	slices.SortFunc(channels, func(a *Channel, b *Channel) int {
		return int(a.ID - b.ID)
	})
	return limitSlice(channels, query.Limit), nil
}

func (s *memoryConversationStore) GetChannel(_ context.Context, id int64) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	channel, ok := s.channels[id]
	if !ok {
		return nil, notFound(ErrChannelNotFound)
	}
	return cloneChannel(channel), nil
}

func (s *memoryConversationStore) UpsertProjectDefaultChannel(_ context.Context, projectID string, req PutDefaultChannelRequest, at time.Time) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req.ChannelID != nil {
		channel, ok := s.channels[*req.ChannelID]
		if !ok {
			return nil, notFound(ErrChannelNotFound)
		}
		channel.ProjectID = &projectID
		channel.Kind = "project_default"
		channel.UpdatedAt = at
		return cloneChannel(channel), nil
	}
	for _, channel := range s.channels {
		if channel.ProjectID != nil && *channel.ProjectID == projectID && channel.Kind == "project_default" {
			channel.Slug = req.Slug
			channel.DisplayName = req.DisplayName
			channel.Settings = defaultJSON(req.Settings)
			channel.UpdatedAt = at
			return cloneChannel(channel), nil
		}
	}
	channel := &Channel{
		ID:          s.nextChannel,
		Slug:        req.Slug,
		DisplayName: req.DisplayName,
		Kind:        "project_default",
		ProjectID:   &projectID,
		CreatedBy:   req.CreatedBy,
		Visibility:  "normal",
		Settings:    defaultJSON(req.Settings),
		CreatedAt:   at,
		UpdatedAt:   at,
	}
	s.nextChannel++
	s.channels[channel.ID] = channel
	return cloneChannel(channel), nil
}

func (s *memoryConversationStore) AppendMessage(_ context.Context, message *ChannelMessage) (*ChannelMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.messages {
		if existing.DedupeKey != nil && message.DedupeKey != nil && *existing.DedupeKey == *message.DedupeKey {
			return cloneMessage(existing), nil
		}
	}
	created := cloneMessage(message)
	created.ID = s.nextMessage
	s.nextMessage++
	s.messages[created.ID] = created
	return cloneMessage(created), nil
}

func (s *memoryConversationStore) ListMessages(_ context.Context, query ListMessagesQuery) ([]*ChannelMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := make([]*ChannelMessage, 0, len(s.messages))
	for _, message := range s.messages {
		if message.ChannelID != query.ChannelID {
			continue
		}
		if query.AfterID != nil && message.ID <= *query.AfterID {
			continue
		}
		if query.AssignmentID != nil && (message.AssignmentID == nil || *message.AssignmentID != *query.AssignmentID) {
			continue
		}
		messages = append(messages, cloneMessage(message))
	}
	slices.SortFunc(messages, func(a *ChannelMessage, b *ChannelMessage) int {
		return int(a.ID - b.ID)
	})
	return limitSlice(messages, query.Limit), nil
}

func (s *memoryConversationStore) UpsertMembership(_ context.Context, membership *ChannelMembership) (*ChannelMembership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.memberships {
		if existing.ChannelID == membership.ChannelID &&
			existing.MemberIdentity == membership.MemberIdentity &&
			existing.MembershipPurpose == membership.MembershipPurpose {
			updated := cloneMembership(membership)
			updated.ID = existing.ID
			updated.CreatedAt = existing.CreatedAt
			if updated.MembershipStatus == "left" {
				leftAt := updated.UpdatedAt
				updated.LeftAt = &leftAt
			}
			s.memberships[updated.ID] = updated
			return cloneMembership(updated), nil
		}
	}
	created := cloneMembership(membership)
	created.ID = s.nextMember
	s.nextMember++
	if created.MembershipStatus == "left" {
		leftAt := created.UpdatedAt
		created.LeftAt = &leftAt
	}
	s.memberships[created.ID] = created
	return cloneMembership(created), nil
}

func (s *memoryConversationStore) ListMemberships(_ context.Context, query ListMembershipsQuery) ([]*ChannelMembership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	memberships := make([]*ChannelMembership, 0, len(s.memberships))
	for _, membership := range s.memberships {
		if query.MemberIdentity != nil && membership.MemberIdentity != *query.MemberIdentity {
			continue
		}
		if query.MembershipPurpose != nil && membership.MembershipPurpose != *query.MembershipPurpose {
			continue
		}
		if query.ChannelID != nil && membership.ChannelID != *query.ChannelID {
			continue
		}
		if !query.IncludeLeft && membership.MembershipStatus == "left" {
			continue
		}
		if query.ProjectID != nil {
			channel := s.channels[membership.ChannelID]
			if channel == nil || channel.ProjectID == nil || *channel.ProjectID != *query.ProjectID {
				continue
			}
		}
		memberships = append(memberships, cloneMembership(membership))
	}
	return limitSlice(memberships, query.Limit), nil
}

func (s *memoryConversationStore) AddReaction(_ context.Context, messageID int64, req AddReactionRequest, at time.Time) (*ChannelReaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	message, ok := s.messages[messageID]
	if !ok {
		return nil, notFound(ErrMessageNotFound)
	}
	for _, existing := range s.reactions {
		if existing.MessageID == messageID && existing.ReactorIdentity == req.ReactorIdentity && existing.Reaction == req.Reaction {
			existing.DeletedAt = nil
			return cloneReaction(existing), nil
		}
	}
	reaction := &ChannelReaction{
		ID:              s.nextReact,
		MessageID:       messageID,
		ChannelID:       message.ChannelID,
		ReactorType:     req.ReactorType,
		ReactorIdentity: req.ReactorIdentity,
		Reaction:        req.Reaction,
		CreatedAt:       at,
	}
	s.nextReact++
	s.reactions[reaction.ID] = reaction
	return cloneReaction(reaction), nil
}

func (s *memoryConversationStore) ListReadCursors(_ context.Context, channelID int64) ([]*ChannelReadCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cursors := make([]*ChannelReadCursor, 0, len(s.cursors))
	for _, cursor := range s.cursors {
		if cursor.ChannelID == channelID {
			cursors = append(cursors, cloneReadCursor(cursor))
		}
	}
	return cursors, nil
}

func (s *memoryConversationStore) UpsertReadCursor(_ context.Context, cursor *ChannelReadCursor) (*ChannelReadCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := cursorKey(cursor.ChannelID, cursor.ReaderType, cursor.ReaderIdentity)
	s.cursors[key] = cloneReadCursor(cursor)
	return cloneReadCursor(cursor), nil
}

func cloneChannel(channel *Channel) *Channel {
	if channel == nil {
		return nil
	}
	cloned := *channel
	cloned.Settings = slices.Clone(channel.Settings)
	return &cloned
}

func cloneMessage(message *ChannelMessage) *ChannelMessage {
	if message == nil {
		return nil
	}
	cloned := *message
	cloned.Metadata = slices.Clone(message.Metadata)
	return &cloned
}

func cloneMembership(membership *ChannelMembership) *ChannelMembership {
	if membership == nil {
		return nil
	}
	cloned := *membership
	cloned.Settings = slices.Clone(membership.Settings)
	return &cloned
}

func cloneReaction(reaction *ChannelReaction) *ChannelReaction {
	if reaction == nil {
		return nil
	}
	cloned := *reaction
	return &cloned
}

func cloneReadCursor(cursor *ChannelReadCursor) *ChannelReadCursor {
	if cursor == nil {
		return nil
	}
	cloned := *cursor
	return &cloned
}

func limitSlice[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func cursorKey(channelID int64, readerType string, readerIdentity string) string {
	return strconv.FormatInt(channelID, 10) + ":" + readerType + ":" + readerIdentity
}
