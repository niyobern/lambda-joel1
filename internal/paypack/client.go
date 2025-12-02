package paypack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://payments.paypack.rw"

// APIError surfaces non-successful HTTP responses from Paypack.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("paypack api error: status=%d body=%s", e.StatusCode, e.Body)
}

// ErrTransactionNotFound marks a FindTransaction miss.
var ErrTransactionNotFound = errors.New("transaction not found")

// Client is a lightweight Paypack API client tailored for Lambda usage.
type Client struct {
	httpClient *http.Client
	baseURL    string
	appID      string
	appSecret  string

	authMu      sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewClientFromEnv constructs a client using PAYPACK_* environment variables.
func NewClientFromEnv(httpClient *http.Client) (*Client, error) {
	appID := strings.TrimSpace(os.Getenv("PAYPACK_APP_ID"))
	appSecret := strings.TrimSpace(os.Getenv("PAYPACK_APP_SECRET"))
	if appID == "" || appSecret == "" {
		return nil, errors.New("PAYPACK_APP_ID and PAYPACK_APP_SECRET must be set")
	}

	baseURL := strings.TrimSpace(os.Getenv("PAYPACK_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		appID:      appID,
		appSecret:  appSecret,
	}, nil
}

// CashIn triggers a mobile-money cash-in transaction for the given number and amount.
func (c *Client) CashIn(ctx context.Context, number string, amount float64) (*Transaction, error) {
	if number == "" {
		return nil, errors.New("number is required")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be positive")
	}

	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"amount": amount,
		"number": number,
	}

	_, body, err := c.doRequest(ctx, http.MethodPost, "/api/transactions/cashin", token, payload)
	if err != nil {
		return nil, err
	}

	var txn Transaction
	if err := json.Unmarshal(body, &txn); err != nil {
		return nil, fmt.Errorf("decode cashin response: %w", err)
	}
	if txn.Ref == "" {
		return nil, errors.New("cashin response missing reference")
	}

	return &txn, nil
}

// FindTransaction fetches the transaction payload, returning ErrTransactionNotFound on misses.
func (c *Client) FindTransaction(ctx context.Context, ref string) (*Transaction, error) {
	if ref == "" {
		return nil, errors.New("ref is required")
	}

	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	status, body, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/transactions/find/%s", ref), token, nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}

	var txn Transaction
	if err := json.Unmarshal(body, &txn); err == nil && txn.Ref != "" {
		return &txn, nil
	}

	var miss TransactionNotFound
	if err := json.Unmarshal(body, &miss); err == nil && miss.Message != "" {
		return nil, ErrTransactionNotFound
	}

	if status == http.StatusNotFound {
		return nil, ErrTransactionNotFound
	}

	return nil, fmt.Errorf("unexpected transaction payload: %s", string(body))
}

func (c *Client) authorize(ctx context.Context) (*AuthResponse, error) {
	payload := map[string]string{
		"client_id":     c.appID,
		"client_secret": c.appSecret,
	}

	_, body, err := c.doRequest(ctx, http.MethodPost, "/api/auth/agents/authorize", "", payload)
	if err != nil {
		return nil, err
	}

	var auth AuthResponse
	if err := json.Unmarshal(body, &auth); err != nil {
		return nil, fmt.Errorf("decode authorize response: %w", err)
	}

	if auth.Access == "" {
		return nil, errors.New("authorize response missing access token")
	}

	return &auth, nil
}

func (c *Client) ensureAccessToken(ctx context.Context) (string, error) {
	c.authMu.Lock()
	valid := c.cachedToken != "" && time.Now().Before(c.tokenExpiry)
	token := c.cachedToken
	c.authMu.Unlock()

	if valid {
		return token, nil
	}

	auth, err := c.authorize(ctx)
	if err != nil {
		return "", err
	}

	lifetime := time.Duration(auth.Expires) * time.Second
	if lifetime <= 0 {
		lifetime = 5 * time.Minute
	}

	buffer := time.Minute
	if lifetime <= buffer {
		buffer = lifetime / 2
	}
	expiresAt := time.Now().Add(lifetime - buffer)

	c.authMu.Lock()
	c.cachedToken = auth.Access
	c.tokenExpiry = expiresAt
	c.authMu.Unlock()

	return auth.Access, nil
}

func (c *Client) doRequest(ctx context.Context, method, path, token string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return 0, nil, err
		}
		body = buf
	}

	url := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}

	if resp.StatusCode >= 400 {
		return resp.StatusCode, data, &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	return resp.StatusCode, data, nil
}
