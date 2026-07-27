package webedge

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var immutableAssetName = regexp.MustCompile(`[.-][A-Za-z0-9_-]{8,}\.`) //nolint:gochecknoglobals

type staticHandler struct {
	cfg StaticConfig
}

func newStaticHandler(cfg StaticConfig) (*staticHandler, error) {
	handler := &staticHandler{cfg: cfg}
	if err := handler.validateRelease(); err != nil {
		return nil, err
	}
	return handler, nil
}

func (h *staticHandler) validateRelease() error {
	if err := validateRegularFile(filepath.Join(h.cfg.Root, "index.html")); err != nil {
		return fmt.Errorf("validating index: %w", err)
	}
	for name, path := range map[string]string{
		"runtime config": h.cfg.RuntimeConfigPath,
		"build sentinel": h.cfg.BuildSentinelPath,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		var object map[string]any
		if err := json.Unmarshal(data, &object); err != nil {
			return fmt.Errorf("parsing %s as a JSON object: %w", name, err)
		}
		if object == nil {
			return fmt.Errorf("%s must be a JSON object", name)
		}
	}
	return nil
}

func validateRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	return nil
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if hasParentTraversal(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	cleanedPath := path.Clean("/" + r.URL.Path)
	requestPath := ""
	var err error
	if cleanedPath == "/" {
		requestPath = "index.html"
	} else {
		requestPath, err = filepath.Localize(strings.TrimPrefix(cleanedPath, "/"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	target := filepath.Join(h.cfg.Root, requestPath)
	if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
		h.serveFile(w, r, target, info)
		return
	}
	indexPath := filepath.Join(h.cfg.Root, "index.html")
	info, statErr := os.Stat(indexPath)
	if statErr != nil || !info.Mode().IsRegular() {
		http.Error(w, "index.html not found", http.StatusServiceUnavailable)
		return
	}
	h.serveFile(w, r, indexPath, info)
}

func hasParentTraversal(requestPath string) bool {
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func (h *staticHandler) serveFile(w http.ResponseWriter, r *http.Request, path string, info os.FileInfo) {
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusServiceUnavailable)
		return
	}
	defer file.Close()

	if strings.HasSuffix(path, ".html") || path == h.cfg.RuntimeConfigPath || path == h.cfg.BuildSentinelPath {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	} else if immutableAssetName.MatchString(filepath.Base(path)) {
		seconds := int64(h.cfg.ImmutableAssetMaxAge.Seconds())
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", seconds))
	} else {
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	}
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func writeJSONError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
