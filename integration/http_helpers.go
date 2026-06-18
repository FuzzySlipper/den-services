package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"den-services/shared/api"
)

func NewAuthenticatedRequest(method string, url string, body []byte, serviceToken string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("creating authenticated request: %w", err)
	}
	if serviceToken != "" {
		request.Header.Set("Authorization", "Bearer "+serviceToken)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

func DecodeErrorEnvelope(response *http.Response) (api.ErrorEnvelope, error) {
	defer response.Body.Close()

	var envelope api.ErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return api.ErrorEnvelope{}, fmt.Errorf("decoding error envelope: %w", err)
	}
	return envelope, nil
}
