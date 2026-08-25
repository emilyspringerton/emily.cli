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

const version = "1.0.0"

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
	case "backlog":
		code = cmd.RunBacklog(rest)
	case "changelog":
		code = cmd.RunChangelog(rest)
	case "context":
		code = cmd.RunContext(rest)
	case "northstar":
		code = cmd.RunNorthstar(rest)
	case "golden":
		code = cmd.RunGolden(rest)
	case "train":
		code = cmd.RunTrain(rest)
	case "gpt2":
		code = cmd.RunGPT2(rest)
	case "key":
		code = cmd.RunKey(rest)
	case "chat":
		code = cmd.RunChat(rest)
	case "shankpit":
		code = cmd.RunShankpit(rest)
	case "iduna":
		code = cmd.RunIduna(rest)
	case "redgarden":
		code = cmd.RunRedgarden(rest)
	case "survival":
		code = cmd.RunSurvival(rest)
	case "gsync":
		code = cmd.RunGSync(rest)
	case "memory":
		code = cmd.RunMemory(rest)
	case "emilyos":
		code = cmd.RunEmilyOS(rest)
	case "session":
		code = cmd.RunSession(rest)
	case "saga":
		code = cmd.RunSaga(rest)
	case "vault":
		code = cmd.RunVault(rest)
	case "claire":
		code = cmd.RunClaire(rest)
	case "promptoverse":
		code = cmd.RunPromptOVerse(rest)
	case "backup":
		if len(rest) > 0 && rest[0] == "decrypt" {
			code = cmd.RunBackupDecrypt(rest[1:])
		} else {
			code = cmd.RunBackup(rest)
		}
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
	case "backlog":
		fmt.Print(`emily backlog — manage the golden backlog

Subcommands:
  emily backlog curate [flags]   — auto-curate FatBaby observations into BACKLOG.md

curate flags:
  --all           Process all uncurated observations (not just most recent --limit)
  --limit N       Max observations per pass (default 10)
  --dry-run       Show what would be added; write nothing
  --no-commit     Write BACKLOG.md but skip git commit
  --no-apple      Skip IDUNA Apple receipt
  --json          Output JSON summary

State: EMILY/var/backlog-curated.txt — one timestamp per line, tracks curated set.
Idempotent: running twice on the same obs set is a no-op.

Examples:
  emily backlog curate                    # curate up to 10 most recent uncurated obs
  emily backlog curate --all              # curate all uncurated obs (up to limit)
  emily backlog curate --limit 1          # curate only the most recent uncurated obs
  emily backlog curate --dry-run --all    # preview without writing
`)
	case "chat":
		fmt.Print(`emily chat — terminal chat interface for Emily Prime (FatBaby mode)

Calls claude-haiku directly. No port, no server. API key from emily key set or ANTHROPIC_API_KEY env.

Usage:
  emily chat [--model MODEL] [--session FILE]

Flags:
  --model    Anthropic model (default: claude-haiku-4-5-20251001)
  --session  JSON file to persist/restore conversation history across sessions

Controls:
  exit / quit / q    end session
  clear              clear screen
  history            show turn count
  \\ at end of line   multi-line input continuation
  Ctrl+C             force exit

Loads EMILY/context/full-system-context.md as system context when present (emily context build).
`)
	case "gpt2":
		fmt.Print(`emily gpt2 — manage the Emily Prime GPT-2 inference stack

Subcommands:
  emily gpt2 start     [--port N] [--model ft|base] [--dry-run]
        Start the Python inference server (scripts/serve.py) on :8088.
  emily gpt2 proxy     [--port N] [--routes path] [--dry-run]
        Start the FatBaby broker proxy on :8679. Bearer: emily-gpt2-local.
  emily gpt2 generate  [--prompt "..."] [--max-tokens N] [--via server|emily|proxy]
        Call the GPT-2 endpoint and print generated text.
  emily gpt2 health
        HTTP health check of :8088, emily-agent :8086, and proxy :8679.
  emily gpt2 status
        Show whether serve.py and the broker proxy are running.
  emily gpt2 tokenizer [--dry-run]
        Build weights/tokenizer.bin for gpt2_run --prompt mode.

Env:
  GPT2_ROOT   path to gpt2-alpine-c (default: sibling of EMILY_ROOT)
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
	case "gsync":
		fmt.Print(`emily gsync — sync git repo(s) to Google Drive

Archives each repo with git archive and uploads the .tar.gz to the
Google Drive folder configured in IDUNA (GOOGLE_DRIVE_FOLDER_ID).

Usage:
  emily gsync SHANKPIT
  emily gsync SHANKPIT EMILY GoblinFoxDragon
  emily gsync --dry-run SHANKPIT

If a repo is not at /home/fatbaby/REPO it is cloned from
  git@github.com:emilyspringerton/REPO  (--org to override)

Archive name: REPO-YYYYMMDD-HHmmss.tar.gz
Upload target: Google Drive folder set by GOOGLE_DRIVE_FOLDER_ID on IDUNA

Requires IDUNA running with:
  GOOGLE_DRIVE_SERVICE_ACCOUNT_JSON  (service account key JSON)
  GOOGLE_DRIVE_FOLDER_ID             (target folder; MyDrive root if empty)

Flags:
  --dry-run   archive but do not upload
  --org STR   GitHub org for clone URLs (default: emilyspringerton)

Env:
  IDUNA_BASE_URL    IDUNA server (default http://localhost:8080)
  IDUNA_AGENT_NAME  agent identity
  IDUNA_AGENT_SECRET M2M credential
`)
	case "shankpit":
		fmt.Print(`emily shankpit — SHANKPIT game server admin + observability

Subcommands:
  status              current server state (players, scenes, uptime)
  players             list all connected players with position
  kick <id>           disconnect player by client ID
  observe             file an Emily observation from server state
  restart             graceful server restart (SIGTERM + relaunch)

Flags:
  --admin-url   admin HTTP base URL (default http://localhost:6970)
                overridden by SHANKPIT_ADMIN_URL env var
  --token       Bearer token for write ops
                overridden by SHANKPIT_ADMIN_TOKEN env var

The admin server runs on :6970 alongside the UDP game server on :6969.
Start server: ./server-go --admin-port 6970 [--admin-token mytoken]
`)
	case "redgarden":
		fmt.Print(`emily redgarden — REDGARDEN persistent bot-pool control

Subcommands:
  bots [N]   set the live bot-pool size to N (default 20 when omitted) and
             restart redgarden-bot-pool.service to apply it
  status     show the current live bot count + service state

N==20 is fully self-sustaining (matches start immediately, continuous data)
but leaves no human slot open at the bot-pool matchmaker (:7778). N<20 opens
(20-N) human slots at :7778, but no match starts there until they fill. The
player-only pool at :7779 always stays open for humans either way.
`)
	case "survival":
		fmt.Print(`emily survival — EINHORN_SURVIVAL (Paper Minecraft) server ops

Subcommands:
  logs [-n N] [-f=false]   tail server.log (default: last 40 lines, follow)
  status                   systemd --user unit state for einhorn-survival.service
  restart                  systemctl --user restart einhorn-survival.service

Env:
  SURVIVAL_ROOT   repo root (default /home/fatbaby/EINHORN_SURVIVAL)

Examples:
  emily survival logs             # tail -f server.log
  emily survival logs -f=false    # print last 40 lines and exit
  emily survival logs -n 200      # follow, starting 200 lines back
  emily survival status
  emily survival restart
`)
	case "emilyos":
		fmt.Print(`emily emilyos — EmilyOS policy kernel interface

Subcommands:
  posture get                 print current posture state
  posture set <STATE>         transition posture (NORMAL/SIEGE/MERCY/INCIDENT/GAME/EXITED)
  verb <verb> <object>        dispatch verb with capability check
  audit tail [-n N]           tail recent audit events
  audit verify                verify HMAC chain integrity (exit 3 if tampered)
  audit export <outdir>       SOC 2 evidence export (audit.jsonl + manifest.json)

Env vars used (passed through):
  EMILY_POSTURE_PATH    default: var/posture.json
  EMILY_AUDIT_PATH      default: var/audit.jsonl
  EMILY_ACTOR_ID        actor for audit attribution
  EMILY_ROLE            role for capability check (operator|admin|auditor)

Exit codes: 0=ok, 1=error, 2=policy deny, 3=chain tampered
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

  emily backlog curate [--all] [--limit N] [--dry-run]
        Auto-curate FatBaby observations into EMILY/BACKLOG.md INTAKE QUEUE.
        State-tracked (EMILY/var/backlog-curated.txt) — idempotent.
        Commits BACKLOG.md and posts a curation Apple to IDUNA.

  emily chat [--model MODEL] [--session FILE]
        Terminal chat with Emily Prime (haiku). No port needed — calls Anthropic directly.
        Requires: emily key set <api-key>. Loads full-system-context.md when present.

  emily gpt2 start [--model ft|base] [--port N]
        Start the GPT-2 inference server (serve.py) on :8088.
  emily gpt2 proxy [--port N]
        Start the FatBaby broker proxy on :8679 (bearer: emily-gpt2-local).
  emily gpt2 status
        Check whether inference server and proxy are running.

  emily session new
        Generate a session fingerprint (Emiree Mandelbrot fractal + squish/tower/gematria +
        Dallas moon phase, hashed to a short legible tag). Auto-used by apples post/changelog
        add to tag their output. Logs to EMILY/var/sessions.ndjson.
  emily session current
        Print the active session's tag.

  emily gsync REPO [REPO ...]
        Clone (if needed) + git archive → upload to Google Drive via IDUNA.
        e.g. emily gsync SHANKPIT  or  emily gsync SHANKPIT EMILY GoblinFoxDragon
        Uses git@github.com:emilyspringerton/<REPO> as clone URL.
        Requires IDUNA running with GOOGLE_DRIVE_SERVICE_ACCOUNT_JSON configured.
        --dry-run: archive but skip upload.

  emily shankpit status
        SHANKPIT game server status: players online, scenes, uptime.
  emily shankpit players
        List connected players with position and scene.
  emily shankpit kick <id>
        Disconnect a player by client ID (requires --token or SHANKPIT_ADMIN_TOKEN).
  emily shankpit observe
        File an Emily observation from current server state.
  emily shankpit restart
        SIGTERM + relaunch SHANKPIT server via emily start --shankpit.

  emily iduna create-account <character-name> [--job WAR] [--email E] [--password P]
        Mint a real DragonsNShit test account (player + credential + character, atomic).
        Prints email/password to log in with at battlegrounds_gui or apps2/mud's login screen.

  emily redgarden bots [N]
        Set the live REDGARDEN persistent bot-pool size (default 20) and restart it.
        N==20 is self-sustaining but leaves no human slot at :7778; N<20 opens (20-N) slots.
  emily redgarden status
        Show the current live bot count and bot-pool/matchmaker service state.

  emily survival logs [-n N] [-f=false]
        Tail EINHORN_SURVIVAL's server.log (default: last 40 lines, follow).
  emily survival status
        systemd --user unit state for einhorn-survival.service.
  emily survival restart
        Restart the live Paper server via systemctl --user.

  emily key set <api-key>
  emily key show
  emily key unset
        Manage ANTHROPIC_API_KEY in EMILY/var/emily-secrets.env.
        Set once; loaded automatically by all emily commands that call Anthropic APIs.

  emily emilyos posture get|set <STATE>
  emily emilyos audit tail|verify|export <outdir>
  emily emilyos verb <verb> <object>
        EmilyOS policy kernel interface (wraps emilyos binary).
        'emily help emilyos' for full reference.

  emily vault init
        One-time IDUNA Vault setup — prompts for a new master passphrase.
  emily vault unlock
        Unlock the vault after an IDUNA restart — prompts for the passphrase.
  emily vault lock
        Discard the in-memory vault key immediately.
  emily vault status
        Show initialized/locked state.
  emily vault add -type <login|note|api_key|totp|document> -name <name> [-field k=v ...]
        Add an item. e.g. -type login -name "AWS Root" -field username=root -field password=hunter2
  emily vault list
        List items (id, type, name — not secrets).
  emily vault get <id>
        Show one item fully decrypted.
  emily vault delete <id>
        Delete an item.
        IDUNA Vault runs loopback-only — this command must run on the IDUNA host.

Environment:
  IDUNA_BASE_URL      IDUNA server (default: http://localhost:8080)
  IDUNA_AGENT_NAME    Agent name (default: EMILY-PRIME)
  IDUNA_AGENT_SECRET  M2M secret (auto-read from IDUNA/var/agent-secrets.env)
  ANTHROPIC_API_KEY   Anthropic key (auto-read from EMILY/var/emily-secrets.env)
  FATBABY_ROOT        PRRJECT_FATBABY root (default: /home/fatbaby/PRRJECT_FATBABY)

Exit codes: 0=ok 1=usage 2=auth 3=write-failure 4=api-error
`)
}
