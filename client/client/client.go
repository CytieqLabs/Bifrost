// Package client provides the typed Bifrost Protocol v1 HTTP client used by
// Isha and other trusted local or remote consumers.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

const maxResponseBody = 4 << 20

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type idempotencyKey struct{}

// WithIdempotencyKey binds one stable key to a logical mutation. Reuse the
// returned context when retrying the same request; a different operation must
// use a different key.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKey{}, key)
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("bifrost: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("bifrost: HTTP %d: %s", e.StatusCode, e.Message)
}

func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("Bifrost endpoint: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("Bifrost endpoint must be an absolute HTTP or HTTPS origin")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("Bifrost endpoint must not contain credentials, a path, query, or fragment")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: httpClient}, nil
}

func (c *Client) Health(ctx context.Context) error {
	var health struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, bifrostv1.HealthPath, nil, &health); err != nil {
		return err
	}
	if health.Status != "ok" {
		return fmt.Errorf("bifrost: unhealthy status %q", health.Status)
	}
	return nil
}

func (c *Client) Status(ctx context.Context) (*bifrostv1.Status, error) {
	var result bifrostv1.Status
	return &result, c.do(ctx, http.MethodGet, bifrostv1.StatusPath, nil, &result)
}

func (c *Client) Runs(ctx context.Context) ([]bifrostv1.Run, error) {
	var result []bifrostv1.Run
	if err := c.do(ctx, http.MethodGet, bifrostv1.RunsPath, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) StartRun(ctx context.Context, input bifrostv1.StartRunRequest) (*bifrostv1.Run, error) {
	var result bifrostv1.Run
	return &result, c.do(ctx, http.MethodPost, bifrostv1.RunsPath, input, &result)
}

func (c *Client) Run(ctx context.Context, runID string) (*bifrostv1.RunDetails, error) {
	var result bifrostv1.RunDetails
	return &result, c.do(ctx, http.MethodGet, bifrostv1.RunPath(runID), nil, &result)
}

func (c *Client) CreateCheckpoint(ctx context.Context, runID string, input bifrostv1.CreateCheckpointRequest) (*bifrostv1.Checkpoint, error) {
	var result bifrostv1.Checkpoint
	return &result, c.do(ctx, http.MethodPost, bifrostv1.RunCheckpointsPath(runID), input, &result)
}

func (c *Client) AddEvidence(ctx context.Context, runID string, input bifrostv1.AddEvidenceRequest) (*bifrostv1.Evidence, error) {
	var result bifrostv1.Evidence
	return &result, c.do(ctx, http.MethodPost, bifrostv1.RunEvidencePath(runID), input, &result)
}

func (c *Client) FinishRun(ctx context.Context, runID string, input bifrostv1.FinishRunRequest) (*bifrostv1.Run, error) {
	var result bifrostv1.Run
	return &result, c.do(ctx, http.MethodPost, bifrostv1.RunFinishPath(runID), input, &result)
}

func (c *Client) CheckPromotion(ctx context.Context, runID string, input bifrostv1.CheckPromotionRequest) (*bifrostv1.Promotion, error) {
	var result bifrostv1.Promotion
	return &result, c.do(ctx, http.MethodPost, bifrostv1.RunPromotionsPath(runID), input, &result)
}

func (c *Client) ApplyPromotion(ctx context.Context, promotionID string) (*bifrostv1.Promotion, error) {
	var result bifrostv1.Promotion
	return &result, c.do(ctx, http.MethodPost, bifrostv1.PromotionApplyPath(promotionID), nil, &result)
}

// StreamEvents replays events after after and then follows the live SSE stream
// until ctx is cancelled. The callback is invoked in sequence order.
func (c *Client) StreamEvents(ctx context.Context, after uint64, onEvent func(bifrostv1.Event) error) error {
	query := "?after=" + strconv.FormatUint(after, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+bifrostv1.EventsPath+query, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		request.Header.Set(bifrostv1.HeaderAuthorization, bifrostv1.BearerPrefix+c.token)
	}
	httpClient := *c.http
	httpClient.Timeout = 0 // stream lifetime is controlled by ctx
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
		var envelope bifrostv1.ErrorEnvelope
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Message != "" {
			return &HTTPError{StatusCode: response.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message}
		}
		return &HTTPError{StatusCode: response.StatusCode, Message: response.Status}
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), maxResponseBody)
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() == 0 {
				continue
			}
			var event bifrostv1.Event
			if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
				return fmt.Errorf("decode Bifrost event: %w", err)
			}
			if onEvent != nil {
				if err := onEvent(event); err != nil {
					return err
				}
			}
			data.Reset()
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimSpace(value))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", bifrostv1.MediaTypeJSON)
	if input != nil {
		request.Header.Set("Content-Type", bifrostv1.MediaTypeJSON)
	}
	if c.token != "" {
		request.Header.Set(bifrostv1.HeaderAuthorization, bifrostv1.BearerPrefix+c.token)
	}
	if key, ok := ctx.Value(idempotencyKey{}).(string); ok && key != "" && method == http.MethodPost {
		request.Header.Set(bifrostv1.HeaderIdempotencyKey, key)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	reader := io.LimitReader(response.Body, maxResponseBody)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope bifrostv1.ErrorEnvelope
		if err := json.NewDecoder(reader).Decode(&envelope); err != nil {
			return &HTTPError{StatusCode: response.StatusCode, Message: response.Status}
		}
		return &HTTPError{StatusCode: response.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(reader).Decode(output); err != nil {
		return fmt.Errorf("decode Bifrost response: %w", err)
	}
	return nil
}
