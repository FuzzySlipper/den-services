package messages

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceSendThreadReadAndWait(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	service := NewService(store, NoopProjectValidator{}, NoopTaskReader{}, func() time.Time { return now })
	taskID := int64(42)

	root, err := service.SendMessage(ctx, "rusty-roleplay", SendMessageRequest{
		TaskID:  &taskID,
		Sender:  "pi",
		Content: "Root note",
		Intent:  IntentQuestion,
	})
	if err != nil {
		t.Fatalf("SendMessage(root) error = %v", err)
	}
	reply, err := service.SendMessage(ctx, "rusty-roleplay", SendMessageRequest{
		TaskID:   &taskID,
		ThreadID: ptrInt64(root.ID()),
		Sender:   "coder",
		Content:  "Reply note",
	})
	if err != nil {
		t.Fatalf("SendMessage(reply) error = %v", err)
	}
	thread, err := service.GetThread(ctx, root.ID())
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if thread.Root.ID() != root.ID() || len(thread.Replies) != 1 || thread.Replies[0].ID() != reply.ID() {
		t.Fatalf("thread = %#v", thread)
	}
	if err := service.MarkRead(ctx, MarkReadRequest{Agent: "agent", MessageIDs: []int64{root.ID()}}); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	result, err := service.WaitForMessages(ctx, "rusty-roleplay", "agent", 0, 10, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForMessages() error = %v", err)
	}
	if len(result.Messages) != 1 || result.Messages[0].ID != reply.ID() {
		t.Fatalf("wait result = %#v", result)
	}
	self, err := service.SendMessage(ctx, "rusty-roleplay", SendMessageRequest{Sender: "agent", Content: "self"})
	if err != nil {
		t.Fatalf("SendMessage(self) error = %v", err)
	}
	result, err = service.WaitForMessages(ctx, "rusty-roleplay", "agent", self.ID()-1, 10, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForMessages(self) error = %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("self-authored message should not wake unread wait: %#v", result)
	}
}

func TestServiceSendMessageAcceptsGitHubCheckEvidenceIntents(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, NoopTaskReader{}, time.Now)
	taskID := int64(42)
	intents := []string{
		IntentGitHubChecksPassed,
		IntentGitHubChecksFailed,
		IntentGitHubChecksTimeout,
		IntentGitHubChecksSuperseded,
		IntentGitHubChecksUpdated,
	}

	for _, intent := range intents {
		intent := intent
		t.Run(intent, func(t *testing.T) {
			message, err := service.SendMessage(ctx, "den-services", SendMessageRequest{
				TaskID:  &taskID,
				Sender:  "review",
				Content: "GitHub check gate evidence",
				Intent:  intent,
			})
			if err != nil {
				t.Fatalf("SendMessage() error = %v", err)
			}
			if message.Intent() != intent {
				t.Fatalf("message intent = %s, want %s", message.Intent(), intent)
			}
		})
	}
}

func TestServiceUnreadCount(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, NoopTaskReader{}, time.Now)
	taskID := int64(42)

	first, err := service.SendMessage(ctx, "den-services", SendMessageRequest{TaskID: &taskID, Sender: "planner", Content: "One"})
	if err != nil {
		t.Fatalf("SendMessage(first) error = %v", err)
	}
	second, err := service.SendMessage(ctx, "den-services", SendMessageRequest{TaskID: &taskID, Sender: "reviewer", Content: "Two", Intent: IntentReviewFeedback})
	if err != nil {
		t.Fatalf("SendMessage(second) error = %v", err)
	}
	if _, err := service.SendMessage(ctx, "den-services", SendMessageRequest{TaskID: &taskID, Sender: "codex", Content: "Self"}); err != nil {
		t.Fatalf("SendMessage(self) error = %v", err)
	}
	if _, err := service.SendMessage(ctx, "other-project", SendMessageRequest{Sender: "planner", Content: "Other"}); err != nil {
		t.Fatalf("SendMessage(other) error = %v", err)
	}
	if err := service.MarkRead(ctx, MarkReadRequest{Agent: "codex", MessageIDs: []int64{first.ID()}}); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}

	count, err := service.UnreadCount(ctx, "den-services", UnreadCountQuery{UnreadFor: "codex"})
	if err != nil {
		t.Fatalf("UnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("UnreadCount() = %d, want 1", count)
	}
	feedbackCount, err := service.UnreadCount(ctx, "den-services", UnreadCountQuery{UnreadFor: "codex", Intent: IntentReviewFeedback})
	if err != nil {
		t.Fatalf("UnreadCount(intent) error = %v", err)
	}
	if feedbackCount != 1 {
		t.Fatalf("UnreadCount(intent) = %d, want 1", feedbackCount)
	}
	after := first.ID()
	afterCount, err := service.UnreadCount(ctx, "den-services", UnreadCountQuery{UnreadFor: "codex", AfterCursor: &after})
	if err != nil {
		t.Fatalf("UnreadCount(after) error = %v", err)
	}
	if afterCount != 1 || second.ID() <= after {
		t.Fatalf("UnreadCount(after) = %d, want second message only", afterCount)
	}
	if _, err := service.UnreadCount(ctx, "den-services", UnreadCountQuery{}); !errors.Is(err, ErrMissingUnreadFor) {
		t.Fatalf("UnreadCount(missing unread_for) error = %v, want ErrMissingUnreadFor", err)
	}
}

func TestServiceNotificationsAndPackets(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, NoopTaskReader{Tasks: map[int64]TaskContext{
		7: {ID: 7, ProjectID: "den-services", Title: "Implement messages", Description: "Build it", Status: "in_progress", Priority: 2},
	}}, time.Now)
	taskID := int64(7)

	notification, err := service.SendNotification(ctx, "den-services", SendNotificationRequest{
		TaskID:  &taskID,
		Sender:  "pi",
		Content: "Heads up",
		Urgency: "high",
		Metadata: map[string]any{
			"type": "agent_work_complete",
		},
	})
	if err != nil {
		t.Fatalf("SendNotification() error = %v", err)
	}
	items, err := service.ListNotifications(ctx, NotificationQuery{ProjectID: "den-services", ReadForAgent: "agent", HasReadFilter: true, IsRead: false})
	if err != nil {
		t.Fatalf("ListNotifications() error = %v", err)
	}
	if len(items) != 1 || items[0].Message.ID() != notification.ID() || items[0].Urgency != "high" || items[0].IsRead == nil || *items[0].IsRead {
		t.Fatalf("notification items = %#v", items)
	}
	if err := service.MarkNotificationsRead(ctx, MarkNotificationsReadRequest{Agent: "agent", NotificationIDs: []int64{notification.ID()}}); err != nil {
		t.Fatalf("MarkNotificationsRead() error = %v", err)
	}
	items, err = service.ListNotifications(ctx, NotificationQuery{ProjectID: "den-services", ReadForAgent: "agent", HasReadFilter: true, IsRead: true})
	if err != nil {
		t.Fatalf("ListNotifications(read) error = %v", err)
	}
	if len(items) != 1 || items[0].IsRead == nil || !*items[0].IsRead {
		t.Fatalf("read notification items = %#v", items)
	}
	scopedNotification, err := service.SendNotification(ctx, "den-services", SendNotificationRequest{
		TaskID:  &taskID,
		Sender:  "pi",
		Content: "Scoped heads up",
	})
	if err != nil {
		t.Fatalf("SendNotification(default urgency) error = %v", err)
	}
	if scopedNotification.Metadata()["urgency"] != DefaultUrgency {
		t.Fatalf("default urgency metadata = %#v", scopedNotification.Metadata())
	}
	if err := service.MarkNotificationsRead(ctx, MarkNotificationsReadRequest{Agent: "agent-2", MarkAll: true, ScopeProjectID: "den-services", ScopeTaskID: &taskID}); err != nil {
		t.Fatalf("MarkNotificationsRead(mark all) error = %v", err)
	}
	if err := service.MarkProjectNotificationsRead(ctx, "agent-3", "den-services"); err != nil {
		t.Fatalf("MarkProjectNotificationsRead() error = %v", err)
	}
	if err := service.MarkTaskNotificationsRead(ctx, "agent-4", "den-services", taskID); err != nil {
		t.Fatalf("MarkTaskNotificationsRead() error = %v", err)
	}
	items, err = service.ListNotifications(ctx, NotificationQuery{ProjectID: "den-services", ReadForAgent: "agent-2", HasReadFilter: true, IsRead: true})
	if err != nil {
		t.Fatalf("ListNotifications(mark all read) error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("mark-all read items len = %d, want 2: %#v", len(items), items)
	}
	if _, err := service.SendNotification(ctx, "den-services", SendNotificationRequest{Sender: "pi", Content: "bad", Urgency: "urgent"}); !errors.Is(err, ErrInvalidUrgency) {
		t.Fatalf("invalid urgency error = %v, want ErrInvalidUrgency", err)
	}
	if err := service.MarkNotificationsRead(ctx, MarkNotificationsReadRequest{Agent: "agent", MarkAll: true, ScopeProjectID: "den-services", NotificationIDs: []int64{notification.ID()}}); !errors.Is(err, ErrInvalidReadMode) {
		t.Fatalf("both read modes error = %v, want ErrInvalidReadMode", err)
	}
	if err := service.MarkNotificationsRead(ctx, MarkNotificationsReadRequest{Agent: "agent"}); !errors.Is(err, ErrInvalidReadMode) {
		t.Fatalf("no read mode error = %v, want ErrInvalidReadMode", err)
	}

	packet, err := service.CreateContextPacket(ctx, "den-services", taskID, CreateContextPacketRequest{PacketType: "coder_context_packet", Sender: "pi"})
	if err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}
	metadata := packet.Metadata()
	if metadata["schema"] != PacketSchema || metadata["role"] != "coder" || metadata["reference_only_launch"] != true {
		t.Fatalf("packet metadata = %#v", metadata)
	}
	if !hasText(packet.Content(), "This packet is a durable instruction record") {
		t.Fatalf("packet content missing safety text: %s", packet.Content())
	}
	latest, err := service.LatestTaskPacket(ctx, "den-services", taskID, "coder_context_packet", "coder")
	if err != nil {
		t.Fatalf("LatestTaskPacket() error = %v", err)
	}
	if latest.ID() != packet.ID() {
		t.Fatalf("latest packet id = %d, want %d", latest.ID(), packet.ID())
	}
	prompt, err := service.RenderWorkerPrompt(ctx, "den-services", packet.ID(), "")
	if err != nil {
		t.Fatalf("RenderWorkerPrompt() error = %v", err)
	}
	if !hasText(prompt.Prompt, "Do not infer executable state transitions") {
		t.Fatalf("prompt = %#v", prompt)
	}
	normalMessage, err := service.SendMessage(ctx, "den-services", SendMessageRequest{TaskID: &taskID, Sender: "pi", Content: "normal note"})
	if err != nil {
		t.Fatalf("SendMessage(normal) error = %v", err)
	}
	if _, err := service.RenderWorkerPrompt(ctx, "den-services", normalMessage.ID(), ""); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("RenderWorkerPrompt(normal) error = %v, want ErrInvalidPacket", err)
	}

	completion, err := service.AppendCompletionPacket(ctx, "den-services", taskID, AppendCompletionPacketRequest{Sender: "coder", Content: "done", Role: "coder", RunID: "run-1"})
	if err != nil {
		t.Fatalf("AppendCompletionPacket() error = %v", err)
	}
	latestCompletion, err := service.LatestCompletion(ctx, "den-services", &taskID, "coder", "run-1")
	if err != nil {
		t.Fatalf("LatestCompletion() error = %v", err)
	}
	if latestCompletion.ID() != completion.ID() {
		t.Fatalf("latest completion id = %d, want %d", latestCompletion.ID(), completion.ID())
	}
}

func ptrInt64(value int64) *int64 {
	return &value
}
