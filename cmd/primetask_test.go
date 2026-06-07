package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/cmd"
)

func TestRunPrimeTask_dryRun(t *testing.T) {
	out := captureStdout(t, func() {
		code := cmd.RunPrimeTask([]string{"--dry-run", "test task description"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	if !strings.Contains(out, "test task description") {
		t.Errorf("dry-run output missing description: %q", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("dry-run output missing marker: %q", out)
	}
	if !strings.Contains(out, "task_id") {
		t.Errorf("dry-run output missing task_id: %q", out)
	}
}

func TestRunPrimeTask_dryRunJSON(t *testing.T) {
	out := captureStdout(t, func() {
		code := cmd.RunPrimeTask([]string{"--dry-run", "--json", "json dry run task"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("--json output not valid JSON: %v\nout: %q", err, out)
	}
	if m["dry_run"] != true {
		t.Errorf("dry_run field: got %v want true", m["dry_run"])
	}
	if _, ok := m["task_id"]; !ok {
		t.Error("dry_run JSON missing task_id field")
	}
	if _, ok := m["filename"]; !ok {
		t.Error("dry_run JSON missing filename field")
	}
}

func TestRunPrimeTask_noArgs(t *testing.T) {
	var code int
	captureStderr(t, func() {
		code = cmd.RunPrimeTask([]string{})
	})
	if code == 0 {
		t.Error("no args should return non-zero exit code")
	}
}

func TestRunPrimeTask_dryRun_priority(t *testing.T) {
	cases := []string{"low", "normal", "high", "critical"}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			out := captureStdout(t, func() {
				code := cmd.RunPrimeTask([]string{"--dry-run", "--priority", p, "desc"})
				if code != 0 {
					t.Errorf("exit code %d for priority %s", code, p)
				}
			})
			if !strings.Contains(out, p) {
				t.Errorf("priority %q not in output: %q", p, out)
			}
		})
	}
}

func TestRunPrimeTask_badPriority(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() {
		code = cmd.RunPrimeTask([]string{"--dry-run", "--priority", "urgent", "desc"})
	})
	if code == 0 {
		t.Error("bad priority should return non-zero exit code")
	}
	if !strings.Contains(stderr, "urgent") {
		t.Errorf("stderr should mention the bad value: %q", stderr)
	}
}

func TestRunPrimeTask_dryRun_withCriteria(t *testing.T) {
	out := captureStdout(t, func() {
		cmd.RunPrimeTask([]string{
			"--dry-run",
			"--criteria", "tests pass",
			"--criteria", "committed to git",
			"task with criteria",
		})
	})
	if !strings.Contains(out, "tests pass") {
		t.Errorf("criteria not in output: %q", out)
	}
	if !strings.Contains(out, "committed to git") {
		t.Errorf("second criterion not in output: %q", out)
	}
}

func TestRunPrimeTask_dryRun_multiWordDescription(t *testing.T) {
	out := captureStdout(t, func() {
		cmd.RunPrimeTask([]string{
			"--dry-run",
			"improve", "entity", "graph", "parser",
		})
	})
	// Multiple positional args should be joined
	if !strings.Contains(out, "improve entity graph parser") {
		t.Errorf("multi-word description not joined: %q", out)
	}
}

func TestRunPrimeTask_dryRun_defaultType(t *testing.T) {
	out := captureStdout(t, func() {
		cmd.RunPrimeTask([]string{"--dry-run", "desc"})
	})
	if !strings.Contains(out, "operator_directive") {
		t.Errorf("default type should be operator_directive: %q", out)
	}
}
