package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/cmd"
)

func TestRunObserve_dryRun(t *testing.T) {
	out := captureStdout(t, func() {
		code := cmd.RunObserve([]string{"--dry-run", "test observation message"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	if !strings.Contains(out, "test observation message") {
		t.Errorf("dry-run output missing summary: %q", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("dry-run output missing dry-run marker: %q", out)
	}
}

func TestRunObserve_dryRunJSON(t *testing.T) {
	out := captureStdout(t, func() {
		code := cmd.RunObserve([]string{"--dry-run", "--json", "json dry run test"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("dry-run --json not valid JSON: %v\nout: %q", err, out)
	}
	if m["dry_run"] != true {
		t.Errorf("dry_run field: got %v want true", m["dry_run"])
	}
	if m["summary"] != "json dry run test" {
		t.Errorf("summary field: got %v want %q", m["summary"], "json dry run test")
	}
}

func TestRunObserve_dryRun_severity(t *testing.T) {
	cases := []struct {
		severity string
	}{
		{"info"},
		{"warn"},
		{"error"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			out := captureStdout(t, func() {
				code := cmd.RunObserve([]string{"--dry-run", "-s", tc.severity, "msg"})
				if code != 0 {
					t.Errorf("exit code %d for severity %s", code, tc.severity)
				}
			})
			if !strings.Contains(out, tc.severity) {
				t.Errorf("severity %q not in output: %q", tc.severity, out)
			}
		})
	}
}

func TestRunObserve_badSeverity(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() {
		code = cmd.RunObserve([]string{"--dry-run", "-s", "critical", "msg"})
	})
	if code == 0 {
		t.Error("bad severity should return non-zero exit code")
	}
	if !strings.Contains(stderr, "critical") {
		t.Errorf("stderr should mention the bad severity: %q", stderr)
	}
}

func TestRunObserve_noArgs(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() {
		code = cmd.RunObserve([]string{})
	})
	if code == 0 {
		t.Error("no args should return non-zero exit code")
	}
	if !strings.Contains(stderr, "usage") && !strings.Contains(stderr, "observe") {
		t.Errorf("stderr should show usage: %q", stderr)
	}
}

func TestRunObserve_stdin_dryRun(t *testing.T) {
	var out string
	pipeStdin(t, "stdin message from pipe\n", func() {
		out = captureStdout(t, func() {
			code := cmd.RunObserve([]string{"--dry-run"})
			if code != 0 {
				t.Errorf("exit code: got %d want 0", code)
			}
		})
	})
	if !strings.Contains(out, "stdin message from pipe") {
		t.Errorf("stdin message missing from output: %q", out)
	}
}

func TestRunObserve_dryRun_withFindings(t *testing.T) {
	out := captureStdout(t, func() {
		cmd.RunObserve([]string{
			"--dry-run",
			"--findings", "detailed findings text",
			"--fix", "suggested remediation",
			"summary text",
		})
	})
	if !strings.Contains(out, "detailed findings text") {
		t.Errorf("findings not in output: %q", out)
	}
	if !strings.Contains(out, "suggested remediation") {
		t.Errorf("fix not in output: %q", out)
	}
}
