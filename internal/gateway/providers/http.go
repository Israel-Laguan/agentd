package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"agentd/internal/models"
)

func postJSON(ctx context.Context, client *http.Client, url string, body any, apiKey string) ([]byte, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: send request: %v", models.ErrLLMUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", readErr)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return data, resp.StatusCode, models.ErrLLMQuotaExceeded
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return data, resp.StatusCode, fmt.Errorf("%w: status %d", models.ErrLLMUnreachable, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, resp.StatusCode, fmt.Errorf("provider rejected request: status %d", resp.StatusCode)
	}
	return data, resp.StatusCode, nil
}

func postJSONWithHeaders(ctx context.Context, client *http.Client, url string, body any, headers map[string]string) ([]byte, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: send request: %v", models.ErrLLMUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", readErr)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return data, resp.StatusCode, models.ErrLLMQuotaExceeded
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return data, resp.StatusCode, fmt.Errorf("%w: status %d", models.ErrLLMUnreachable, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, resp.StatusCode, fmt.Errorf("provider rejected request: status %d", resp.StatusCode)
	}
	return data, resp.StatusCode, nil
}
