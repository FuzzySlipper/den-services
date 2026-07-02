package broker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type HealthEvidence struct {
	URL            string `json:"url"`
	StatusCode     int    `json:"status_code"`
	ReadyTextFound bool   `json:"ready_text_found"`
	HeaderMatched  bool   `json:"header_matched"`
	Matched        bool   `json:"matched"`
	Error          string `json:"error,omitempty"`
}

func checkHealth(ctx context.Context, client *http.Client, manifest *Manifest, port int) HealthEvidence {
	url := manifest.HealthURL(port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return HealthEvidence{URL: url, Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return HealthEvidence{URL: url, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	readyTextFound := true
	if strings.TrimSpace(manifest.Serve.ReadyText) != "" {
		readyTextFound = strings.Contains(string(body), manifest.Serve.ReadyText)
	}
	headerMatched := true
	if strings.TrimSpace(manifest.Serve.IdentityHeader) != "" {
		headerMatched = resp.Header.Get(manifest.Serve.IdentityHeader) == manifest.Project
	}
	matched := resp.StatusCode >= 200 && resp.StatusCode < 300 && readyTextFound && headerMatched
	return HealthEvidence{
		URL:            url,
		StatusCode:     resp.StatusCode,
		ReadyTextFound: readyTextFound,
		HeaderMatched:  headerMatched,
		Matched:        matched,
	}
}

func waitForHealth(ctx context.Context, client *http.Client, manifest *Manifest, port int, timeout time.Duration, interval time.Duration) (HealthEvidence, error) {
	deadline := time.Now().Add(timeout)
	var last HealthEvidence
	for {
		last = checkHealth(ctx, client, manifest, port)
		if last.Matched {
			return last, nil
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("dev server did not become healthy at %s", last.URL)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func portInUse(host string, port int, timeout time.Duration) bool {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func findFreePort(host string, ports PortRange, blocked map[int]bool) (int, error) {
	for port := ports.Start; port <= ports.End; port++ {
		if blocked[port] {
			continue
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, fmt.Errorf("%w: %d-%d", ErrNoPortAvailable, ports.Start, ports.End)
}
