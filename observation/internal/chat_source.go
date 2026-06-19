package observation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"den-services/shared/identity"
)

type ChatEventSource interface {
	ListChatEvents(ctx context.Context, limit int) ([]LaneEvent, error)
}

type StoreWithChatSource struct {
	ObservationStore
	chatSource ChatEventSource
}

func NewStoreWithChatSource(store ObservationStore, chatSource ChatEventSource) ObservationStore {
	if chatSource == nil {
		return store
	}
	return &StoreWithChatSource{ObservationStore: store, chatSource: chatSource}
}

func (s *StoreWithChatSource) ListChatEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	return s.chatSource.ListChatEvents(ctx, limit)
}

type LegacyChannelsChatSource struct {
	baseURL    string
	httpClient *http.Client
}

func NewLegacyChannelsChatSource(baseURL string, timeout time.Duration) (*LegacyChannelsChatSource, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil, fmt.Errorf("legacy channels base URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("legacy channels base URL must be absolute: %s", baseURL)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("legacy channels timeout must be positive")
	}
	return &LegacyChannelsChatSource{
		baseURL:    trimmed,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (s *LegacyChannelsChatSource) ListChatEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	channels, err := s.listChannels(ctx, limit)
	if err != nil {
		return nil, err
	}
	events := make([]LaneEvent, 0, limit)
	for _, channel := range channels {
		messages, err := s.listMessages(ctx, channel.ID, limit)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if !isDisplayConversationMessage(message) {
				continue
			}
			event, err := legacyMessageToLaneEvent(message)
			if err != nil {
				return nil, err
			}
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *LegacyChannelsChatSource) listChannels(ctx context.Context, limit int) ([]legacyChannelDTO, error) {
	requestURL := fmt.Sprintf("%s/api/channels?limit=%d", s.baseURL, limit)
	var channels []legacyChannelDTO
	if err := s.getJSON(ctx, requestURL, &channels); err != nil {
		return nil, fmt.Errorf("listing legacy channels: %w", err)
	}
	return channels, nil
}

func (s *LegacyChannelsChatSource) listMessages(ctx context.Context, channelID int64, limit int) ([]legacyMessageDTO, error) {
	requestURL := fmt.Sprintf("%s/api/channels/%d/messages?limit=%d", s.baseURL, channelID, limit)
	var messages []legacyMessageDTO
	if err := s.getJSON(ctx, requestURL, &messages); err != nil {
		return nil, fmt.Errorf("listing legacy channel %d messages: %w", channelID, err)
	}
	return messages, nil
}

func (s *LegacyChannelsChatSource) getJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	res, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("GET %s returned %s", requestURL, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(target)
}

type legacyChannelDTO struct {
	ID int64 `json:"id"`
}

type legacyMessageDTO struct {
	ID             int64           `json:"id"`
	ChannelID      int64           `json:"channelId"`
	SenderIdentity string          `json:"senderIdentity"`
	Body           string          `json:"body"`
	MessageKind    string          `json:"messageKind"`
	SourceKind     *string         `json:"sourceKind"`
	CreatedAt      legacyTimestamp `json:"createdAt"`
}

type legacyTimestamp struct {
	time.Time
}

func (t *legacyTimestamp) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := parseLegacyTimestamp(raw)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

func parseLegacyTimestamp(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	var lastErr error
	for _, format := range formats {
		parsed, err := time.Parse(format, trimmed)
		if err == nil {
			return parsed.UTC(), nil
		}
		lastErr = err
	}
	return time.Time{}, fmt.Errorf("parsing timestamp %q: %w", raw, lastErr)
}

func isDisplayConversationMessage(message legacyMessageDTO) bool {
	if strings.TrimSpace(message.Body) == "" || message.CreatedAt.IsZero() {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(message.MessageKind))
	if kind != "" && kind != "human_text" && kind != "agent_text" && kind != "system_message" && kind != "mirror_summary" {
		return false
	}
	if message.SourceKind == nil {
		return true
	}
	source := strings.ToLower(strings.TrimSpace(*message.SourceKind))
	// Exclude haunted executable delivery rows and observability rows from the
	// conversation read surface. Observation imports only display chat summaries;
	// delivery and lifecycle authority remain in their owning domains.
	return source == "" || source == "task_message" || source == "agent_stream_entry"
}

func legacyMessageToLaneEvent(message legacyMessageDTO) (LaneEvent, error) {
	author := identity.AgentIdentity{
		Profile:    identity.ProfileIdentity(message.SenderIdentity),
		InstanceID: identity.AgentInstanceID("legacy-channel-message:" + strconv.FormatInt(message.ID, 10)),
	}
	if !author.IsValid() {
		author = identity.AgentIdentity{
			Profile:    identity.ProfileIdentity("legacy-channel-author"),
			InstanceID: identity.AgentInstanceID("legacy-channel-message:" + strconv.FormatInt(message.ID, 10)),
		}
	}
	payload, err := json.Marshal(map[string]any{
		"channel_id": message.ChannelID,
		"body":       message.Body,
		"source":     "legacy_den_channels_http",
	})
	if err != nil {
		return LaneEvent{}, err
	}
	return LaneEvent{
		EventID:       "conversation:" + strconv.FormatInt(message.ID, 10),
		SourceDomain:  SourceDomainConversation,
		EventType:     "message",
		AgentIdentity: &author,
		Payload:       payload,
		DisplayOnly:   true,
		CreatedAt:     message.CreatedAt.Time.UTC(),
	}, nil
}
