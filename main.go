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
	if command == "--help" || command == "-h" {
		printUsage()
		os.Exit(0)
	}
	// emily help <command> — dispatch to per-command help
	if command == "help" {
		if len(rest) == 0 {
			printUsage()
			os.Exit(0)
		}
		os.Exit(printCommandHelp(rest[0]))
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

func printCommandHelp(command string) int {
	switch command {
	case "observe":
		fmt.Print(`emily observe — post an observation to the FatBaby pipeline

Usage:
  emily observe [flags] <message>
  echo "message" | emily observe [flags]

Flags:
  -s, --severity string   info | warn | error  (default: info)
  --findings string        Longer findings text (message becomes the summary)
  --fix string             Suggested remediation
  --no-apple               Skip IDUNA Apple receipt (obs file still written)
  --dry-run                Print JSON that would be written; don't write it
  --json                   Output JSON with written path and Apple ID

Behavior:
  1. Writes <FATBABY_ROOT>/var/emily-observations/<RFC3339>.json
  2. Atomically updates latest.json symlink
  3. Observation-watcher picks up within ~10s → invokes Claude Code
  4. If IDUNA credentials available: posts signal_observation Apple immediately

Examples:
  emily observe "redis heap is growing"
  emily observe -s error "eps-processor at 0 articles/hr" --fix "check ticker map"
  git log -1 --oneline | emily observe -s info
  emily observe --dry-run "test probe"
`)
	case "apples":
		fmt.Print(`emily apples — interact with the IDUNA Apples log

Subcommands:
  emily apples list [flags] [repo]
  emily apples post [flags] <title> [body]
  emily apples get [flags] <id>

list flags:
  -n, --limit int    Max apples (default 20)
  -t, --type string  Filter by apple_type
  -r, --repo string  Filter by source_repo (or positional arg)
  --full             Show body (first 5 lines)
  --json             Output JSON array

post flags:
  -t, --type string  apple_type (required)
  -r, --repo string  source_repo (default: CLI)
  --run-id string    run_id tag
  --json             Output JSON with filed ID
  [body]             Body text; read from stdin if omitted and stdin is a pipe

get flags:
  --json             Output full Apple as JSON

Examples:
  emily apples list                        # last 20 apples
  emily apples list TYLER -n 5            # last 5 from TYLER
  emily apples list -t signal_observation --full
  emily apples get 39                      # show Apple #39 with full body
  echo "body text" | emily apples post -t rsi_iteration "Build 0019"
`)
	case "watch":
		fmt.Print(`emily watch — tail IDUNA Apples log in real-time

Usage:
  emily watch [flags] [repo]

Flags:
  -i, --interval int   Poll interval in seconds (default 5)
  -r, --repo string    Filter by source_repo (or positional arg)
  -t, --type string    Filter by apple_type
  --quiet              Suppress header; output one line per apple

Behavior:
  Bootstraps at the current highest Apple ID — only new apples are shown.
  Polls every --interval seconds. Ctrl-C exits cleanly.

Examples:
  emily watch                    # all repos, 5s poll
  emily watch TYLER              # only TYLER apples
  emily watch --interval 2 -t rsi_iteration
  emily watch --quiet TYLER      # machine-readable stream
`)
	case "status":
		fmt.Print(`emily status — cross-repo git state + last Apple per agent

Usage:
  emily status [flags]

Flags:
  --no-git      Skip git repo checks
  --no-iduna    Skip IDUNA Apple query
  --json        Output full status as JSON (repos + last apples)

Shows: branch, last commit, dirty count, backlog done/pending for each repo.
Shows: last Apple per source_repo, total Apple count.
`)
	case "sync":
		fmt.Print(`emily sync — sync FatBaby observations → IDUNA as signal_observation Apples

Usage:
  emily sync [flags]

Flags:
  --all              Backfill all observations (not just new ones)
  --dry-run          Show what would be posted
  --limit int        Max observations to process per pass (default 10)
  --watch            Daemon mode — poll for new obs files until Ctrl-C
  --interval int     Poll interval in seconds for --watch mode (default 10)
  --json             Output JSON per-Apple line

State file: EMILY/var/fatbaby-synced.txt tracks what's been posted.

Examples:
  emily sync                      # sync up to 10 new observations
  emily sync --all                # backfill everything
  emily sync --dry-run --all      # preview what would be posted
  emily sync --watch              # daemon: auto-post as obs files appear
  emily sync --watch --interval 5 # daemon with 5s poll
`)
	default:
		fmt.Fprintf(os.Stderr, "emily: no help for %q — try: emily help\n", command)
		return 1
	}
	return 0
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
