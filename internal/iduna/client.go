// internal/iduna/client.go
// Lightweight IDUNA HTTP client — auth + apples.
// Zero external dependencies. Caches JWT in-process for the binary's lifetime.

package iduna

import (
	"bytes"
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
	ID         int64           `json:"id"`
	AgentID    string          `json:"agent_id"`
	SourceRepo string          `json:"source_repo"`
	RunID      string          `json:"run_id"`
	AppleType  string          `json:"apple_type"`
	Title      string          `json:"title"`
	Body       string          `json:"body,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	RecordedAt string          `json:"recorded_at"`
}

// AppleListFilters controls what GET /api/v1/apples returns.
type AppleListFilters struct {
	Limit      int
	SourceRepo string
	AppleType  string
}

// Auth fetches a fresh JWT on every call -- founder, real-time: "again it
// ended in an UNAUTHENTICATED error whats going on are you having to
// rotate keys or what is happening" then "ok yea we are gonna need to
// auto refresh the token like every api call or what." This used to
// reuse a cached token as long as it claimed to still have >5 minutes of
// life left, which meant a token that became invalid EARLIER than its own
// exp claim (e.g. a long-running drain spanning an IDUNA restart, which
// this session did repeatedly while other invocations were mid-run) had
// no way to self-heal short of that specific process being killed and
// restarted. IDUNA's auth endpoint is localhost and fast, so trading a
// few ms of latency per call for "can never get stuck on a stale token"
// is a clearly worthwhile trade -- tokenExp/mu are no longer meaningfully
// used but kept on Client rather than ripped out, in case a future
// caching strategy wants them back.
func (c *Client) Auth() error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

// DriveFile is one file record from GET /api/v1/drive/files.
type DriveFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MimeType    string `json:"mime_type"`
	Size        string `json:"size"`
	WebViewLink string `json:"web_view_link"`
	CreatedTime string `json:"created_time"`
}

// DriveUpload uploads a file to the Drive folder via IDUNA.
// Returns the created DriveFile on success.
func (c *Client) DriveUpload(filename, mimeType string, data []byte) (*DriveFile, error) {
	if err := c.Auth(); err != nil {
		return nil, err
	}

	boundary := "----EmilyDriveBoundary7f3a9b"
	var buf bytes.Buffer
	buf.WriteString("--" + boundary + "\r\n")
	fmt.Fprintf(&buf, "Content-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\n", filename)
	fmt.Fprintf(&buf, "Content-Type: %s\r\n\r\n", mimeType)
	buf.Write(data)
	buf.WriteString("\r\n--" + boundary + "--\r\n")

	req, _ := http.NewRequest("POST", c.BaseURL+"/api/v1/drive/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive upload: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("drive upload %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var f DriveFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("drive upload decode: %w", err)
	}
	return &f, nil
}

// DriveList fetches the list of files in the configured Drive folder.
func (c *Client) DriveList() ([]DriveFile, error) {
	if err := c.Auth(); err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", c.BaseURL+"/api/v1/drive/files", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive list: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("drive list %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var wrapped struct {
		Files []DriveFile `json:"files"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("drive list decode: %w", err)
	}
	return wrapped.Files, nil
}

// DriveGet fetches metadata for one file by ID.
func (c *Client) DriveGet(fileID string) (*DriveFile, error) {
	if err := c.Auth(); err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", c.BaseURL+"/api/v1/drive/files/"+fileID, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive get: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("drive file %q not found", fileID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("drive get %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var f DriveFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("drive get decode: %w", err)
	}
	return &f, nil
}

// PromptOVerseNode is the payload for POST /api/v1/promptoverse/nodes.
type PromptOVerseNode struct {
	Slug           string            `json:"slug"`
	Label          string            `json:"label"`
	Subject        string            `json:"subject"`
	Kind           string            `json:"kind"`
	EZPrompt       string            `json:"ez_prompt"`
	ExpandedPrompt string            `json:"expanded_prompt"`
	ImageBase64    string            `json:"image_base64"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// ErrPromptOVerseNodeExists means the slug (subject+style combination)
// already exists -- a real, expected outcome for a stale/duplicate queue
// entry, not a genuine failure. Callers (drainQueue) should skip and
// continue on this specific error rather than stopping the whole drain.
var ErrPromptOVerseNodeExists = errors.New("promptoverse: node already exists")

// PostPromptOVerseNode publishes one node to the Prompt-o-verse gallery.
// Requires promptoverse.write (EMILY-PRIME has it as of 2026-08-17).
func (c *Client) PostPromptOVerseNode(n PromptOVerseNode) (url string, err error) {
	if err := c.Auth(); err != nil {
		return "", err
	}

	body, _ := json.Marshal(n)
	req, _ := http.NewRequest("POST", c.BaseURL+"/api/v1/promptoverse/nodes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("post promptoverse node: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == http.StatusConflict {
		return "", ErrPromptOVerseNodeExists
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("post promptoverse node %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("post promptoverse node decode: %w", err)
	}
	return result.URL, nil
}

// PromptOVerseNodeSummary is the (subject, label) pair for one existing
// node -- enough for `emily promptoverse add` to dedupe against and weigh
// style variety, without pulling full prompts/images.
type PromptOVerseNodeSummary struct {
	Slug    string `json:"slug"`
	Subject string `json:"subject"`
	Label   string `json:"label"`
}

// ListPromptOVerseNodes returns the (subject, label) pair for every
// published node -- used by `emily promptoverse add` to (a) skip any
// subject+style combination that already exists (founder: "we should not
// prompt for the tobacco card with that exact same prompt if we already
// have one") and (b) prefer globally under-used styles so runs don't keep
// favoring whichever styles happen to sit first in the registry.
func (c *Client) ListPromptOVerseNodes() ([]PromptOVerseNodeSummary, error) {
	if err := c.Auth(); err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", c.BaseURL+"/api/v1/promptoverse/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list promptoverse nodes: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list promptoverse nodes %d: %s", resp.StatusCode, trimMsg(raw))
	}

	var wrapped struct {
		Nodes []PromptOVerseNodeSummary `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("list promptoverse nodes decode: %w", err)
	}
	return wrapped.Nodes, nil
}

// PromptOVerseNodeDetail is the full record for one node, including the
// prompts -- used by `emily promptoverse regenerate` to look up what it's
// creating a variant of.
type PromptOVerseNodeDetail struct {
	Slug           string            `json:"slug"`
	Label          string            `json:"label"`
	Subject        string            `json:"subject"`
	Kind           string            `json:"kind"`
	EZPrompt       string            `json:"ez_prompt"`
	ExpandedPrompt string            `json:"expanded_prompt"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// ErrPromptOVerseNodeNotFound means the slug doesn't exist.
var ErrPromptOVerseNodeNotFound = errors.New("promptoverse: node not found")

// GetPromptOVerseNodeBySlug fetches one node's full record -- public
// endpoint, no auth required.
func (c *Client) GetPromptOVerseNodeBySlug(slug string) (PromptOVerseNodeDetail, error) {
	req, _ := http.NewRequest("GET", c.BaseURL+"/api/v1/promptoverse/nodes/"+slug, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return PromptOVerseNodeDetail{}, fmt.Errorf("get promptoverse node: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == http.StatusNotFound {
		return PromptOVerseNodeDetail{}, ErrPromptOVerseNodeNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return PromptOVerseNodeDetail{}, fmt.Errorf("get promptoverse node %d: %s", resp.StatusCode, trimMsg(raw))
	}
	var n PromptOVerseNodeDetail
	if err := json.Unmarshal(raw, &n); err != nil {
		return PromptOVerseNodeDetail{}, fmt.Errorf("get promptoverse node decode: %w", err)
	}
	return n, nil
}

// PromptOVerseVariant is a new, additional generated image for an
// already-published node -- "regenerate with variation" (S176-30).
// Additive: the original node/image is never touched.
type PromptOVerseVariant struct {
	EZPrompt       string `json:"ez_prompt"`
	ExpandedPrompt string `json:"expanded_prompt"`
	ImageBase64    string `json:"image_base64"`
	Note           string `json:"note"`
}

// AddPromptOVerseVariant attaches a variant to an existing node. Requires
// promptoverse.write.
func (c *Client) AddPromptOVerseVariant(slug string, v PromptOVerseVariant) (url string, err error) {
	if err := c.Auth(); err != nil {
		return "", err
	}
	body, _ := json.Marshal(v)
	req, _ := http.NewRequest("POST", c.BaseURL+"/api/v1/promptoverse/nodes/"+slug+"/variants", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("add promptoverse variant: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrPromptOVerseNodeNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("add promptoverse variant %d: %s", resp.StatusCode, trimMsg(raw))
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("add promptoverse variant decode: %w", err)
	}
	return result.URL, nil
}

// MergeNodeTags overlays extra key/values onto an existing node's Tags
// without touching its image/prompt data -- used to backfill metadata
// onto nodes published before some later context existed (e.g. a
// subject-level annotation added after the fact; see `emily promptoverse
// backfill-annotation`). Requires promptoverse.write.
func (c *Client) MergeNodeTags(slug string, tags map[string]string) error {
	if err := c.Auth(); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"tags": tags})
	req, _ := http.NewRequest("PATCH", c.BaseURL+"/api/v1/promptoverse/nodes/"+slug+"/tags", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("merge promptoverse node tags: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrPromptOVerseNodeNotFound
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("merge promptoverse node tags %d: %s", resp.StatusCode, trimMsg(raw))
	}
	return nil
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
