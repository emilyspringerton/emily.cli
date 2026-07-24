package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// RunEmilyOS wraps the emilyos binary with env var passthrough.
// Usage: emily emilyos posture get|set <STATE>
//        emily emilyos verb <verb> <object>
//        emily emilyos audit tail|verify|export <outdir>
func RunEmilyOS(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(`emily emilyos — EmilyOS policy kernel interface

Usage:
  emily emilyos posture get
  emily emilyos posture set <STATE>
  emily emilyos verb <verb> <object>
  emily emilyos audit tail [-n N]
  emily emilyos audit verify
  emily emilyos audit export <outdir>

Env vars forwarded:
  EMILY_POSTURE_PATH — path to posture.json (default: var/posture.json)
  EMILY_AUDIT_PATH   — path to audit.jsonl  (default: var/audit.jsonl)
  EMILY_ACTOR_ID     — actor identity for verb dispatch
  EMILY_SESSION_ID   — session for audit attribution
  EMILY_ROLE         — role for capability check (operator|admin|auditor)

Exit codes:
  0 = success, 1 = error, 2 = policy deny, 3 = audit chain tampered
`)
		return 0
	}

	emilyosPath, err := resolveEmilyOS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily emilyos: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Hint: build with: cd /home/fatbaby/EmilyOS && GOWORK=off go build -o /usr/local/bin/emilyos ./cmd/emilyos\n")
		return 1
	}

	cmd := exec.Command(emilyosPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Forward EmilyOS env vars.
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "emily emilyos: %v\n", err)
		return 1
	}
	return 0
}

func resolveEmilyOS() (string, error) {
	// Prefer $PATH first so installed binaries are found.
	if path, err := exec.LookPath("emilyos"); err == nil {
		return path, nil
	}
	// Fallback: built binary beside emily.cli.
	candidates := []string{
		"/home/fatbaby/EmilyOS/emilyos",
		"/usr/local/bin/emilyos",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("emilyos binary not found in PATH or fallback locations")
}
