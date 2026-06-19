package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"den-services/shared/identity"
)

type RuntimeClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

func NewRuntimeClient(baseURL string, serviceToken string, timeout time.Duration) *RuntimeClient {
	return &RuntimeClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		serviceToken: strings.TrimSpace(serviceToken),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *RuntimeClient) IsAlive(ctx context.Context, instanceID identity.AgentInstanceID) (bool, error) {
	if c.serviceToken == "" {
		return false, ErrMissingRuntimeAuth
	}
	requestURL := c.baseURL + "/v1/runtime/instances/" + url.PathEscape(instanceID.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return false, fmt.Errorf("creating runtime liveness request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("checking runtime liveness: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("runtime liveness status: %s; check DEN_DELIVERY_RUNTIME_SERVICE_TOKEN", resp.Status)
	}
	var decoded struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return false, fmt.Errorf("decoding runtime liveness: %w", err)
	}
	switch decoded.State {
	case "active", "idle", "busy":
		return true, nil
	default:
		return false, nil
	}
}
