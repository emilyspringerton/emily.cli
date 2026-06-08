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

const version = "0.6.0"

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
	case "start":
		code = cmd.RunStart(rest)
	case "observe", "eo":
		code = cmd.RunObserve(rest)
	case "apples":
		code = cmd.RunApples(rest)
	case "watch":
		code = cmd.RunWatch(rest)
	case "status":
		code = cmd.RunStatus(rest)
	case "sync":
		code = cmd.RunSync(rest)
	case "install":
		code = cmd.RunInstall(rest)
	case "prime-task":
		code = cmd.RunPrimeTask(rest)
	case "agents":
		code = cmd.RunAgents(rest)
	case "tui":
		code = cmd.RunTUI(rest)
	default:
		fmt.Fprintf(os.Stderr, "emily: unknown command %q\n\n", command)
		printUsage()
		os.Exit(1)
	}

	os.Exit(code)
}

func printCommandHelp(command string) int {
	switch command {
	case "start":
		fmt.Print(`emily start — launch the Emily OS agent stack in the background

Usage:
  emily start [flags]

Flags:
  --iduna      also start IDUNA via systemctl --user start iduna.service
  --dry-run    show what would be started without actually starting anything

Behavior:
  1. (--iduna) Checks if iduna.service is active; starts it if not.
  2. Starts observation-watcher as a detached background process.
     Polls PRRJECT_FATBABY/var/emily-observations/ and EMILY/signals/tasks/
     every 10s; invokes 'claude' when a new observation or prime-task arrives.
  3. Starts emily-agent in daemon mode (~5m cycle with jitter).
     Runs the RSI loop autonomously, posting Apple receipts to IDUNA.
  Both processes log to EMILY/var/logs/.

Idempotent: already-running processes are detected via pgrep and skipped.

Examples:
  emily start                   # start obs-watcher + emily-agent
  emily start --iduna           # also bring up IDUNA
  emily start --dry-run         # preview without starting
`)
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
  --no-git         Skip git repo checks
  --no-iduna       Skip IDUNA Apple query
  --json           Output full status as JSON (repos + last apples)
  --watch          Live-updating mode — refresh every --interval seconds
  --interval int   Refresh interval for --watch (default 30)

Shows: branch, last commit, dirty count, backlog done/pending for each repo.
Shows: last Apple per source_repo, total Apple count.

Examples:
  emily status                    # one-shot status
  emily status --watch            # live dashboard, 30s refresh
  emily status --watch --interval 10 --no-git  # IDUNA-only, 10s refresh
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
	case "agents":
		fmt.Print(`emily agents — agent activity dashboard from IDUNA Apples log

Usage:
  emily agents [flags]

Flags:
  -n int         Apples to scan for activity history (default 200)
  --since int    Only show agents active in the last N minutes
  --json         Output JSON array

Output: REPO | LAST SEEN | TOTAL | LAST TYPE | LAST TITLE (newest-active first)

Note: synthesized from Apples log — reflects last Apple posted, not a heartbeat.
Agents that have not posted an Apple won't appear, even if registered.

Examples:
  emily agents              # all agents, last 200 apples
  emily agents --since 60   # agents active in the last hour
  emily agents --json       # JSON for scripts/piping
`)
	case "prime-task":
		fmt.Print(`emily prime-task — write a directed task to EMILY/signals/tasks/

The observation-watcher (PRRJECT_FATBABY) polls EMILY/signals/tasks/ every 10s.
When it finds a new task file it invokes Claude Code on FatBaby with the task
as the prompt. This closes the operator → Emily Prime → FatBaby directed loop.

Usage:
  emily prime-task [flags] <description>

Flags:
  --type string       task_type field (default: operator_directive)
  --priority string   low|normal|high|critical (default: normal)
  --context string    strategic context for Claude (optional)
  --criteria string   acceptance criterion (repeatable)
  --deadline string   optional deadline (free text)
  --dry-run           print what would be written without writing it
  --no-apple          skip IDUNA Apple receipt
  --json              output JSON confirmation

Examples:
  emily prime-task "add test for eps-processor edge case in Q1 earnings"
  emily prime-task --priority high --type improve_signal \
    --criteria "go test ./... passes" \
    "entity-graph parser misses director names with Jr. suffix"
  emily prime-task --dry-run "preview task without writing"
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
  emily start [--iduna] [--dry-run]
        Start the Emily OS agent stack in the background.
        Launches observation-watcher + emily-agent daemon; --iduna also starts IDUNA.

  emily observe [flags] <message>   (alias: emily eo)
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

  emily sync [--all] [--dry-run] [--watch]
        Sync FatBaby observations → IDUNA as signal_observation Apples.
        --watch runs as a daemon, polling every --interval seconds.

  emily install --cron [--write]
        Print recommended crontab entries. --write installs them.

  emily prime-task [flags] <description>
        Write a directed task to EMILY/signals/tasks/ for the obs-watcher.
        Flags: --type, --priority, --context, --criteria (repeatable), --dry-run

  emily agents [--since N]
        Agent activity dashboard — last-seen, apple count, and last apple per repo.
        Synthesized from the Apples log. --since N = only agents active in last N minutes.

  emily tui
        Bloomberg-style terminal dashboard. Live panels: repos, Apple feed, process health.
        Hotkeys: F1=fire RSI task, F2=run Tyler, F3=start system, F4=tail logs, r=refresh, q=quit.

Environment:
  IDUNA_BASE_URL      IDUNA server (default: http://localhost:8080)
  IDUNA_AGENT_NAME    Agent name (default: EMILY-PRIME)
  IDUNA_AGENT_SECRET  M2M secret (auto-read from IDUNA/var/agent-secrets.env)
  FATBABY_ROOT        PRRJECT_FATBABY root (default: /home/fatbaby/PRRJECT_FATBABY)

Exit codes: 0=ok 1=usage 2=auth 3=write-failure 4=api-error
`)
}
