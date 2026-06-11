// internal/obs/writer.go
// Observation writer — writes structured JSON to PRRJECT_FATBABY's observation directory.
// The observation-watcher polls this directory and invokes Claude Code on new observations.

package obs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Payload matches the PRRJECT_FATBABY observation JSON schema exactly.
// All agents (emily-agent, Emily CLI) write the same format.
type Payload struct {
	Timestamp    string `json:"timestamp"`
	Summary      string `json:"summary"`
	Severity     string `json:"severity"`
	Findings     string `json:"findings,omitempty"`
	SuggestedFix string `json:"suggested_fix,omitempty"`
	Amendment    string `json:"amendment,omitempty"`  // human correction note; original summary preserved
	AmendedAt    string `json:"amended_at,omitempty"` // RFC3339 timestamp of amendment
}

// Write atomically writes an observation to root/var/emily-observations/.
// Returns the path of the written file.
func Write(fatBabyRoot string, p Payload) (string, error) {
	dir := filepath.Join(fatBabyRoot, "var", "emily-observations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if p.Timestamp == "" {
		p.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if p.Severity == "" {
		p.Severity = "info"
	}

	// Filename: RFC3339 with colons replaced by dashes (filesystem-safe)
	fname := strings.ReplaceAll(p.Timestamp, ":", "-") + ".json"
	fpath := filepath.Join(dir, fname)

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(fpath, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", fpath, err)
	}

	// Atomic symlink: latest.json → new file.
	// os.Rename is atomic on Linux; create a temp link first.
	latestPath := filepath.Join(dir, "latest.json")
	tmpLink := latestPath + ".tmp"
	_ = os.Remove(tmpLink)
	if err := os.Symlink(fname, tmpLink); err != nil {
		// Symlink failure is non-fatal — file is still written and discoverable by name
		_ = err
	} else {
		_ = os.Rename(tmpLink, latestPath)
	}

	return fpath, nil
}

// Amend reads an existing observation file by its timestamp key, appends a
// correction note, and writes it back. The original summary is preserved.
// key must be an RFC3339 timestamp (e.g. "2026-06-11T01:24:22Z").
func Amend(fatBabyRoot, key, amendment string) (string, error) {
	dir := filepath.Join(fatBabyRoot, "var", "emily-observations")
	// Try exact filename first, then fuzzy match
	fname := strings.ReplaceAll(key, ":", "-") + ".json"
	fpath := filepath.Join(dir, fname)
	if _, err := os.Stat(fpath); os.IsNotExist(err) {
		// Try with Z-suffix normalisation
		alt := strings.TrimSuffix(strings.ReplaceAll(key, ":", "-"), "Z") + ".json"
		if _, err2 := os.Stat(filepath.Join(dir, alt)); err2 == nil {
			fname = alt
			fpath = filepath.Join(dir, alt)
		} else {
			return "", fmt.Errorf("observation %q not found in %s", key, dir)
		}
	}

	raw, err := os.ReadFile(fpath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fpath, err)
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("parse %s: %w", fpath, err)
	}
	p.Amendment = amendment
	p.AmendedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(fpath, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", fpath, err)
	}
	return fpath, nil
}
