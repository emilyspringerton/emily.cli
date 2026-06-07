package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/cmd"
)

func TestRunSync_noObsDir(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("FATBABY_ROOT", dir)
	t.Setenv("EMILY_ROOT", dir)
	t.Setenv("IDUNA_AGENT_SECRET", "fake-for-test")

	out := captureStdout(t, func() {
		code := cmd.RunSync([]string{"--dry-run"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})
	_ = out // "no observations found" or empty is acceptable
}

func TestRunSync_dryRun_withObsFiles(t *testing.T) {
	dir := t.TempDir()
	obsDir := filepath.Join(dir, "var", "emily-observations")
	os.MkdirAll(obsDir, 0o755)

	// Write a test observation file
	obs := map[string]string{
		"timestamp": "2026-06-07T10:00:00Z",
		"summary":   "sync test observation",
		"severity":  "info",
	}
	data, _ := json.Marshal(obs)
	os.WriteFile(filepath.Join(obsDir, "2026-06-07T10-00-00Z.json"), data, 0o644)

	t.Setenv("FATBABY_ROOT", dir)
	t.Setenv("EMILY_ROOT", dir)
	t.Setenv("IDUNA_AGENT_SECRET", "fake-for-test")

	out := captureStdout(t, func() {
		code := cmd.RunSync([]string{"--dry-run", "--all"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	if !strings.Contains(out, "DRY-RUN") {
		t.Errorf("dry-run output missing DRY-RUN marker: %q", out)
	}
	if !strings.Contains(out, "2026-06-07T10-00-00Z.json") {
		t.Errorf("dry-run output should show observation filename: %q", out)
	}
}

func TestRunSync_dryRun_skipsLatestJSON(t *testing.T) {
	dir := t.TempDir()
	obsDir := filepath.Join(dir, "var", "emily-observations")
	os.MkdirAll(obsDir, 0o755)

	// Write a real obs file and a latest.json symlink target
	obs := map[string]string{
		"timestamp": "2026-06-07T11:00:00Z",
		"summary":   "latest test",
		"severity":  "warn",
	}
	data, _ := json.Marshal(obs)
	fname := "2026-06-07T11-00-00Z.json"
	os.WriteFile(filepath.Join(obsDir, fname), data, 0o644)
	// Write latest.json as a regular file (symlink skipped in test)
	os.WriteFile(filepath.Join(obsDir, "latest.json"), data, 0o644)

	t.Setenv("FATBABY_ROOT", dir)
	t.Setenv("EMILY_ROOT", dir)
	t.Setenv("IDUNA_AGENT_SECRET", "fake-for-test")

	out := captureStdout(t, func() {
		cmd.RunSync([]string{"--dry-run", "--all"})
	})

	// latest.json should not appear as a posted file
	if strings.Contains(out, "latest.json →") {
		t.Errorf("latest.json should be skipped, but appeared in output: %q", out)
	}
	// The real file should appear
	if !strings.Contains(out, fname) {
		t.Errorf("real obs file should appear in output: %q", out)
	}
}

func TestRunSync_noSecret(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FATBABY_ROOT", dir)
	t.Setenv("EMILY_ROOT", dir)
	t.Setenv("IDUNA_AGENT_SECRET", "")
	t.Setenv("IDUNA_SECRETS", "/nonexistent/secrets.env")

	var code int
	captureStderr(t, func() {
		code = cmd.RunSync([]string{}) // no dry-run → needs secret
	})
	if code == 0 {
		t.Error("RunSync without secret and without --dry-run should fail")
	}
}
