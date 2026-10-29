package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// defaultOpsURL is web-edge on the Den service host, reachable from the LAN;
// it forwards /api/v1/ops to Gateway and on to the Ops service.
const defaultOpsURL = "http://192.168.1.5:18080/api"

const opsRequestTimeout = 20 * time.Second

type OpsClient struct {
	baseURL    string
	httpClient *http.Client
}

// OpsHTTPError is a response Ops actually sent. A 4xx means the report itself
// is wrong and resending it cannot help; anything else is worth retrying.
type OpsHTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *OpsHTTPError) Error() string {
	message := strings.TrimSpace(string(e.Body))
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	return fmt.Sprintf("ops service returned HTTP %d: %s", e.StatusCode, message)
}

func (e *OpsHTTPError) Permanent() bool { return e.StatusCode >= 400 && e.StatusCode < 500 }

func NewOpsClient(baseURL string, httpClient *http.Client) (*OpsClient, error) {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse ops URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("ops URL %q must be an http(s) URL with a host", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: opsRequestTimeout}
	}
	return &OpsClient{baseURL: normalized, httpClient: httpClient}, nil
}

func OpsClientFromEnv() (*OpsClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("DEN_OPS_URL"))
	if baseURL == "" {
		baseURL = defaultOpsURL
	}
	return NewOpsClient(baseURL, nil)
}

// Do sends one request to an Ops path such as /v1/ops/summary and returns the
// response body unchanged.
func (c *OpsClient) Do(ctx context.Context, method string, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build ops request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ops service unreachable: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read ops response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &OpsHTTPError{StatusCode: response.StatusCode, Body: payload}
	}
	return payload, nil
}

func opsPath(segments ...string) string {
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return "/v1/ops/" + strings.Join(escaped, "/")
}

// opsSpoolEntry is one undelivered write, kept on disk until Ops accepts it.
type opsSpoolEntry struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Body     json.RawMessage `json:"body"`
	QueuedAt time.Time       `json:"queued_at"`
}

// OpsSpool holds reports that could not be delivered. Ops treats repeats and
// out-of-order replays as harmless, so replaying everything is always safe.
type OpsSpool struct{ dir string }

func OpsSpoolFromEnv() (*OpsSpool, error) {
	if dir := strings.TrimSpace(os.Getenv("DEN_OPS_SPOOL")); dir != "" {
		return &OpsSpool{dir: dir}, nil
	}
	if state := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); state != "" {
		return &OpsSpool{dir: filepath.Join(state, "den-tool", "ops-spool")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate ops spool: set DEN_OPS_SPOOL: %w", err)
	}
	return &OpsSpool{dir: filepath.Join(home, ".local", "state", "den-tool", "ops-spool")}, nil
}

func (s *OpsSpool) Dir() string { return s.dir }

func (s *OpsSpool) Add(method string, path string, body []byte, now time.Time) (string, error) {
	if !json.Valid(body) {
		return "", errors.New("refusing to spool a body that is not JSON")
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return "", fmt.Errorf("create ops spool: %w", err)
	}
	entry, err := json.Marshal(opsSpoolEntry{Method: method, Path: path, Body: body, QueuedAt: now.UTC()})
	if err != nil {
		return "", fmt.Errorf("encode spool entry: %w", err)
	}
	name := fmt.Sprintf("%020d-%s.json", now.UnixNano(), spoolSlug(path))
	temporary := filepath.Join(s.dir, "."+name+".tmp")
	if err := os.WriteFile(temporary, entry, 0o640); err != nil {
		return "", fmt.Errorf("write spool entry: %w", err)
	}
	final := filepath.Join(s.dir, name)
	if err := os.Rename(temporary, final); err != nil {
		return "", fmt.Errorf("commit spool entry: %w", err)
	}
	return final, nil
}

type OpsReplayResult struct {
	Delivered int      `json:"delivered"`
	Pending   int      `json:"pending"`
	Rejected  []string `json:"rejected"`
	Error     string   `json:"error,omitempty"`
}

// Replay sends spooled entries oldest first. It stops at the first transport
// or server failure so ordering is kept; entries Ops rejects as invalid move
// to rejected/ instead of blocking the queue forever.
func (s *OpsSpool) Replay(ctx context.Context, client *OpsClient) OpsReplayResult {
	result := OpsReplayResult{Rejected: []string{}}
	entries, err := s.pending()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for index, name := range entries {
		path := filepath.Join(s.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			result.Error = fmt.Sprintf("read %s: %v", name, err)
			result.Pending = len(entries) - index
			return result
		}
		var entry opsSpoolEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			s.reject(name)
			result.Rejected = append(result.Rejected, name)
			continue
		}
		_, err = client.Do(ctx, entry.Method, entry.Path, entry.Body)
		var httpErr *OpsHTTPError
		switch {
		case err == nil:
			_ = os.Remove(path)
			result.Delivered++
		case errors.As(err, &httpErr) && httpErr.Permanent():
			s.reject(name)
			result.Rejected = append(result.Rejected, name)
		default:
			result.Error = err.Error()
			result.Pending = len(entries) - index
			return result
		}
	}
	return result
}

func (s *OpsSpool) pending() ([]string, error) {
	items, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ops spool: %w", err)
	}
	var names []string
	for _, item := range items {
		if item.Type().IsRegular() && strings.HasSuffix(item.Name(), ".json") && !strings.HasPrefix(item.Name(), ".") {
			names = append(names, item.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

func (s *OpsSpool) reject(name string) {
	rejected := filepath.Join(s.dir, "rejected")
	if err := os.MkdirAll(rejected, 0o750); err == nil {
		_ = os.Rename(filepath.Join(s.dir, name), filepath.Join(rejected, name))
	}
}

func spoolSlug(path string) string {
	var builder strings.Builder
	for _, r := range strings.TrimPrefix(path, "/v1/ops/") {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	slug := builder.String()
	if len(slug) > 120 {
		slug = slug[:120]
	}
	return slug
}

// deliverOrSpool sends a write and, if Ops cannot take it right now, keeps it
// for a later replay. Only a 4xx (a bad report) is returned as an error.
func deliverOrSpool(ctx context.Context, client *OpsClient, spool *OpsSpool, method string, path string, body []byte, now time.Time, stderr io.Writer) ([]byte, error) {
	response, err := client.Do(ctx, method, path, body)
	if err == nil {
		return response, nil
	}
	var httpErr *OpsHTTPError
	if errors.As(err, &httpErr) && httpErr.Permanent() {
		return nil, err
	}
	file, spoolErr := spool.Add(method, path, body, now)
	if spoolErr != nil {
		return nil, fmt.Errorf("%w; and could not spool it: %w", err, spoolErr)
	}
	_, _ = fmt.Fprintf(stderr, "den-tool: warning: %v; spooled to %s for later delivery\n", err, file)
	return nil, nil
}
