package evaluator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClientPostsStatelessVisionRequest(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var decoded openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		data, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("marshalling request: %v", err)
		}
		body = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdict\":\"uncertain\"}"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIClientConfig{
		BaseURL: server.URL + "/v1",
		APIKey:  "test-key",
	}, server.Client())

	response, err := client.Complete(t.Context(), ChatRequest{
		Model:           "vision-test",
		Temperature:     0,
		MaxOutputTokens: 100,
		Messages: []ChatMessage{{
			Role: "user",
			Content: []ContentPart{
				{Type: "text", Text: "inspect this"},
				{Type: "image", MimeType: "image/png", Data: []byte("image-bytes")},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if response.Content != `{"verdict":"uncertain"}` {
		t.Fatalf("Content = %q", response.Content)
	}
	if !strings.Contains(body, `"response_format":{"type":"json_object"}`) {
		t.Fatalf("missing json_object response format: %s", body)
	}
	if !strings.Contains(body, `"image_url"`) || !strings.Contains(body, `data:image/png;base64,`) {
		t.Fatalf("missing image attachment: %s", body)
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "session") || strings.Contains(lower, "thread") {
		t.Fatalf("request contains persistent conversation handle: %s", body)
	}
}
