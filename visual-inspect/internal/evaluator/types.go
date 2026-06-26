package evaluator

import (
	"context"
	"time"

	"den-services/visual-inspect/internal/schema"
)

type Image struct {
	ScreenshotID string
	MimeType     string
	Bytes        []byte
	Width        int
	Height       int
	SHA256       string
	Sensitive    bool
}

type Evaluator interface {
	Evaluate(ctx context.Context, req schema.EvaluateRequest, images []Image) (schema.EvaluateResponse, error)
}

type Config struct {
	Provider        string
	BaseURL         string
	APIKey          string
	Model           string
	Temperature     float64
	Timeout         time.Duration
	MaxOutputTokens int
	MaxRetries      int
	DefaultProfile  string
	Profiles        map[string]PromptProfile
}

type PromptProfile struct {
	Name                 string
	SystemPromptFile     string
	DeveloperPromptFile  string
	ResponseSchemaFile   string
	MinConfidenceForPass float64
	MinConfidenceForFail float64
}

type ProviderClient interface {
	Complete(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type ChatRequest struct {
	Provider        string
	Model           string
	Temperature     float64
	MaxOutputTokens int
	Messages        []ChatMessage
}

type ChatMessage struct {
	Role    string
	Content []ContentPart
}

type ContentPart struct {
	Type     string
	Text     string
	MimeType string
	Data     []byte
}

type ChatResponse struct {
	Content string
}
