// emily — Emily Lite CLI
// Operator terminal for the Emily OS / Einhorn Industrial agent system.
//
// Usage:
//   emily observe [flags] <message>        — post observation to FatBaby pipeline
//   emily apples list [filter]             — query IDUNA Apples log
//   emily apples post -t <type> <title>    — post Apple to IDUNA
//   emily watch [repo]                     — tail IDUNA Apples log in real-time
//   emily status                           — cross-repo git + IDUNA state
//   emily sync [--all] [--dry-run]         — sync FatBaby observations → IDUNA
//
// Auth: reads IDUNA_AGENT_SECRET from env or IDUNA/var/agent-secrets.env automatically.
// Docs: docs/NORTHSTAR.md, docs/COMMANDS.md, docs/DESIGN.md

package main

import (
	"fmt"
	"os"

	"github.com/emilyspringerton/emily-cli/cmd"
)

const version = "0.3.0"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	command := args[0]
	rest := args[1:]

	if command == "--version" || command == "-v" {
		fmt.Printf("emily %s\n", version)
		os.Exit(0)
	}
	if command == "--help" || command == "-h" || command == "help" {
		printUsage()
		os.Exit(0)
	}

	var code int
	switch command {
	case "observe":
		code = cmd.RunObserve(rest)
	case "apples":
		code = cmd.RunApples(rest)
	case "watch":
		code = cmd.RunWatch(rest)
	case "status":
		code = cmd.RunStatus(rest)
	case "sync":
		code = cmd.RunSync(rest)
	default:
		fmt.Fprintf(os.Stderr, "emily: unknown command %q\n\n", command)
		printUsage()
		os.Exit(1)
	}

	os.Exit(code)
}

func printUsage() {
	fmt.Print(`emily — Emily Lite CLI (` + version + `)

Operator terminal for the Einhorn Industrial agent system.
Docs: emily.cli/docs/NORTHSTAR.md

Usage:
  emily observe [flags] <message>
        Post an observation to the FatBaby pipeline.
        Flags: -s/--severity info|warn|error, --findings, --fix, --dry-run

  emily apples list [filter]
        Query IDUNA Apples log.
        Flags: -n/--limit N, -t/--type <type>, --full, --json

  emily apples post -t <type> <title> [body]
        Post an Apple to IDUNA. Body read from stdin if not given.

  emily watch [repo]
        Tail IDUNA Apples log in real-time. Polls every 5s (--interval N).
        Prints new Apples as they arrive. Ctrl-C to stop.

  emily status
        Cross-repo git state + last Apple per agent from IDUNA.

  emily sync [--all] [--dry-run]
        Sync FatBaby observations → IDUNA as signal_observation Apples.

Environment:
  IDUNA_BASE_URL      IDUNA server (default: http://localhost:8080)
  IDUNA_AGENT_NAME    Agent name (default: EMILY-PRIME)
  IDUNA_AGENT_SECRET  M2M secret (auto-read from IDUNA/var/agent-secrets.env)
  FATBABY_ROOT        PRRJECT_FATBABY root (default: /home/fatbaby/PRRJECT_FATBABY)

Exit codes: 0=ok 1=usage 2=auth 3=write-failure 4=api-error
`)
}
