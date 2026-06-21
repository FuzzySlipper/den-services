package observation

import (
	"context"
	"sort"
	"strconv"
	"time"
)

type ObservationStore interface {
	AppendActivityEvent(ctx context.Context, event *ActivityEvent) (*ActivityEvent, error)
	ListActivityEvents(ctx context.Context, limit int) ([]LaneEvent, error)
	ListDeliveryEvents(ctx context.Context, limit int) ([]LaneEvent, error)
	ListRuntimeEvents(ctx context.Context, limit int) ([]LaneEvent, error)
	ListChatEvents(ctx context.Context, limit int) ([]LaneEvent, error)
	ListActivityEventsForAgent(ctx context.Context, agentID string, limit int) ([]LaneEvent, error)
	ListActiveWork(ctx context.Context) ([]ActiveWorkItem, error)
	ListRuntimeProjections(ctx context.Context, agentID string) ([]RuntimeProjection, error)
	ListActiveWorkForAgent(ctx context.Context, agentID string) ([]ActiveWorkItem, error)
	ListAgentIDs(ctx context.Context, limit int) ([]string, error)
	ListAssignmentMessages(ctx context.Context, assignmentID string, limit int) ([]AssignmentMessage, error)
	ListActivityEventsForAssignment(ctx context.Context, assignmentID string, limit int) ([]LaneEvent, error)
}

type ObservationService struct {
	store        ObservationStore
	clock        func() time.Time
	defaultLimit int
	maxLimit     int
}

func NewObservationService(store ObservationStore, clock func() time.Time, defaultLimit int, maxLimit int) *ObservationService {
	return &ObservationService{
		store:        store,
		clock:        clock,
		defaultLimit: defaultLimit,
		maxLimit:     maxLimit,
	}
}

func (s *ObservationService) AppendLifecycleEvent(ctx context.Context, req CreateLifecycleEventRequest) (*ActivityEvent, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	event, err := NewActivityEvent(req.SourceDomain, req.EventType, req.AgentIdentity, req.RuntimeInstanceID, req.Payload, s.clock())
	if err != nil {
		return nil, badRequest(err)
	}
	return s.store.AppendActivityEvent(ctx, event)
}

func (s *ObservationService) Lane(ctx context.Context, rawLimit string) ([]LaneEvent, error) {
	limit, err := s.parseLimit(rawLimit)
	if err != nil {
		return nil, badRequest(err)
	}
	events, err := s.collectLaneEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(left int, right int) bool {
		return events[left].CreatedAt.After(events[right].CreatedAt)
	})
	if len(events) > limit {
		return events[:limit], nil
	}
	return events, nil
}

func (s *ObservationService) ActiveWork(ctx context.Context) ([]ActiveWorkItem, error) {
	return s.store.ListActiveWork(ctx)
}

func (s *ObservationService) AgentsOverview(ctx context.Context, rawLimit string) (AgentsOverview, error) {
	limit, err := s.parseLimit(rawLimit)
	if err != nil {
		return AgentsOverview{}, badRequest(err)
	}
	agentIDs, err := s.store.ListAgentIDs(ctx, limit)
	if err != nil {
		return AgentsOverview{}, err
	}
	agents := make([]AgentOverviewSummary, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		overview, err := s.AgentOverview(ctx, agentID)
		if err != nil {
			return AgentsOverview{}, err
		}
		agents = append(agents, AgentOverviewSummary{
			AgentID:          overview.AgentID,
			RuntimeInstances: overview.RuntimeInstances,
			ActiveWork:       overview.ActiveWork,
			ActivityEvents:   overview.ActivityEvents,
		})
	}
	return AgentsOverview{Agents: agents}, nil
}

func (s *ObservationService) AgentOverview(ctx context.Context, agentID string) (AgentOverview, error) {
	if agentID == "" {
		return AgentOverview{}, badRequest(ErrInvalidQuery)
	}
	runtimes, err := s.store.ListRuntimeProjections(ctx, agentID)
	if err != nil {
		return AgentOverview{}, err
	}
	activeWork, err := s.store.ListActiveWorkForAgent(ctx, agentID)
	if err != nil {
		return AgentOverview{}, err
	}
	activityEvents, err := s.store.ListActivityEventsForAgent(ctx, agentID, s.defaultLimit)
	if err != nil {
		return AgentOverview{}, err
	}
	if len(runtimes) == 0 && len(activeWork) == 0 && len(activityEvents) == 0 {
		return AgentOverview{}, notFound("agent", agentID)
	}
	return AgentOverview{
		AgentID:          agentID,
		RuntimeInstances: runtimes,
		ActiveWork:       activeWork,
		ActivityEvents:   activityEvents,
	}, nil
}

func (s *ObservationService) AssignmentTranscript(ctx context.Context, assignmentID string, rawLimit string) (AssignmentTranscript, error) {
	if assignmentID == "" {
		return AssignmentTranscript{}, badRequest(ErrInvalidQuery)
	}
	limit, err := s.parseLimit(rawLimit)
	if err != nil {
		return AssignmentTranscript{}, badRequest(err)
	}
	messages, err := s.store.ListAssignmentMessages(ctx, assignmentID, limit)
	if err != nil {
		return AssignmentTranscript{}, err
	}
	return AssignmentTranscript{AssignmentID: assignmentID, Messages: messages}, nil
}

func (s *ObservationService) AssignmentTrace(ctx context.Context, assignmentID string, rawLimit string) (AssignmentTrace, error) {
	transcript, err := s.AssignmentTranscript(ctx, assignmentID, rawLimit)
	if err != nil {
		return AssignmentTrace{}, err
	}
	limit := s.defaultLimit
	if rawLimit != "" {
		parsed, parseErr := s.parseLimit(rawLimit)
		if parseErr != nil {
			return AssignmentTrace{}, badRequest(parseErr)
		}
		limit = parsed
	}
	activity, err := s.store.ListActivityEventsForAssignment(ctx, assignmentID, limit)
	if err != nil {
		return AssignmentTrace{}, err
	}
	transcriptAvailability := "available"
	if len(transcript.Messages) == 0 {
		transcriptAvailability = "no_assignment_messages"
	}
	activityAvailability := "available"
	if len(activity) == 0 {
		activityAvailability = "no_activity_events"
	}
	summary := "Observation successor trace: display-only transcript and activity evidence; executable assignment state remains Core/Delivery-owned."
	return AssignmentTrace{
		AssignmentID:           assignmentID,
		Transcript:             transcript.Messages,
		ActivityEvents:         activity,
		TranscriptAvailability: transcriptAvailability,
		ActivityAvailability:   activityAvailability,
		Summary:                &summary,
	}, nil
}

func (s *ObservationService) ActivityHistory(ctx context.Context, rawLimit string, agentID string, assignmentID string) ([]LaneEvent, error) {
	limit, err := s.parseLimit(rawLimit)
	if err != nil {
		return nil, badRequest(err)
	}
	if assignmentID != "" {
		return s.store.ListActivityEventsForAssignment(ctx, assignmentID, limit)
	}
	if agentID != "" {
		return s.store.ListActivityEventsForAgent(ctx, agentID, limit)
	}
	return s.store.ListActivityEvents(ctx, limit)
}

func (s *ObservationService) collectLaneEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	var events []LaneEvent
	activity, err := s.store.ListActivityEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	events = append(events, activity...)
	delivery, err := s.store.ListDeliveryEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	events = append(events, delivery...)
	runtimeEvents, err := s.store.ListRuntimeEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	events = append(events, runtimeEvents...)
	chat, err := s.store.ListChatEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	events = append(events, chat...)
	return events, nil
}

func (s *ObservationService) parseLimit(raw string) (int, error) {
	if raw == "" {
		return s.defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalidQuery
	}
	if limit <= 0 || limit > s.maxLimit {
		return 0, ErrInvalidQuery
	}
	return limit, nil
}
