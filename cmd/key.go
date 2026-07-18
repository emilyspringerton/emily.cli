package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// RunKey implements `emily key set|show|unset`.
//
// Two forms:
//
//	emily key set <api-key>                    — legacy shorthand: sets
//	                                              ANTHROPIC_API_KEY in EMILY's
//	                                              secrets file (back-compat).
//	emily key set <NAME> <VALUE> [--target T]  — general form: writes any
//	                                              named secret to a target env
//	                                              file. Targets:
//	                                                emily (default) — EMILY/var/emily-secrets.env,
//	                                                  shell-sourced (export-prefixed)
//	                                                iduna — ~/.config/iduna/env,
//	                                                  read by iduna.service's systemd
//	                                                  EnvironmentFile= (no export prefix —
//	                                                  systemd's parser doesn't understand it)
//	              --file <path>                — write to an arbitrary path instead
//
// Added 2026-07-18 (EMILY BACKLOG SECTION 153, S153-05) so secrets like
// MAILCHIMP_API_KEY (consumed by IDUNA's mailing-list feature) can be set
// without hand-editing env files.
func RunKey(args []string) int {
	if len(args) == 0 {
		return keyUsage()
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "set":
		return keySet(rest)
	case "show":
		return keyShow(rest)
	case "unset":
		return keyUnset(rest)
	default:
		fmt.Fprintf(os.Stderr, "emily key: unknown subcommand %q\n\n", sub)
		return keyUsage()
	}
}

// targetPath resolves --target/--file flags (and any remaining positional
// args) to a concrete file path. Returns the path and the remaining
// positional args with flags stripped.
func targetPath(args []string) (path string, rest []string) {
	emilyRoot := envOrKey("EMILY_ROOT", "/home/fatbaby/EMILY")
	path = config.ReadEmilySecretsFile(emilyRoot) // default target: emily

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 < len(args) {
				switch args[i+1] {
				case "emily":
					path = config.ReadEmilySecretsFile(emilyRoot)
				case "iduna":
					home, _ := os.UserHomeDir()
					path = home + "/.config/iduna/env"
				default:
					fmt.Fprintf(os.Stderr, "emily key: unknown --target %q (want: emily, iduna)\n", args[i+1])
				}
				i++
				continue
			}
		case "--file":
			if i+1 < len(args) {
				path = args[i+1]
				i++
				continue
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return path, rest
}

func keySet(args []string) int {
	path, rest := targetPath(args)

	// Legacy shorthand: a single arg that looks like an Anthropic key sets
	// ANTHROPIC_API_KEY in the (default or explicit) emily secrets file.
	if len(rest) == 1 && strings.HasPrefix(rest[0], "sk-ant-") {
		return keySetNamed(path, "ANTHROPIC_API_KEY", rest[0])
	}

	if len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "emily key set: usage: emily key set <NAME> <VALUE> [--target emily|iduna] [--file <path>]")
		fmt.Fprintln(os.Stderr, "           or: emily key set <api-key>   (legacy: sets ANTHROPIC_API_KEY)")
		return 1
	}
	name := strings.TrimSpace(rest[0])
	value := strings.TrimSpace(rest[1])
	if name == "" || value == "" {
		fmt.Fprintln(os.Stderr, "emily key set: name and value must not be empty")
		return 1
	}
	return keySetNamed(path, name, value)
}

func keySetNamed(path, name, value string) int {
	// EMILY's secrets file is shell-sourced (needs "export "); anything else
	// (iduna's env, or an explicit --file) is assumed to be a systemd
	// EnvironmentFile= target, which requires plain KEY=VALUE.
	emilyRoot := envOrKey("EMILY_ROOT", "/home/fatbaby/EMILY")
	exportPrefix := path == config.ReadEmilySecretsFile(emilyRoot)

	if err := config.WriteEnvFile(path, name, value, exportPrefix); err != nil {
		fmt.Fprintf(os.Stderr, "emily key set: failed to write %s: %v\n", path, err)
		return 3
	}

	fmt.Printf("✓ %s saved (%s)\n", name, maskKey(value))
	fmt.Printf("  File: %s\n", path)
	if strings.Contains(path, ".config/iduna/env") {
		fmt.Println("  Restart iduna.service to pick it up: systemctl --user restart iduna.service")
	} else {
		fmt.Println("  All emily commands will now load it automatically (if EMILY_ROOT's secrets file).")
	}
	return 0
}

func keyShow(args []string) int {
	path, rest := targetPath(args)

	if len(rest) == 0 {
		// Legacy behavior: report ANTHROPIC_API_KEY status.
		if fromEnv := os.Getenv("ANTHROPIC_API_KEY"); fromEnv != "" {
			fmt.Printf("ANTHROPIC_API_KEY is set in environment: %s\n", maskKey(fromEnv))
			return 0
		}
		cfg, _ := config.Resolve()
		if cfg != nil && cfg.AnthropicKey != "" {
			fmt.Printf("ANTHROPIC_API_KEY loaded from file (%s): %s\n", path, maskKey(cfg.AnthropicKey))
			return 0
		}
		fmt.Println("ANTHROPIC_API_KEY is not set.")
		fmt.Println("Run:  emily key set <api-key>")
		return 1
	}

	name := rest[0]
	value := config.ReadEnvValue(path, name)
	if value == "" {
		fmt.Printf("%s is not set in %s\n", name, path)
		return 1
	}
	fmt.Printf("%s is set in %s: %s\n", name, path, maskKey(value))
	return 0
}

func keyUnset(args []string) int {
	path, rest := targetPath(args)
	name := "ANTHROPIC_API_KEY"
	if len(rest) > 0 {
		name = rest[0]
	}
	if err := config.RemoveEnvFile(path, name); err != nil {
		fmt.Fprintf(os.Stderr, "emily key unset: %v\n", err)
		return 3
	}
	fmt.Printf("✓ %s removed from %s\n", name, path)
	return 0
}

func maskKey(key string) string {
	if len(key) <= 12 {
		return "***"
	}
	return key[:12] + "..." + key[len(key)-4:]
}

func keyUsage() int {
	fmt.Print(`emily key — manage secrets in env files

Subcommands:
  emily key set <api-key>                       Legacy: save ANTHROPIC_API_KEY to EMILY's secrets file
  emily key set <NAME> <VALUE> [--target T]     Save a named secret (default target: emily)
  emily key show [NAME] [--target T]            Show whether a secret is configured (masked)
  emily key unset [NAME] [--target T]           Remove a secret

Targets (--target):
  emily   EMILY/var/emily-secrets.env  (default; shell-sourced, export-prefixed)
  iduna   ~/.config/iduna/env          (read by iduna.service's systemd EnvironmentFile=)

Or use --file <path> to target an arbitrary env file directly.

File permissions: 0600 (owner read/write only) on every write.

Examples:
  emily key set sk-ant-api03-...                          # legacy ANTHROPIC_API_KEY shorthand
  emily key set MAILCHIMP_API_KEY abc123-us21 --target iduna
  emily key set MAILCHIMP_LIST_ID a1b2c3d4e5 --target iduna
  emily key show MAILCHIMP_API_KEY --target iduna
  emily key unset MAILCHIMP_API_KEY --target iduna
`)
	return 1
}

func envOrKey(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
