package evaluator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type OpenAIClientConfig struct {
	BaseURL    string
	APIKey     string
	MaxRetries int
}

type OpenAIClient struct {
	cfg        OpenAIClientConfig
	httpClient *http.Client
}

func NewOpenAIClient(cfg OpenAIClientConfig, httpClient *http.Client) *OpenAIClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIClient{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (c *OpenAIClient) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	endpoint, err := chatCompletionsURL(c.cfg.BaseURL)
	if err != nil {
		return ChatResponse{}, err
	}
	body, err := json.Marshal(toOpenAIRequest(req))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("encoding chat request: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		response, err := c.post(ctx, endpoint, body)
		if err == nil {
			return response, nil
		}
		lastErr = err
	}
	return ChatResponse{}, lastErr
}

func (c *OpenAIClient) post(ctx context.Context, endpoint string, body []byte) (ChatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("building chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.cfg.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("posting chat request: %w", err)
	}
	defer resp.Body.Close()
	var decoded openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ChatResponse{}, fmt.Errorf("decoding chat response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ChatResponse{}, fmt.Errorf("chat provider returned status %d", resp.StatusCode)
	}
	if len(decoded.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("chat provider returned no choices")
	}
	return ChatResponse{Content: decoded.Choices[0].Message.Content}, nil
}

func chatCompletionsURL(baseURL string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("llm base_url is required")
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("parsing llm base_url: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/chat/completions"
	return parsed.String(), nil
}

type openAIRequest struct {
	Model           string          `json:"model"`
	Messages        []openAIMessage `json:"messages"`
	Temperature     float64         `json:"temperature"`
	MaxOutputTokens int             `json:"max_tokens"`
	ResponseFormat  responseFormat  `json:"response_format"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAIMessage struct {
	Role    string              `json:"role"`
	Content []openAIContentPart `json:"content"`
}

type openAIContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *openAIImage `json:"image_url,omitempty"`
}

type openAIImage struct {
	URL string `json:"url"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
	Message openAIResponseMessage `json:"message"`
}

type openAIResponseMessage struct {
	Content string `json:"content"`
}

func toOpenAIRequest(req ChatRequest) openAIRequest {
	messages := make([]openAIMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		parts := make([]openAIContentPart, 0, len(message.Content))
		for _, part := range message.Content {
			switch part.Type {
			case "image":
				parts = append(parts, openAIContentPart{
					Type: "image_url",
					ImageURL: &openAIImage{
						URL: "data:" + part.MimeType + ";base64," + base64.StdEncoding.EncodeToString(part.Data),
					},
				})
			default:
				parts = append(parts, openAIContentPart{Type: "text", Text: part.Text})
			}
		}
		messages = append(messages, openAIMessage{Role: message.Role, Content: parts})
	}
	return openAIRequest{
		Model:           req.Model,
		Messages:        messages,
		Temperature:     req.Temperature,
		MaxOutputTokens: req.MaxOutputTokens,
		ResponseFormat:  responseFormat{Type: "json_object"},
	}
}
