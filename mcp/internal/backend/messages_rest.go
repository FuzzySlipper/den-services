package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"den-services/mcp/internal/config"
)

type messagesToolArguments struct {
	ProjectID               string          `json:"project_id"`
	TaskID                  *int64          `json:"task_id"`
	ThreadID                *int64          `json:"thread_id"`
	MessageID               int64           `json:"message_id"`
	PacketMessageID         int64           `json:"packet_message_id"`
	Sender                  string          `json:"sender"`
	Content                 string          `json:"content"`
	Intent                  string          `json:"intent"`
	Metadata                json.RawMessage `json:"metadata"`
	Agent                   string          `json:"agent"`
	MessageIDs              json.RawMessage `json:"message_ids"`
	NotificationIDs         json.RawMessage `json:"notification_ids"`
	Urgency                 string          `json:"urgency"`
	UnreadFor               string          `json:"unread_for"`
	Cursor                  *int64          `json:"cursor"`
	Limit                   *int            `json:"limit"`
	TimeoutMS               *int            `json:"timeout_ms"`
	Since                   *string         `json:"since"`
	Verbose                 *bool           `json:"verbose"`
	PacketType              string          `json:"packet_type"`
	Role                    string          `json:"role"`
	RunID                   string          `json:"run_id"`
	CompletionReportingMode string          `json:"completion_reporting_mode"`
	MetadataType            string          `json:"metadata_type"`
	ReadForAgent            string          `json:"read_for_agent"`
	IsRead                  *bool           `json:"is_read"`
	Offset                  *int            `json:"offset"`
}

type sendMessageBody struct {
	TaskID   *int64         `json:"task_id,omitempty"`
	ThreadID *int64         `json:"thread_id,omitempty"`
	Sender   string         `json:"sender"`
	Content  string         `json:"content"`
	Intent   string         `json:"intent,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type markReadBody struct {
	Agent      string  `json:"agent"`
	MessageIDs []int64 `json:"message_ids,omitempty"`
}

type sendNotificationBody struct {
	TaskID   *int64         `json:"task_id,omitempty"`
	Sender   string         `json:"sender"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Urgency  string         `json:"urgency,omitempty"`
}

type markNotificationsReadBody struct {
	Agent           string  `json:"agent"`
	NotificationIDs []int64 `json:"notification_ids,omitempty"`
}

type markScopedNotificationsReadBody struct {
	Agent string `json:"agent"`
}

func (c *Client) callMessagesREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildMessagesRESTRequest(ctx, backend, route, call)
	if err != nil {
		return Result{}, nil, err
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return Result{}, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{}, nil, fmt.Errorf("reading messages backend response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, responseBody), nil
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func buildMessagesRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeMessagesToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := messagesRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := messagesRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building messages backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func decodeMessagesToolArguments(raw json.RawMessage) (messagesToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments messagesToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return messagesToolArguments{}, fmt.Errorf("decoding messages tool arguments: %w", err)
	}
	return arguments, nil
}

func messagesRESTRequestBody(operation string, arguments messagesToolArguments) ([]byte, error) {
	metadata, err := parseObject(arguments.Metadata)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "send_message":
		return json.Marshal(sendMessageBody{
			TaskID:   arguments.TaskID,
			ThreadID: arguments.ThreadID,
			Sender:   strings.TrimSpace(arguments.Sender),
			Content:  arguments.Content,
			Intent:   strings.TrimSpace(arguments.Intent),
			Metadata: metadata,
		})
	case "mark_read":
		messageIDs, err := parseInt64List(arguments.MessageIDs)
		if err != nil {
			return nil, err
		}
		return json.Marshal(markReadBody{Agent: strings.TrimSpace(arguments.Agent), MessageIDs: messageIDs})
	case "send_user_notification":
		return json.Marshal(sendNotificationBody{
			TaskID:   arguments.TaskID,
			Sender:   strings.TrimSpace(arguments.Sender),
			Content:  arguments.Content,
			Metadata: metadata,
			Urgency:  strings.TrimSpace(arguments.Urgency),
		})
	case "mark_notifications_read":
		notificationIDs, err := parseInt64List(arguments.NotificationIDs)
		if err != nil {
			return nil, err
		}
		return json.Marshal(markNotificationsReadBody{
			Agent:           strings.TrimSpace(arguments.Agent),
			NotificationIDs: notificationIDs,
		})
	case "mark_project_notifications_read":
		return json.Marshal(markScopedNotificationsReadBody{Agent: strings.TrimSpace(arguments.Agent)})
	case "mark_task_notifications_read":
		return json.Marshal(markScopedNotificationsReadBody{Agent: strings.TrimSpace(arguments.Agent)})
	case "get_messages", "wait_for_messages", "get_thread", "get_user_notifications", "get_latest_task_packet", "render_worker_prompt", "get_latest_worker_completion":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: messages operation %s", ErrUnsupportedAdapter, operation)
	}
}

func messagesRESTURL(baseURL string, route Route, arguments messagesToolArguments) (string, error) {
	routePath, err := expandMessagesPath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing messages backend URL: %w", err)
	}
	query := parsedURL.Query()
	switch route.Operation {
	case "get_messages":
		setInt64Query(query, "task_id", arguments.TaskID)
		setStringQuery(query, "since", arguments.Since)
		setStringValueQuery(query, "unread_for", arguments.UnreadFor)
		setStringValueQuery(query, "intent", arguments.Intent)
		setIntQuery(query, "limit", arguments.Limit)
		setBoolQuery(query, "verbose", arguments.Verbose)
	case "wait_for_messages":
		setStringValueQuery(query, "unread_for", arguments.UnreadFor)
		setInt64Query(query, "cursor", arguments.Cursor)
		setIntQuery(query, "limit", arguments.Limit)
		setIntQuery(query, "timeout_ms", arguments.TimeoutMS)
	case "get_user_notifications":
		setStringValueQuery(query, "project_id", arguments.ProjectID)
		setInt64Query(query, "task_id", arguments.TaskID)
		setStringValueQuery(query, "sender", arguments.Sender)
		setStringValueQuery(query, "metadata_type", arguments.MetadataType)
		setStringValueQuery(query, "urgency", arguments.Urgency)
		setStringValueQuery(query, "read_for_agent", arguments.ReadForAgent)
		setBoolQuery(query, "is_read", arguments.IsRead)
		setIntQuery(query, "limit", arguments.Limit)
		setIntQuery(query, "offset", arguments.Offset)
		setBoolQuery(query, "verbose", arguments.Verbose)
	case "get_latest_task_packet":
		setStringValueQuery(query, "packet_type", arguments.PacketType)
		setStringValueQuery(query, "role", arguments.Role)
		setBoolQuery(query, "verbose", arguments.Verbose)
	case "get_thread":
		setBoolQuery(query, "verbose", arguments.Verbose)
	case "render_worker_prompt":
		setStringValueQuery(query, "completion_reporting_mode", arguments.CompletionReportingMode)
	case "get_latest_worker_completion":
		setStringValueQuery(query, "role", arguments.Role)
		setStringValueQuery(query, "run_id", arguments.RunID)
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func expandMessagesPath(path string, arguments messagesToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("messages route requires project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(result, "{task_id}") {
		if arguments.TaskID == nil || *arguments.TaskID == 0 {
			return "", fmt.Errorf("messages route requires task_id")
		}
		result = strings.ReplaceAll(result, "{task_id}", strconv.FormatInt(*arguments.TaskID, 10))
	}
	if strings.Contains(result, "{thread_id}") {
		if arguments.ThreadID == nil || *arguments.ThreadID == 0 {
			return "", fmt.Errorf("messages route requires thread_id")
		}
		result = strings.ReplaceAll(result, "{thread_id}", strconv.FormatInt(*arguments.ThreadID, 10))
	}
	if strings.Contains(result, "{message_id}") {
		messageID := arguments.MessageID
		if messageID == 0 {
			messageID = arguments.PacketMessageID
		}
		if messageID == 0 {
			return "", fmt.Errorf("messages route requires message_id")
		}
		result = strings.ReplaceAll(result, "{message_id}", strconv.FormatInt(messageID, 10))
	}
	return result, nil
}

func parseObject(raw json.RawMessage) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var object map[string]any
	if err := json.Unmarshal(trimmed, &object); err == nil {
		return object, nil
	}
	var encoded string
	if err := json.Unmarshal(trimmed, &encoded); err != nil {
		return nil, fmt.Errorf("decoding JSON object: %w", err)
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(encoded), &object); err != nil {
		return nil, fmt.Errorf("decoding JSON-encoded object: %w", err)
	}
	return object, nil
}

func setStringValueQuery(query url.Values, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	query.Set(key, strings.TrimSpace(value))
}

func setBoolQuery(query url.Values, key string, value *bool) {
	if value == nil {
		return
	}
	query.Set(key, strconv.FormatBool(*value))
}
