package iduna

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CoderDo calls a /api/v1/coder/... endpoint as this agent (fresh JWT per call, see Auth).
// A 204 returns (nil, 204, nil): the work queue is empty.
func (c *Client) CoderDo(method, path string, in any) ([]byte, int, error) {
	if err := c.Auth(); err != nil {
		return nil, 0, err
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+"/api/v1/coder"+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return raw, resp.StatusCode, fmt.Errorf("coder %s %s: %d %s", method, path, resp.StatusCode, trimMsg(raw))
	}
	return raw, resp.StatusCode, nil
}
