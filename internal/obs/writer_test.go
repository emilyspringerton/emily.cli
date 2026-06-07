package obs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/obs"
)

func TestWrite_createsFile(t *testing.T) {
	dir := t.TempDir()
	p := obs.Payload{Summary: "test summary", Severity: "info"}

	path, err := obs.Write(dir, p)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not found at %s: %v", path, err)
	}
}

func TestWrite_jsonSchema(t *testing.T) {
	dir := t.TempDir()
	p := obs.Payload{
		Summary:      "schema test",
		Severity:     "warn",
		Findings:     "detailed findings here",
		SuggestedFix: "try rebooting",
	}

	path, err := obs.Write(dir, p)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, _ := os.ReadFile(path)
	var got obs.Payload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Summary != p.Summary {
		t.Errorf("summary: got %q want %q", got.Summary, p.Summary)
	}
	if got.Severity != p.Severity {
		t.Errorf("severity: got %q want %q", got.Severity, p.Severity)
	}
	if got.Findings != p.Findings {
		t.Errorf("findings: got %q want %q", got.Findings, p.Findings)
	}
	if got.SuggestedFix != p.SuggestedFix {
		t.Errorf("suggested_fix: got %q want %q", got.SuggestedFix, p.SuggestedFix)
	}
	if got.Timestamp == "" {
		t.Error("timestamp is empty")
	}
}

func TestWrite_defaultSeverity(t *testing.T) {
	dir := t.TempDir()
	p := obs.Payload{Summary: "no severity set"}

	path, _ := obs.Write(dir, p)
	data, _ := os.ReadFile(path)
	var got obs.Payload
	json.Unmarshal(data, &got)

	if got.Severity != "info" {
		t.Errorf("default severity: got %q want %q", got.Severity, "info")
	}
}

func TestWrite_filenameIsFsafe(t *testing.T) {
	dir := t.TempDir()
	p := obs.Payload{Summary: "fname test"}

	path, _ := obs.Write(dir, p)
	fname := filepath.Base(path)

	if strings.Contains(fname, ":") {
		t.Errorf("filename contains colon: %q", fname)
	}
	if !strings.HasSuffix(fname, ".json") {
		t.Errorf("filename missing .json suffix: %q", fname)
	}
}

func TestWrite_timestampIsRFC3339(t *testing.T) {
	dir := t.TempDir()
	p := obs.Payload{Summary: "ts test"}

	path, _ := obs.Write(dir, p)
	data, _ := os.ReadFile(path)
	var got obs.Payload
	json.Unmarshal(data, &got)

	if _, err := time.Parse(time.RFC3339, got.Timestamp); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", got.Timestamp, err)
	}
}

func TestWrite_latestSymlink(t *testing.T) {
	dir := t.TempDir()
	p := obs.Payload{Summary: "symlink test"}

	path, _ := obs.Write(dir, p)
	obsDir := filepath.Join(dir, "var", "emily-observations")
	latest := filepath.Join(obsDir, "latest.json")

	target, err := os.Readlink(latest)
	if err != nil {
		t.Fatalf("latest.json symlink: %v", err)
	}
	if target != filepath.Base(path) {
		t.Errorf("latest.json → %q, want %q", target, filepath.Base(path))
	}
}

func TestWrite_latestSymlinkUpdates(t *testing.T) {
	dir := t.TempDir()

	obs.Write(dir, obs.Payload{Summary: "first", Timestamp: "2026-06-07T00:00:00Z"})
	time.Sleep(2 * time.Millisecond) // ensure different mtime
	path2, _ := obs.Write(dir, obs.Payload{Summary: "second", Timestamp: "2026-06-07T00:00:01Z"})

	obsDir := filepath.Join(dir, "var", "emily-observations")
	latest := filepath.Join(obsDir, "latest.json")
	target, _ := os.Readlink(latest)

	if target != filepath.Base(path2) {
		t.Errorf("latest.json should point to second write %q, got %q", filepath.Base(path2), target)
	}
}

func TestWrite_explicitTimestamp(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-01-15T10:30:00Z"
	p := obs.Payload{Summary: "ts explicit", Timestamp: ts}

	path, _ := obs.Write(dir, p)
	data, _ := os.ReadFile(path)
	var got obs.Payload
	json.Unmarshal(data, &got)

	if got.Timestamp != ts {
		t.Errorf("timestamp: got %q want %q", got.Timestamp, ts)
	}
	if !strings.Contains(filepath.Base(path), "2026-01-15T10-30-00Z") {
		t.Errorf("filename should contain sanitized timestamp, got %q", filepath.Base(path))
	}
}
