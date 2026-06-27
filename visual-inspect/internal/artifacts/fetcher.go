package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"den-services/visual-inspect/internal/config"
	"den-services/visual-inspect/internal/schema"
)

const (
	MimePNG  = "image/png"
	MimeJPEG = "image/jpeg"
)

type Image struct {
	ScreenshotID string
	RefScheme    string
	MimeType     string
	Bytes        []byte
	ByteCount    int64
	Width        int
	Height       int
	SHA256       string
	Sensitive    bool
}

type Fetcher struct {
	cfg        config.ArtifactConfig
	httpClient *http.Client
}

type artifactMetadata struct {
	ArtifactID string `json:"artifact_id"`
	MimeType   string `json:"mime_type"`
	Sensitive  bool   `json:"sensitive"`
}

func NewFetcher(cfg config.ArtifactConfig, httpClient *http.Client) *Fetcher {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Fetcher{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (f *Fetcher) Fetch(ctx context.Context, ref schema.ScreenshotRef) (Image, error) {
	parsed, err := url.Parse(ref.Ref)
	if err != nil {
		return Image{}, schema.BadRequest("screenshots.%s.ref is invalid: %v", ref.ID, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if !f.schemeAllowed(scheme) {
		return Image{}, schema.BadRequest("screenshots.%s.ref scheme is not allowed: %s", ref.ID, scheme)
	}
	metadata := artifactMetadata{}
	data, err := f.readBytes(ctx, parsed, scheme, ref.Ref)
	if err != nil {
		return Image{}, err
	}
	if scheme == "den-artifact" {
		metadata, data, err = f.readDenArtifact(ctx, parsed, ref.Ref)
		if err != nil {
			return Image{}, err
		}
	}
	if int64(len(data)) > f.cfg.MaxBytesPerImage {
		return Image{}, schema.PayloadTooLarge("screenshots.%s exceeds max_bytes_per_image", ref.ID)
	}
	width, height, err := decodeDimensions(data, ref.MimeType)
	if err != nil {
		return Image{}, schema.BadRequest("screenshots.%s could not be decoded as %s: %v", ref.ID, ref.MimeType, err)
	}
	if int64(width)*int64(height) > f.cfg.MaxPixelsPerImage {
		return Image{}, schema.PayloadTooLarge("screenshots.%s exceeds max_pixels_per_image", ref.ID)
	}
	hash := sha256.Sum256(data)
	return Image{
		ScreenshotID: ref.ID,
		RefScheme:    scheme,
		MimeType:     ref.MimeType,
		Bytes:        data,
		ByteCount:    int64(len(data)),
		Width:        width,
		Height:       height,
		SHA256:       hex.EncodeToString(hash[:]),
		Sensitive:    ref.Sensitive || metadata.Sensitive,
	}, nil
}

func (f *Fetcher) readBytes(ctx context.Context, parsed *url.URL, scheme string, rawRef string) ([]byte, error) {
	switch scheme {
	case "file":
		return f.readFile(parsed)
	case "http", "https":
		return f.readHTTP(ctx, parsed)
	case "den-artifact":
		return nil, nil
	default:
		return nil, schema.UnsupportedArtifact("unsupported ref scheme: %s", rawRef)
	}
}

func (f *Fetcher) readFile(parsed *url.URL) ([]byte, error) {
	path := parsed.Path
	if path == "" {
		return nil, schema.BadRequest("file ref path is required")
	}
	if !f.pathAllowed(path) {
		return nil, schema.BadRequest("file ref is outside allowed_file_roots")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening screenshot file: %w", err)
	}
	defer file.Close()
	return readLimited(file, f.cfg.MaxBytesPerImage)
}

func (f *Fetcher) readHTTP(ctx context.Context, parsed *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, schema.BadRequest("http screenshot ref is invalid: %v", err)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching screenshot ref: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, schema.BadRequest("http screenshot ref returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > f.cfg.MaxBytesPerImage {
		return nil, schema.PayloadTooLarge("http screenshot ref exceeds max_bytes_per_image")
	}
	return readLimited(resp.Body, f.cfg.MaxBytesPerImage)
}

func (f *Fetcher) readDenArtifact(ctx context.Context, parsed *url.URL, rawRef string) (artifactMetadata, []byte, error) {
	metadata, err := f.fetchArtifactMetadata(ctx, parsed, rawRef)
	if err != nil {
		return artifactMetadata{}, nil, err
	}
	data, err := f.fetchArtifactContent(ctx, metadata.ArtifactID)
	if err != nil {
		return artifactMetadata{}, nil, err
	}
	return metadata, data, nil
}

func (f *Fetcher) fetchArtifactMetadata(ctx context.Context, parsed *url.URL, rawRef string) (artifactMetadata, error) {
	endpoint, err := f.artifactMetadataURL(parsed, rawRef)
	if err != nil {
		return artifactMetadata{}, err
	}
	var metadata artifactMetadata
	if err := f.getArtifactJSON(ctx, endpoint, &metadata); err != nil {
		return artifactMetadata{}, err
	}
	if metadata.ArtifactID == "" {
		return artifactMetadata{}, schema.ArtifactUnavailable("artifact metadata response missing artifact_id")
	}
	return metadata, nil
}

func (f *Fetcher) artifactMetadataURL(parsed *url.URL, rawRef string) (string, error) {
	if f.cfg.ServiceBaseURL == "" {
		return "", schema.UnsupportedArtifact("artifact service base url is not configured")
	}
	if strings.HasPrefix(parsed.Host, "art_") && parsed.Path == "" {
		return f.cfg.ServiceBaseURL + "/v1/artifacts/" + url.PathEscape(parsed.Host) + "/metadata", nil
	}
	values := url.Values{}
	values.Set("ref", rawRef)
	return f.cfg.ServiceBaseURL + "/v1/artifacts/resolve?" + values.Encode(), nil
}

func (f *Fetcher) fetchArtifactContent(ctx context.Context, artifactID string) ([]byte, error) {
	endpoint := f.cfg.ServiceBaseURL + "/v1/artifacts/" + url.PathEscape(artifactID) + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, schema.UnsupportedArtifact("artifact content url is invalid: %v", err)
	}
	f.authorizeArtifactRequest(req)
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, schema.ArtifactUnavailable("fetching artifact content: %v", err)
	}
	defer resp.Body.Close()
	if err := artifactStatusError(resp.StatusCode, "artifact content"); err != nil {
		return nil, err
	}
	if resp.ContentLength > f.cfg.MaxBytesPerImage {
		return nil, schema.PayloadTooLarge("artifact content exceeds max_bytes_per_image")
	}
	return readLimited(resp.Body, f.cfg.MaxBytesPerImage)
}

func (f *Fetcher) getArtifactJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return schema.UnsupportedArtifact("artifact metadata url is invalid: %v", err)
	}
	f.authorizeArtifactRequest(req)
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return schema.ArtifactUnavailable("fetching artifact metadata: %v", err)
	}
	defer resp.Body.Close()
	if err := artifactStatusError(resp.StatusCode, "artifact metadata"); err != nil {
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return schema.ArtifactUnavailable("decoding artifact metadata: %v", err)
	}
	return nil
}

func (f *Fetcher) authorizeArtifactRequest(req *http.Request) {
	if f.cfg.ServiceToken != "" {
		req.Header.Set("Authorization", "Bearer "+f.cfg.ServiceToken)
	}
}

func artifactStatusError(status int, label string) error {
	switch {
	case status >= 200 && status <= 299:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return schema.ArtifactUnauthorized("%s request was not authorized", label)
	case status == http.StatusNotFound:
		return schema.ArtifactNotFound("%s was not found", label)
	default:
		return schema.ArtifactUnavailable("%s request returned status %d", label, status)
	}
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading screenshot bytes: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, schema.PayloadTooLarge("screenshot exceeds max_bytes_per_image")
	}
	return data, nil
}

func decodeDimensions(data []byte, mimeType string) (int, int, error) {
	switch mimeType {
	case MimePNG, MimeJPEG:
	default:
		return 0, 0, fmt.Errorf("unsupported mime type %s", mimeType)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func (f *Fetcher) schemeAllowed(scheme string) bool {
	for _, allowed := range f.cfg.AllowedSchemes {
		if strings.EqualFold(allowed, scheme) {
			return true
		}
	}
	return false
}

func (f *Fetcher) pathAllowed(path string) bool {
	cleanPath := filepath.Clean(path)
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return false
	}
	for _, root := range f.cfg.AllowedFileRoots {
		cleanRoot := filepath.Clean(root)
		resolvedRoot, err := filepath.EvalSymlinks(cleanRoot)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolvedPath)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
			return true
		}
	}
	return false
}
