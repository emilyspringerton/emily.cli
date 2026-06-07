package cmd_test

import (
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/cmd"
)

func TestRunApples_unknownSubcommand(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() {
		code = cmd.RunApples([]string{"bogus"})
	})
	if code == 0 {
		t.Error("unknown subcommand should return non-zero exit code")
	}
	if !strings.Contains(stderr, "bogus") {
		t.Errorf("stderr should mention the unknown subcommand: %q", stderr)
	}
}

func TestRunApples_noSubcommand(t *testing.T) {
	var code int
	captureStderr(t, func() {
		code = cmd.RunApples([]string{})
	})
	if code == 0 {
		t.Error("no subcommand should return non-zero exit code")
	}
}

func TestRunApples_list_noSecret(t *testing.T) {
	t.Setenv("IDUNA_AGENT_SECRET", "")
	t.Setenv("IDUNA_SECRETS", "/nonexistent/secrets.env")

	var code int
	captureStderr(t, func() {
		code = cmd.RunApples([]string{"list"})
	})
	if code == 0 {
		t.Error("list without secret should return non-zero exit code")
	}
}

func TestRunApples_list_sinceFlag_parsesCleanly(t *testing.T) {
	// --since is a valid flag; verify it is parsed without error (auth error expected, not usage error).
	t.Setenv("IDUNA_AGENT_SECRET", "")
	t.Setenv("IDUNA_SECRETS", "/nonexistent/secrets.env")

	var code int
	captureStderr(t, func() {
		code = cmd.RunApples([]string{"list", "--since", "60", "-n", "100"})
	})
	if code == 1 {
		t.Error("--since flag should not cause a usage error (exit 1); got exit 1 — flag parse failed")
	}
}

func TestRunApples_get_badID(t *testing.T) {
	var code int
	captureStderr(t, func() {
		code = cmd.RunApples([]string{"get", "notanumber"})
	})
	if code == 0 {
		t.Error("get with non-numeric ID should return non-zero exit code")
	}
}

func TestRunApples_post_missingType(t *testing.T) {
	var code int
	captureStderr(t, func() {
		code = cmd.RunApples([]string{"post", "title without type"})
	})
	if code == 0 {
		t.Error("post without --type should return non-zero exit code")
	}
}
