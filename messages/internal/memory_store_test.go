package messages

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
)

type memoryStore struct {
	mu       sync.Mutex
	nextID   int64
	messages []*Message
	reads    map[int64]map[string]bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{nextID: 1, reads: map[int64]map[string]bool{}}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) CreateMessage(_ context.Context, message *Message) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, err := NewMessage(NewMessageParams{
		ID:        s.nextID,
		ProjectID: message.ProjectID(),
		TaskID:    message.TaskID(),
		ThreadID:  message.ThreadID(),
		Sender:    message.Sender(),
		Content:   message.Content(),
		Intent:    message.Intent(),
		Metadata:  message.Metadata(),
		CreatedAt: message.CreatedAt(),
	})
	if err != nil {
		return nil, err
	}
	s.nextID++
	s.messages = append(s.messages, created)
	return created, nil
}

func (s *memoryStore) GetMessage(_ context.Context, id int64) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, message := range s.messages {
		if message.ID() == id {
			return message, nil
		}
	}
	return nil, notFound(id)
}

func (s *memoryStore) ListMessages(_ context.Context, query ListMessagesQuery) ([]*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []*Message
	for _, message := range s.messages {
		if message.ProjectID() != query.ProjectID {
			continue
		}
		if query.TaskID != nil && (message.TaskID() == nil || *message.TaskID() != *query.TaskID) {
			continue
		}
		if query.Since != nil && !message.CreatedAt().After(*query.Since) {
			continue
		}
		if query.Intent != "" && message.Intent() != query.Intent {
			continue
		}
		if query.UnreadFor != "" && (message.Sender() == query.UnreadFor || s.isRead(message.ID(), query.UnreadFor)) {
			continue
		}
		found = append(found, message)
	}
	sortMessagesDesc(found)
	return limitMessages(found, query.Limit), nil
}

func (s *memoryStore) UnreadAfterCursor(_ context.Context, projectID string, unreadFor string, cursor int64, limit int) ([]*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []*Message
	for _, message := range s.messages {
		if message.ProjectID() == projectID && message.ID() > cursor && message.Sender() != unreadFor && !s.isRead(message.ID(), unreadFor) {
			found = append(found, message)
		}
	}
	sortMessagesDesc(found)
	return limitMessages(found, limit), nil
}

func (s *memoryStore) GetThread(ctx context.Context, id int64) (Thread, error) {
	root, err := s.GetMessage(ctx, id)
	if err != nil {
		return Thread{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var replies []*Message
	for _, message := range s.messages {
		if message.ThreadID() != nil && *message.ThreadID() == id {
			replies = append(replies, message)
		}
	}
	slices.SortFunc(replies, func(left *Message, right *Message) int {
		if left.CreatedAt().Equal(right.CreatedAt()) {
			return int(left.ID() - right.ID())
		}
		if left.CreatedAt().Before(right.CreatedAt()) {
			return -1
		}
		return 1
	})
	return Thread{Root: root, Replies: replies}, nil
}

func (s *memoryStore) MarkRead(_ context.Context, agent string, ids []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if s.reads[id] == nil {
			s.reads[id] = map[string]bool{}
		}
		s.reads[id][agent] = true
	}
	return nil
}

func (s *memoryStore) ListNotifications(_ context.Context, query NotificationQuery) ([]NotificationItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []NotificationItem
	for _, message := range s.messages {
		if message.Intent() != IntentNotification {
			continue
		}
		if query.ProjectID != "" && message.ProjectID() != query.ProjectID {
			continue
		}
		if query.TaskID != nil && (message.TaskID() == nil || *message.TaskID() != *query.TaskID) {
			continue
		}
		if query.Sender != "" && message.Sender() != query.Sender {
			continue
		}
		metadata := message.Metadata()
		if query.MetadataType != "" && metadata["type"] != query.MetadataType {
			continue
		}
		urgency, _ := metadata["urgency"].(string)
		if urgency == "" {
			urgency = DefaultUrgency
		}
		if query.Urgency != "" && urgency != query.Urgency {
			continue
		}
		var isRead *bool
		if query.ReadForAgent != "" {
			read := s.isRead(message.ID(), query.ReadForAgent)
			isRead = &read
			if query.HasReadFilter && read != query.IsRead {
				continue
			}
		}
		items = append(items, NotificationItem{Message: message, Urgency: urgency, IsRead: isRead})
	}
	sort.Slice(items, func(left int, right int) bool {
		return items[left].Message.ID() > items[right].Message.ID()
	})
	if query.Offset >= len(items) {
		return nil, nil
	}
	items = items[query.Offset:]
	if query.Limit > 0 && len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *memoryStore) MarkNotificationsRead(_ context.Context, agent string, ids []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		for _, message := range s.messages {
			if message.ID() == id && message.Intent() == IntentNotification {
				if s.reads[id] == nil {
					s.reads[id] = map[string]bool{}
				}
				s.reads[id][agent] = true
			}
		}
	}
	return nil
}

func (s *memoryStore) MarkAllNotificationsRead(_ context.Context, agent string, projectID string, taskID *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, message := range s.messages {
		if message.Intent() != IntentNotification || message.ProjectID() != projectID {
			continue
		}
		if taskID != nil && (message.TaskID() == nil || *message.TaskID() != *taskID) {
			continue
		}
		if s.reads[message.ID()] == nil {
			s.reads[message.ID()] = map[string]bool{}
		}
		s.reads[message.ID()][agent] = true
	}
	return nil
}

func (s *memoryStore) LatestTaskPacket(_ context.Context, projectID string, taskID int64, packetType string, role string) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.messages) - 1; i >= 0; i-- {
		message := s.messages[i]
		metadata := message.Metadata()
		if message.ProjectID() == projectID &&
			message.TaskID() != nil && *message.TaskID() == taskID &&
			metadata["schema"] == PacketSchema &&
			(packetType == "" || metadata["type"] == packetType || metadata["packet_kind"] == packetType) &&
			(role == "" || metadata["role"] == role) {
			return message, nil
		}
	}
	return nil, notFound(0)
}

func (s *memoryStore) LatestCompletion(_ context.Context, projectID string, taskID *int64, role string, runID string) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.messages) - 1; i >= 0; i-- {
		message := s.messages[i]
		metadata := message.Metadata()
		completion, _ := metadata["completion_packet"].(bool)
		if message.ProjectID() == projectID &&
			(taskID == nil || (message.TaskID() != nil && *message.TaskID() == *taskID)) &&
			(metadata["schema"] == CompletionSchema || completion) &&
			(role == "" || metadata["role"] == role) &&
			(runID == "" || metadata["run_id"] == runID || metadata["session_id"] == runID) {
			return message, nil
		}
	}
	return nil, notFound(0)
}

func (s *memoryStore) isRead(messageID int64, agent string) bool {
	return s.reads[messageID] != nil && s.reads[messageID][agent]
}

func sortMessagesDesc(messages []*Message) {
	slices.SortFunc(messages, func(left *Message, right *Message) int {
		if left.CreatedAt().Equal(right.CreatedAt()) {
			return int(right.ID() - left.ID())
		}
		if left.CreatedAt().After(right.CreatedAt()) {
			return -1
		}
		return 1
	})
}

func limitMessages(messages []*Message, limit int) []*Message {
	if limit > 0 && len(messages) > limit {
		return messages[:limit]
	}
	return messages
}

func hasText(value string, want string) bool {
	return strings.Contains(value, want)
}
