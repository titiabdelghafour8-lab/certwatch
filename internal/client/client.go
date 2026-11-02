// Package client is a typed Go client for the certwatch HTTP API. The CLI
// uses it, and it can be imported by other Go programs.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
	"github.com/titiabdelghafour8-lab/certwatch/internal/version"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

// APIError is returned for non-2xx responses.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e model.ErrorResponse
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if json.Unmarshal(raw, &e) != nil || e.Error.Message == "" {
			e.Error.Message = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		return &APIError{Status: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func (c *Client) Signup(ctx context.Context, email string) (model.SignupResponse, error) {
	var out model.SignupResponse
	return out, c.do(ctx, http.MethodPost, "/v1/signup", model.SignupRequest{Email: email}, &out)
}

func (c *Client) Recover(ctx context.Context, email string) (model.MessageResponse, error) {
	var out model.MessageResponse
	return out, c.do(ctx, http.MethodPost, "/v1/recover", model.RecoverRequest{Email: email}, &out)
}

func (c *Client) Me(ctx context.Context) (model.MeResponse, error) {
	var out model.MeResponse
	return out, c.do(ctx, http.MethodGet, "/v1/me", nil, &out)
}

func (c *Client) ListMonitors(ctx context.Context) ([]model.Monitor, error) {
	var out model.List[model.Monitor]
	return out.Items, c.do(ctx, http.MethodGet, "/v1/monitors", nil, &out)
}

func (c *Client) CreateMonitor(ctx context.Context, req model.CreateMonitorRequest) (model.Monitor, error) {
	var out model.Monitor
	return out, c.do(ctx, http.MethodPost, "/v1/monitors", req, &out)
}

func (c *Client) GetMonitor(ctx context.Context, monitorID int64) (model.Monitor, error) {
	var out model.Monitor
	return out, c.do(ctx, http.MethodGet, "/v1/monitors/"+id(monitorID), nil, &out)
}

func (c *Client) DeleteMonitor(ctx context.Context, monitorID int64) error {
	return c.do(ctx, http.MethodDelete, "/v1/monitors/"+id(monitorID), nil, nil)
}

func (c *Client) CheckMonitor(ctx context.Context, monitorID int64) (model.Monitor, error) {
	var out model.Monitor
	return out, c.do(ctx, http.MethodPost, "/v1/monitors/"+id(monitorID)+"/check", nil, &out)
}

func (c *Client) SetPaused(ctx context.Context, monitorID int64, paused bool) (model.Monitor, error) {
	action := "/resume"
	if paused {
		action = "/pause"
	}
	var out model.Monitor
	return out, c.do(ctx, http.MethodPost, "/v1/monitors/"+id(monitorID)+action, nil, &out)
}

func (c *Client) ListChannels(ctx context.Context) ([]model.Channel, error) {
	var out model.List[model.Channel]
	return out.Items, c.do(ctx, http.MethodGet, "/v1/channels", nil, &out)
}

func (c *Client) CreateChannel(ctx context.Context, req model.CreateChannelRequest) (model.Channel, error) {
	var out model.Channel
	return out, c.do(ctx, http.MethodPost, "/v1/channels", req, &out)
}

func (c *Client) DeleteChannel(ctx context.Context, channelID int64) error {
	return c.do(ctx, http.MethodDelete, "/v1/channels/"+id(channelID), nil, nil)
}

func (c *Client) TestChannel(ctx context.Context, channelID int64) error {
	return c.do(ctx, http.MethodPost, "/v1/channels/"+id(channelID)+"/test", nil, nil)
}

func (c *Client) ListKeys(ctx context.Context) ([]model.APIKey, error) {
	var out model.List[model.APIKey]
	return out.Items, c.do(ctx, http.MethodGet, "/v1/keys", nil, &out)
}

func (c *Client) CreateKey(ctx context.Context, name string) (model.CreateKeyResponse, error) {
	var out model.CreateKeyResponse
	return out, c.do(ctx, http.MethodPost, "/v1/keys", model.CreateKeyRequest{Name: name}, &out)
}

func (c *Client) RevokeKey(ctx context.Context, keyID int64) error {
	return c.do(ctx, http.MethodDelete, "/v1/keys/"+id(keyID), nil, nil)
}

func (c *Client) Checkout(ctx context.Context, plan string) (string, error) {
	var out model.URLResponse
	return out.URL, c.do(ctx, http.MethodPost, "/v1/billing/checkout", model.CheckoutRequest{Plan: plan}, &out)
}

func (c *Client) Portal(ctx context.Context) (string, error) {
	var out model.URLResponse
	return out.URL, c.do(ctx, http.MethodPost, "/v1/billing/portal", nil, &out)
}
