// Package cloudflare provides bounded, context-aware access to the Cloudflare API.
package cloudflare

import (
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
	"sync"
	"time"
)

type Credentials struct {
	Token, Email, Key string
}

type Zone struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Paused    bool      `json:"paused"`
	Type      string    `json:"type"`
	CreatedOn time.Time `json:"created_on"`
}

type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("Cloudflare HTTP %d: %s", e.Status, e.Message) }

type Client struct {
	HTTP    *http.Client
	BaseURL string
	mu      sync.Mutex
	// Backoff is shared across requests so concurrent monitors respect rate limits.
	nextRequest time.Time
}

func NewClient() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		BaseURL: "https://api.cloudflare.com/client/v4",
	}
}

type envelope struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Info struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c *Client) request(ctx context.Context, auth Credentials, method, path string, payload, result any) (int, error) {
	if auth.Token == "" && (auth.Email == "" || auth.Key == "") {
		return 0, errors.New("Cloudflare credentials are missing")
	}
	var data []byte
	var err error
	if payload != nil {
		data, err = json.Marshal(payload)
		if err != nil {
			return 0, err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.wait(ctx); err != nil {
			return 0, err
		}
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(data))
		if err != nil {
			return 0, err
		}
		if auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		} else {
			req.Header.Set("X-Auth-Email", auth.Email)
			req.Header.Set("X-Auth-Key", auth.Key)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return 0, fmt.Errorf("Cloudflare request failed: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		resp.Body.Close()
		if readErr != nil {
			return 0, readErr
		}
		if len(body) > 2<<20 {
			return 0, errors.New("Cloudflare response exceeds 2 MiB")
		}
		var response envelope
		decodeErr := json.Unmarshal(body, &response)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && decodeErr == nil && response.Success {
			if result != nil {
				if len(response.Result) == 0 || bytes.Equal(response.Result, []byte("null")) {
					return 0, errors.New("Cloudflare returned an empty result")
				}
				if err := json.Unmarshal(response.Result, result); err != nil {
					return 0, fmt.Errorf("invalid Cloudflare result: %w", err)
				}
			}
			return response.Info.TotalPages, nil
		}
		message := http.StatusText(resp.StatusCode)
		if decodeErr != nil {
			message = "invalid JSON response"
		} else if len(response.Errors) > 0 {
			message = response.Errors[0].Message
		} else if !response.Success {
			message = "API reported success=false"
		}
		apiErr := &APIError{Status: resp.StatusCode, Message: message}
		if len(response.Errors) > 0 {
			apiErr.Code = response.Errors[0].Code
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if retryable {
			delay := time.Duration(1<<attempt) * time.Second
			if retry := retryDelay(resp.Header.Get("Retry-After")); retry > delay {
				delay = retry
			}
			c.mu.Lock()
			c.nextRequest = maxTime(c.nextRequest, time.Now().Add(delay))
			c.mu.Unlock()
		}
		// A POST may already have created a record; never retry it automatically.
		if !retryable || attempt == 2 || method == http.MethodPost {
			return 0, apiErr
		}
	}
	panic("unreachable")
}

func retryDelay(value string) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 3600)) * time.Second
	}
	if until, err := http.ParseTime(value); err == nil {
		return time.Until(until)
	}
	return 0
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (c *Client) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		delay := time.Until(c.nextRequest)
		c.mu.Unlock()
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func list[T any](ctx context.Context, c *Client, auth Credentials, path string, query url.Values) ([]T, error) {
	items := make([]T, 0)
	for page := 1; page <= 10000; page++ {
		query.Set("page", strconv.Itoa(page))
		var batch []T
		pages, err := c.request(ctx, auth, http.MethodGet, path+"?"+query.Encode(), nil, &batch)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if page >= pages || len(batch) == 0 {
			return items, nil
		}
	}
	return nil, errors.New("Cloudflare pagination exceeds limit")
}

func (c *Client) Zones(ctx context.Context, auth Credentials) ([]Zone, error) {
	return list[Zone](ctx, c, auth, "/zones", url.Values{"per_page": {"50"}})
}

func (c *Client) Records(ctx context.Context, auth Credentials, zone string, filter url.Values) ([]Record, error) {
	if zone == "" {
		return nil, errors.New("zone ID is required")
	}
	if filter == nil {
		filter = url.Values{}
	}
	filter.Set("per_page", "100")
	return list[Record](ctx, c, auth, recordPath(zone, ""), filter)
}

func recordPath(zone, id string) string {
	path := "/zones/" + url.PathEscape(zone) + "/dns_records"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	return path
}

func (c *Client) Record(ctx context.Context, auth Credentials, zone, id string) (*Record, error) {
	if zone == "" || id == "" {
		return nil, errors.New("zone and record IDs are required")
	}
	var record Record
	_, err := c.request(ctx, auth, http.MethodGet, recordPath(zone, id), nil, &record)
	return &record, err
}

func (c *Client) SaveRecord(ctx context.Context, auth Credentials, zone, id string, payload any) (*Record, error) {
	if zone == "" {
		return nil, errors.New("zone ID is required")
	}
	method := http.MethodPatch
	if id == "" {
		method = http.MethodPost
	}
	var record Record
	_, err := c.request(ctx, auth, method, recordPath(zone, id), payload, &record)
	return &record, err
}

func (c *Client) DeleteRecord(ctx context.Context, auth Credentials, zone, id string) error {
	if zone == "" || id == "" {
		return errors.New("zone and record IDs are required")
	}
	_, err := c.request(ctx, auth, http.MethodDelete, recordPath(zone, id), nil, nil)
	return err
}
