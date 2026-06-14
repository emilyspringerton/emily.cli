package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// RunKey implements `emily key set|show|unset`.
// Manages ANTHROPIC_API_KEY in EMILY/var/emily-secrets.env so that all emily
// commands that call Anthropic APIs (emily backlog promote, emily context build)
// work without manually exporting the key each session.
func RunKey(args []string) int {
	if len(args) == 0 {
		return keyUsage()
	}

	sub := args[0]
	rest := args[1:]

	emilyRoot := envOrKey("EMILY_ROOT", "/home/fatbaby/EMILY")
	secretsFile := config.ReadEmilySecretsFile(emilyRoot)

	switch sub {
	case "set":
		return keySet(rest, secretsFile)
	case "show":
		return keyShow(secretsFile)
	case "unset":
		return keyUnset(secretsFile)
	default:
		fmt.Fprintf(os.Stderr, "emily key: unknown subcommand %q\n\n", sub)
		return keyUsage()
	}
}

func keySet(args []string, secretsFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily key set: API key required\n\nUsage: emily key set <api-key>")
		return 1
	}
	key := strings.TrimSpace(args[0])
	if key == "" {
		fmt.Fprintln(os.Stderr, "emily key set: key must not be empty")
		return 1
	}
	if !strings.HasPrefix(key, "sk-ant-") {
		fmt.Fprintln(os.Stderr, "emily key set: key should start with sk-ant-\nObtain one at https://console.anthropic.com/")
		return 1
	}

	if err := config.WriteEmilySecret(secretsFile, "ANTHROPIC_API_KEY", key); err != nil {
		fmt.Fprintf(os.Stderr, "emily key set: failed to write secrets file: %v\n", err)
		return 3
	}

	masked := maskKey(key)
	fmt.Printf("✓ ANTHROPIC_API_KEY saved (%s)\n", masked)
	fmt.Printf("  File: %s\n", secretsFile)
	fmt.Println("  All emily commands will now load it automatically.")
	fmt.Println("  To export for this shell session:")
	fmt.Printf("    export ANTHROPIC_API_KEY=%s\n", key)
	fmt.Println("  Or add to ~/.bashrc:")
	fmt.Printf("    echo 'source %s' >> ~/.bashrc\n", secretsFile)
	return 0
}

func keyShow(secretsFile string) int {
	// Check env first (may have been injected by Resolve or set manually).
	fromEnv := os.Getenv("ANTHROPIC_API_KEY")
	if fromEnv != "" {
		fmt.Printf("ANTHROPIC_API_KEY is set in environment: %s\n", maskKey(fromEnv))
		return 0
	}

	// Check secrets file.
	cfg, _ := config.Resolve()
	if cfg != nil && cfg.AnthropicKey != "" {
		fmt.Printf("ANTHROPIC_API_KEY loaded from file (%s): %s\n", secretsFile, maskKey(cfg.AnthropicKey))
		return 0
	}

	fmt.Println("ANTHROPIC_API_KEY is not set.")
	fmt.Println("Run:  emily key set <api-key>")
	return 1
}

func keyUnset(secretsFile string) int {
	if err := config.RemoveEmilySecret(secretsFile, "ANTHROPIC_API_KEY"); err != nil {
		fmt.Fprintf(os.Stderr, "emily key unset: %v\n", err)
		return 3
	}
	fmt.Printf("✓ ANTHROPIC_API_KEY removed from %s\n", secretsFile)
	fmt.Println("  Note: if it is set in your shell environment, unset it with:")
	fmt.Println("    unset ANTHROPIC_API_KEY")
	return 0
}

func maskKey(key string) string {
	if len(key) <= 12 {
		return "***"
	}
	return key[:12] + "..." + key[len(key)-4:]
}

func keyUsage() int {
	fmt.Print(`emily key — manage ANTHROPIC_API_KEY

Saves the key to EMILY/var/emily-secrets.env so all emily commands that call
Anthropic APIs (emily backlog promote, emily context build) work automatically.

Subcommands:
  emily key set <api-key>   Save key to EMILY/var/emily-secrets.env
  emily key show            Show whether a key is configured (masked)
  emily key unset           Remove key from EMILY/var/emily-secrets.env

The key is loaded automatically by emily on every invocation — no need to
export it in each shell session. It is also injected into the process env so
subcommands calling os.Getenv("ANTHROPIC_API_KEY") see it without extra wiring.

File permissions: 0600 (owner read/write only).

Examples:
  emily key set sk-ant-api03-...
  emily key show
  emily key unset
`)
	return 1
}

func envOrKey(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
