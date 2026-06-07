package cmd_test

import (
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/cmd"
)

func TestRunInstall_noCron(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() {
		code = cmd.RunInstall([]string{})
	})
	if code == 0 {
		t.Error("no flags should return non-zero exit code")
	}
	if !strings.Contains(stderr, "usage") && !strings.Contains(stderr, "--cron") {
		t.Errorf("stderr should show usage: %q", stderr)
	}
}

func TestRunInstall_cron_print(t *testing.T) {
	out := captureStdout(t, func() {
		code := cmd.RunInstall([]string{"--cron"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	// Must contain a sync entry
	if !strings.Contains(out, "emily") && !strings.Contains(out, "sync") {
		t.Errorf("cron output should mention emily sync: %q", out)
	}
	// Must contain a cron expression
	if !strings.Contains(out, "* * * *") {
		t.Errorf("cron output should contain a cron expression: %q", out)
	}
	// Must contain Tyler cron
	if !strings.Contains(out, "cron-emily.sh") {
		t.Errorf("cron output should contain cron-emily.sh entry: %q", out)
	}
}

func TestRunInstall_cron_noWriteByDefault(t *testing.T) {
	out := captureStdout(t, func() {
		cmd.RunInstall([]string{"--cron"})
	})
	// Without --write, should print instructions but not run crontab
	if !strings.Contains(out, "--write") {
		t.Errorf("print-only mode should mention --write: %q", out)
	}
}

func TestRunInstall_systemd_print(t *testing.T) {
	out := captureStdout(t, func() {
		code := cmd.RunInstall([]string{"--systemd"})
		if code != 0 {
			t.Errorf("exit code: got %d want 0", code)
		}
	})

	if !strings.Contains(out, "[Unit]") {
		t.Errorf("systemd output should contain [Unit] section: %q", out)
	}
	if !strings.Contains(out, "emily sync") {
		t.Errorf("systemd output should reference emily sync: %q", out)
	}
	if !strings.Contains(out, "emily-sync.service") {
		t.Errorf("systemd output should mention the unit name: %q", out)
	}
}

func TestRunInstall_systemd_noWriteByDefault(t *testing.T) {
	out := captureStdout(t, func() {
		cmd.RunInstall([]string{"--systemd"})
	})
	// Without --write, should print instructions
	if !strings.Contains(out, "--write") {
		t.Errorf("print-only mode should mention --write: %q", out)
	}
}
