package conversation

import (
	"context"
	"time"
)

type ConversationStore interface {
	Ping(ctx context.Context) error
	CreateChannel(ctx context.Context, channel *Channel) (*Channel, error)
	ListChannels(ctx context.Context, query ListChannelsQuery) ([]*Channel, error)
	GetChannel(ctx context.Context, id int64) (*Channel, error)
	UpsertProjectDefaultChannel(ctx context.Context, projectID string, req PutDefaultChannelRequest, at time.Time) (*Channel, error)
	AppendMessage(ctx context.Context, message *ChannelMessage) (*ChannelMessage, error)
	ListMessages(ctx context.Context, query ListMessagesQuery) ([]*ChannelMessage, error)
	UpsertMembership(ctx context.Context, membership *ChannelMembership) (*ChannelMembership, error)
	ListMemberships(ctx context.Context, query ListMembershipsQuery) ([]*ChannelMembership, error)
	AddReaction(ctx context.Context, messageID int64, req AddReactionRequest, at time.Time) (*ChannelReaction, error)
	ListReadCursors(ctx context.Context, channelID int64) ([]*ChannelReadCursor, error)
	UpsertReadCursor(ctx context.Context, cursor *ChannelReadCursor) (*ChannelReadCursor, error)
}

type WakeTargetResolver interface {
	ResolveWakeTargets(ctx context.Context, memberships []*ChannelMembership) error
}

type Service struct {
	store       ConversationStore
	wakeTargets WakeTargetResolver
	clock       func() time.Time
	config      *Config
}

func NewService(store ConversationStore, wakeTargets WakeTargetResolver, clock func() time.Time, config *Config) *Service {
	if wakeTargets == nil {
		wakeTargets = NoopWakeTargetResolver{}
	}
	return &Service{
		store:       store,
		wakeTargets: wakeTargets,
		clock:       clock,
		config:      config,
	}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CreateChannel(ctx context.Context, req CreateChannelRequest) (*Channel, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	now := s.clock().UTC()
	return s.store.CreateChannel(ctx, &Channel{
		Slug:        req.Slug,
		DisplayName: req.DisplayName,
		Kind:        req.Kind,
		ProjectID:   req.ProjectID,
		SpaceID:     req.SpaceID,
		CreatedBy:   req.CreatedBy,
		Visibility:  req.Visibility,
		Settings:    defaultJSON(req.Settings),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func (s *Service) ListChannels(ctx context.Context, query ListChannelsQuery) ([]*Channel, error) {
	if query.Limit <= 0 || query.Limit > 1000 {
		return nil, badRequest(ErrInvalidLimit)
	}
	return s.store.ListChannels(ctx, query)
}

func (s *Service) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	if id <= 0 {
		return nil, badRequest(ErrInvalidChannel)
	}
	return s.store.GetChannel(ctx, id)
}

func (s *Service) PutDefaultChannel(ctx context.Context, projectID string, req PutDefaultChannelRequest) (*Channel, error) {
	if err := req.Validate(projectID); err != nil {
		return nil, badRequest(err)
	}
	return s.store.UpsertProjectDefaultChannel(ctx, projectID, req, s.clock().UTC())
}

func (s *Service) AppendMessage(ctx context.Context, channelID int64, req AppendMessageRequest, dedupeKey string) (*ChannelMessage, error) {
	if err := req.Validate(channelID, dedupeKey); err != nil {
		return nil, badRequest(err)
	}
	now := s.clock().UTC()
	return s.store.AppendMessage(ctx, &ChannelMessage{
		ChannelID:           channelID,
		SenderType:          req.SenderType,
		SenderIdentity:      req.SenderIdentity,
		Body:                req.Body,
		MessageKind:         req.MessageKind,
		SourceKind:          req.SourceKind,
		SourceID:            req.SourceID,
		SourceProjectID:     req.SourceProjectID,
		TargetProjectID:     req.TargetProjectID,
		TargetTaskID:        req.TargetTaskID,
		AssignmentID:        req.AssignmentID,
		WorkerRunID:         req.WorkerRunID,
		WorkerRole:          req.WorkerRole,
		ProfileIdentity:     req.ProfileIdentity,
		AgentInstanceID:     req.AgentInstanceID,
		PoolMemberID:        req.PoolMemberID,
		SessionOwnerID:      req.SessionOwnerID,
		SessionID:           req.SessionID,
		Summary:             req.Summary,
		DeepLink:            req.DeepLink,
		ThreadRootMessageID: req.ThreadRootMessageID,
		ReplyToMessageID:    req.ReplyToMessageID,
		Metadata:            defaultJSON(req.Metadata),
		DedupeKey:           &dedupeKey,
		CreatedAt:           now,
	})
}

func (s *Service) ListMessages(ctx context.Context, query ListMessagesQuery) ([]*ChannelMessage, error) {
	if query.ChannelID <= 0 {
		return nil, badRequest(ErrInvalidChannel)
	}
	if query.AfterID != nil && *query.AfterID <= 0 {
		return nil, badRequest(ErrInvalidMessage)
	}
	if query.Limit <= 0 || query.Limit > 1000 {
		return nil, badRequest(ErrInvalidLimit)
	}
	return s.store.ListMessages(ctx, query)
}

func (s *Service) PutMembership(ctx context.Context, channelID int64, req PutMembershipRequest) (*ChannelMembership, error) {
	if err := req.Validate(channelID); err != nil {
		return nil, badRequest(err)
	}
	canSend := true
	canReact := true
	canInvite := false
	if req.CanSend != nil {
		canSend = *req.CanSend
	}
	if req.CanReact != nil {
		canReact = *req.CanReact
	}
	if req.CanInvite != nil {
		canInvite = *req.CanInvite
	}
	now := s.clock().UTC()
	membership, err := s.store.UpsertMembership(ctx, &ChannelMembership{
		ChannelID:         channelID,
		MemberType:        req.MemberType,
		MemberIdentity:    req.MemberIdentity,
		ProfileIdentity:   req.ProfileIdentity,
		MembershipStatus:  req.MembershipStatus,
		WakePolicy:        req.WakePolicy,
		CanSend:           canSend,
		CanReact:          canReact,
		CanInvite:         canInvite,
		MembershipPurpose: req.MembershipPurpose,
		Settings:          defaultJSON(req.Settings),
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.wakeTargets.ResolveWakeTargets(ctx, []*ChannelMembership{membership}); err != nil {
		return nil, err
	}
	return membership, nil
}

func (s *Service) ListMemberships(ctx context.Context, query ListMembershipsQuery) ([]*ChannelMembership, error) {
	if query.ChannelID != nil && *query.ChannelID <= 0 {
		return nil, badRequest(ErrInvalidChannel)
	}
	if query.Limit <= 0 || query.Limit > 1000 {
		return nil, badRequest(ErrInvalidLimit)
	}
	memberships, err := s.store.ListMemberships(ctx, query)
	if err != nil {
		return nil, err
	}
	if err := s.wakeTargets.ResolveWakeTargets(ctx, memberships); err != nil {
		return nil, err
	}
	return memberships, nil
}

func (s *Service) AddReaction(ctx context.Context, messageID int64, req AddReactionRequest) (*ChannelReaction, error) {
	if err := req.Validate(messageID); err != nil {
		return nil, badRequest(err)
	}
	return s.store.AddReaction(ctx, messageID, req, s.clock().UTC())
}

func (s *Service) ListReadCursors(ctx context.Context, channelID int64) ([]*ChannelReadCursor, error) {
	if channelID <= 0 {
		return nil, badRequest(ErrInvalidChannel)
	}
	return s.store.ListReadCursors(ctx, channelID)
}

func (s *Service) PutReadCursor(ctx context.Context, channelID int64, req PutReadCursorRequest) (*ChannelReadCursor, error) {
	if err := req.Validate(channelID); err != nil {
		return nil, badRequest(err)
	}
	return s.store.UpsertReadCursor(ctx, &ChannelReadCursor{
		ChannelID:         channelID,
		ReaderType:        req.ReaderType,
		ReaderIdentity:    req.ReaderIdentity,
		InstanceID:        req.InstanceID,
		LastReadMessageID: req.LastReadMessageID,
		LastReadAt:        s.clock().UTC(),
	})
}
