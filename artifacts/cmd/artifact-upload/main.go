package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	baseURLEnv = "DEN_ARTIFACTS_BASE_URL"
	tokenEnv   = "DEN_ARTIFACTS_SERVICE_TOKEN"
)

type uploadConfig struct {
	filePath      string
	baseURL       string
	token         string
	projectID     string
	taskID        int64
	reviewRoundID int64
	findingID     int64
	ownerKind     string
	ownerID       string
	logicalName   string
	mimeType      string
	createdBy     string
	sensitive     bool
	temporary     bool
	timeout       time.Duration
}

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := upload(context.Background(), cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags(args []string, getenv func(string) string) (uploadConfig, error) {
	var cfg uploadConfig
	flags := flag.NewFlagSet("artifact-upload", flag.ContinueOnError)
	flags.StringVar(&cfg.filePath, "file", "", "local image file to upload")
	flags.StringVar(&cfg.baseURL, "base-url", strings.TrimRight(getenv(baseURLEnv), "/"), "artifacts service base URL")
	flags.StringVar(&cfg.token, "token", getenv(tokenEnv), "artifacts service bearer token")
	flags.StringVar(&cfg.projectID, "project-id", "", "Den project id")
	flags.Int64Var(&cfg.taskID, "task-id", 0, "Den task id")
	flags.Int64Var(&cfg.reviewRoundID, "review-round-id", 0, "review round id")
	flags.Int64Var(&cfg.findingID, "finding-id", 0, "review finding id")
	flags.StringVar(&cfg.ownerKind, "owner-kind", "", "artifact owner kind")
	flags.StringVar(&cfg.ownerID, "owner-id", "", "artifact owner id")
	flags.StringVar(&cfg.logicalName, "logical-name", "", "logical artifact name")
	flags.StringVar(&cfg.mimeType, "mime-type", "", "MIME type override")
	flags.StringVar(&cfg.createdBy, "created-by", "", "creator identity")
	flags.BoolVar(&cfg.sensitive, "sensitive", false, "mark artifact sensitive")
	flags.BoolVar(&cfg.temporary, "temporary", false, "apply temporary retention")
	flags.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "upload request timeout")
	if err := flags.Parse(args); err != nil {
		return uploadConfig{}, err
	}
	cfg.baseURL = strings.TrimRight(cfg.baseURL, "/")
	if err := cfg.validate(); err != nil {
		return uploadConfig{}, err
	}
	return cfg, nil
}

func (c uploadConfig) validate() error {
	if strings.TrimSpace(c.filePath) == "" {
		return errors.New("-file is required")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return fmt.Errorf("-base-url or %s is required", baseURLEnv)
	}
	if strings.TrimSpace(c.token) == "" {
		return fmt.Errorf("-token or %s is required", tokenEnv)
	}
	if c.timeout <= 0 {
		return errors.New("-timeout must be positive")
	}
	return nil
}

func upload(ctx context.Context, cfg uploadConfig, output io.Writer) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := addFields(writer, cfg); err != nil {
		return err
	}
	if err := addFile(writer, cfg); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing multipart body: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, cfg.baseURL+"/v1/artifacts", body)
	if err != nil {
		return fmt.Errorf("building upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("uploading artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("artifact upload returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var decoded json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("decoding upload response: %w", err)
	}
	if !json.Valid(decoded) {
		return errors.New("artifact upload response was not valid json")
	}
	if _, err := output.Write(append(decoded, '\n')); err != nil {
		return fmt.Errorf("writing upload response: %w", err)
	}
	return nil
}

func addFields(writer *multipart.Writer, cfg uploadConfig) error {
	fields := map[string]string{
		"project_id": cfg.projectID,
		"owner_kind": cfg.ownerKind,
		"owner_id":   cfg.ownerID,
		"mime_type":  cfg.mimeType,
		"created_by": cfg.createdBy,
	}
	logicalName := cfg.logicalName
	if strings.TrimSpace(logicalName) == "" {
		logicalName = filepath.Base(cfg.filePath)
	}
	fields["logical_name"] = logicalName
	if cfg.taskID > 0 {
		fields["task_id"] = strconv.FormatInt(cfg.taskID, 10)
	}
	if cfg.reviewRoundID > 0 {
		fields["review_round_id"] = strconv.FormatInt(cfg.reviewRoundID, 10)
	}
	if cfg.findingID > 0 {
		fields["finding_id"] = strconv.FormatInt(cfg.findingID, 10)
	}
	if cfg.sensitive {
		fields["sensitive"] = "true"
	}
	if cfg.temporary {
		fields["temporary"] = "true"
	}
	for key, value := range fields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("writing multipart field %s: %w", key, err)
		}
	}
	return nil
}

func addFile(writer *multipart.Writer, cfg uploadConfig) error {
	file, err := os.Open(cfg.filePath)
	if err != nil {
		return fmt.Errorf("opening image file: %w", err)
	}
	defer file.Close()
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filepath.Base(cfg.filePath))))
	if cfg.mimeType != "" {
		header.Set("Content-Type", cfg.mimeType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("creating multipart file part: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("copying image file: %w", err)
	}
	return nil
}

func escapeQuotes(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}
