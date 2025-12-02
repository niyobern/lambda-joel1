package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultCallbackTimeout = 15 * time.Second

// HTTPSCallbackSender posts subscription outcomes to an HTTPS endpoint.
type HTTPSCallbackSender struct {
	url        string
	secret     string
	httpClient *http.Client
}

// NewHTTPSCallbackSender builds an HTTPS callback client.
func NewHTTPSCallbackSender(url, secret string, client *http.Client) (*HTTPSCallbackSender, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, errors.New("callback URL is required")
	}

	if client == nil {
		client = &http.Client{Timeout: defaultCallbackTimeout}
	}

	return &HTTPSCallbackSender{
		url:        url,
		secret:     secret,
		httpClient: client,
	}, nil
}

// Send transmits the subscription response as JSON to the configured endpoint.
func (h *HTTPSCallbackSender) Send(ctx context.Context, payload SubscriptionResponse) error {
	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(payload); err != nil {
		return fmt.Errorf("encode callback payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, body)
	if err != nil {
		return fmt.Errorf("build callback request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if h.secret != "" {
		req.Header.Set("X-Callback-Secret", h.secret)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send callback request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("callback endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return nil
}
