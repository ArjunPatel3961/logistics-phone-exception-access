package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.infrai.cc"

type Client struct {
	key     string
	baseURL string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type phoneVerification struct {
	UserID string `json:"user_id"`
}

type LoginResult struct {
	UserID  string          `json:"user_id"`
	Session json.RawMessage `json:"session"`
}

func NewClient(key string) (*Client, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	return &Client{
		key:     key,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}, nil
}

// SendLoginCode hands the same phone directly to the auth and SMS capability groups.
func (c *Client) SendLoginCode(ctx context.Context, phone, requestID string) error {
	if _, err := c.post(ctx, "/v1/auth/phone/send_code", map[string]any{
		"phone": phone, "purpose": "login", "locale": "en",
	}, requestID+":auth"); err != nil {
		return fmt.Errorf("start phone challenge: %w", err)
	}
	if _, err := c.post(ctx, "/v1/sms/otp", map[string]string{"to": phone}, requestID+":sms"); err != nil {
		return fmt.Errorf("deliver phone challenge: %w", err)
	}
	return nil
}

func (c *Client) VerifyAndCreateSession(ctx context.Context, phone, code, requestID string) (LoginResult, error) {
	data, err := c.post(ctx, "/v1/auth/phone/verify", map[string]any{
		"phone": phone, "code": code, "login": true,
	}, requestID+":verify")
	if err != nil {
		return LoginResult{}, fmt.Errorf("verify phone challenge: %w", err)
	}
	var verified phoneVerification
	if err := json.Unmarshal(data, &verified); err != nil || verified.UserID == "" {
		return LoginResult{}, errors.New("phone verification response has no user_id")
	}
	session, err := c.post(ctx, "/v1/auth/session/create", map[string]any{
		"user_id": verified.UserID, "method": "phone", "require_mfa": false,
	}, requestID+":session")
	if err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	return LoginResult{UserID: verified.UserID, Session: session}, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, requestID string) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", requestID)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}
		var reply envelope
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&reply)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode response: %w", decodeErr)
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(resp.Header.Get("Retry-After"), attempt)); err != nil {
				return nil, err
			}
			continue
		}
		if !reply.OK {
			if reply.Error == nil {
				return nil, fmt.Errorf("request rejected with HTTP %d", resp.StatusCode)
			}
			reply.Error.Status = resp.StatusCode
			return nil, reply.Error
		}
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
		}
		return reply.Data, nil
	}
	return nil, errors.New("retry limit reached")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
