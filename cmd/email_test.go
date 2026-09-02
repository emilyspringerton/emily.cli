package cmd

import (
	"os"
	"testing"
)

// TestRunEmail_MissingRequiredFlags covers the real validation path -- no network access
// needed, this never reaches sendPlainTextEmail. Actually sending mail needs a real SMTP
// endpoint and real credentials, out of scope for a unit test; see this file's own header
// comment for how that side was verified instead (a live send attempt this same session).
func TestRunEmail_MissingRequiredFlags(t *testing.T) {
	cases := [][]string{
		{},
		{"send"},
		{"send", "--to", "x@example.com"},
		{"send", "--to", "x@example.com", "--subject", "hi"},
		{"bogus-subcommand"},
	}
	for _, args := range cases {
		if code := RunEmail(args); code == 0 {
			t.Errorf("RunEmail(%v) = 0, want nonzero (missing required flags)", args)
		}
	}
}

func TestRunEmail_UnknownFlag(t *testing.T) {
	code := RunEmail([]string{"send", "--to", "x@example.com", "--subject", "hi", "--body", "hey", "--bogus"})
	if code == 0 {
		t.Errorf("RunEmail with an unknown flag = 0, want nonzero")
	}
}

func TestRunEmail_BodyFileNotFound(t *testing.T) {
	code := RunEmail([]string{"send", "--to", "x@example.com", "--subject", "hi", "--body-file", "/nonexistent/path/body.txt"})
	if code == 0 {
		t.Errorf("RunEmail with a missing --body-file = 0, want nonzero")
	}
}

// TestRunEmail_NoCredentialsConfigured verifies the real, honest "not configured" error path
// (rather than a silent hang trying to actually dial SMTP) when GMAIL_SMTP_ADDRESS/PASSWORD are
// genuinely unset. Clears both real env vars for the duration of this test and points
// EMILY_SECRETS at an empty temp file so config.Resolve() can't fall back to a real secrets
// file on this machine and mask the case being tested.
func TestRunEmail_NoCredentialsConfigured(t *testing.T) {
	t.Setenv("GMAIL_SMTP_ADDRESS", "")
	t.Setenv("GMAIL_SMTP_PASSWORD", "")
	empty := t.TempDir() + "/empty-secrets.env"
	if err := os.WriteFile(empty, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EMILY_SECRETS", empty)

	code := RunEmail([]string{"send", "--to", "x@example.com", "--subject", "hi", "--body", "hey"})
	if code == 0 {
		t.Errorf("RunEmail with no Gmail credentials configured = 0, want nonzero")
	}
}
