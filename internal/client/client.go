// Package client wraps the claudewatch HTTP API for the CLI.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
)

// Client talks to a running claudewatch daemon over HTTP.
type Client struct {
	base string
	http *http.Client
}

// New returns a Client targeting baseURL (e.g., "http://localhost:7777").
func New(baseURL string) *Client {
	return &Client{
		base: baseURL,
		http: &http.Client{Timeout: 3 * time.Second},
	}
}

// ErrNotFound is returned for 404 responses (e.g., no pending sessions).
type ErrNotFound struct{ Body string }

func (e ErrNotFound) Error() string { return "not found: " + e.Body }

func (c *Client) do(req *http.Request, into any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound{Body: string(body)}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, body)
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(body, into)
}

// List fetches /sessions with optional status/project filters.
func (c *Client) List(status, project string) ([]state.SessionState, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if project != "" {
		q.Set("project", project)
	}
	u := c.base + "/sessions"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	var out []state.SessionState
	return out, c.do(req, &out)
}

// Next returns the name of the oldest pending session, or ErrNotFound if none.
func (c *Client) Next() (string, error) {
	req, _ := http.NewRequest(http.MethodGet, c.base+"/pending/oldest", nil)
	var out struct {
		Name string `json:"name"`
	}
	if err := c.do(req, &out); err != nil {
		return "", err
	}
	return out.Name, nil
}

// Clear marks a session as idle.
func (c *Client) Clear(name string) error {
	req, _ := http.NewRequest(http.MethodPost, c.base+"/sessions/"+url.PathEscape(name)+"/clear", nil)
	return c.do(req, nil)
}

// Snooze suppresses a session for `minutes` (or daemon default if 0).
func (c *Client) Snooze(name string, minutes int) error {
	body := []byte(`{}`)
	if minutes > 0 {
		body = []byte(`{"minutes":` + strconv.Itoa(minutes) + `}`)
	}
	req, _ := http.NewRequest(http.MethodPost, c.base+"/sessions/"+url.PathEscape(name)+"/snooze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, nil)
}

// Summary fetches aggregate counts.
func (c *Client) Summary() (state.Summary, error) {
	req, _ := http.NewRequest(http.MethodGet, c.base+"/summary", nil)
	var out state.Summary
	return out, c.do(req, &out)
}
