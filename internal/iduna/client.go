// internal/iduna/client.go
// Lightweight IDUNA HTTP client — auth + apples.
// Zero external dependencies. Caches JWT in-process for the binary's lifetime.

package iduna

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client authenticates as an agent and interacts with IDUNA APIs.
type Client struct {
	BaseURL     string
	AgentName   string
	AgentSecret string

	mu       sync.Mutex
	token    string
	tokenExp time.Time
	http     *http.Client
}

// New creates a Client from config values.
func New(baseURL, agentName, agentSecret string) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		AgentName:   agentName,
		AgentSecret: agentSecret,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

// ApplePayload is the body for POST /api/v1/apples.
type ApplePayload struct {
	AppleType  string `json:"apple_type"`
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	SourceRepo string `json:"source_repo"`
	RunID      string `json:"run_id,omitempty"`
}

// Apple is one record from GET /api/v1/apples.
type Apple struct {
	ID         int64  `json:"id"`
	AgentID    string `json:"agent_id"`
	SourceRepo string `json:"source_repo"`
	RunID      string `json:"run_id"`
	AppleType  string `json:"apple_type"`
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	RecordedAt string `json:"recorded_at"`
}

// AppleListFilters controls what GET /api/v1/apples returns.
type AppleListFilters struct {
	Limit      int
	SourceRepo string
	AppleType  string
}

// Auth fetches a JWT if none exists or if it expires within 5 minutes.
func (c *Client) Auth() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.tokenExp) > 5*time.Minute {
		return nil
	}

	body, _ := json.Marshal(map[string]string{
		"agent_name":   c.AgentName,
		"agent_secret": c.AgentSecret,
	})
	resp, err := c.http.Post(c.BaseURL+"/api/v1/auth/agent",
		"application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("iduna auth: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iduna auth %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // seconds; may be absent
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return fmt.Errorf("iduna auth decode: %w", err)
	}
	c.token = tok.AccessToken
	if tok.ExpiresIn > 0 {
		c.tokenExp = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	} else {
		c.tokenExp = time.Now().Add(55 * time.Minute)
	}
	return nil
}

// ListApples queries GET /api/v1/apples with optional filters.
func (c *Client) ListApples(f AppleListFilters) ([]Apple, error) {
	if err := c.Auth(); err != nil {
		return nil, err
	}

	params := url.Values{}
	if f.Limit > 0 {
		params.Set("limit", strconv.Itoa(f.Limit))
	}
	if f.SourceRepo != "" {
		params.Set("source_repo", f.SourceRepo)
	}

	endpoint := c.BaseURL + "/api/v1/apples"
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apples list: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apples list %d: %s", resp.StatusCode, trimMsg(raw))
	}

	// Response may be {"apples":[...]} or [...] directly
	var wrapped struct {
		Apples []Apple `json:"apples"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Apples != nil {
		apples := wrapped.Apples
		if f.AppleType != "" {
			apples = filterByType(apples, f.AppleType)
		}
		return apples, nil
	}
	var apples []Apple
	if err := json.Unmarshal(raw, &apples); err != nil {
		return nil, fmt.Errorf("apples decode: %w", err)
	}
	if f.AppleType != "" {
		apples = filterByType(apples, f.AppleType)
	}
	return apples, nil
}

// GetApple fetches a single Apple by ID from GET /api/v1/apples/:id.
func (c *Client) GetApple(id int64) (*Apple, error) {
	if err := c.Auth(); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/api/v1/apples/%d", c.BaseURL, id)
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get apple %d: %w", id, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("apple #%d not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get apple %d: %d: %s", id, resp.StatusCode, trimMsg(raw))
	}

	var a Apple
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("get apple decode: %w", err)
	}
	return &a, nil
}

// PostApple creates a new Apple and returns its ID.
func (c *Client) PostApple(payload ApplePayload) (int64, error) {
	if err := c.Auth(); err != nil {
		return 0, err
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", c.BaseURL+"/api/v1/apples",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post apple: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("post apple %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("post apple decode: %w", err)
	}
	return result.ID, nil
}

func filterByType(apples []Apple, appleType string) []Apple {
	out := apples[:0]
	for _, a := range apples {
		if a.AppleType == appleType {
			out = append(out, a)
		}
	}
	return out
}

func trimMsg(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
