# Emily CLI — Command Reference
## Full Specification for `emily` Binary

*Last updated: 2026-06-07*

---

## Global Flags

```
emily [--json] [--quiet] [--base-url URL] <command> [args]

  --json            Output JSON instead of human-readable text
  --quiet           Suppress all non-essential output (good for cron)
  --base-url URL    Override IDUNA_BASE_URL
  --fatbaby DIR     Override FATBABY_ROOT path
```

---

## emily observe

Post a human (or script) observation into the FatBaby observation pipeline.

```
emily observe [flags] <message>

  -s, --severity string   Observation severity: info|warn|error (default: info)
  -f, --fix string        Suggested fix (optional)
  --findings string       Detailed findings (longer form; message becomes summary)
  --dry-run               Print the JSON that would be written, don't write it
```

### Behavior

1. Resolves `FATBABY_ROOT` (env or default `/home/fatbaby/PRRJECT_FATBABY`)
2. Generates timestamp-named file: `var/emily-observations/<RFC3339>.json`
3. Writes JSON with fields: `timestamp`, `summary`, `severity`, `findings`, `suggested_fix`
4. Atomically symlinks `var/emily-observations/latest.json` to the new file
5. Prints confirmation and observation path

The observation-watcher (`go run ./cmd/observation-watcher` in PRRJECT_FATBABY) polls `latest.json` every 10s. When it sees a new observation (by timestamp hash), it invokes Claude Code with the observation as context.

### Examples

```bash
# Quick one-liner
emily observe "jon-agent is returning 504s on /setups endpoint"

# With severity and fix hint
emily observe -s error "eps-processor dropped to 0 articles/hour" \
  --fix "check ticker map size — was 2 entries last time"

# Detailed findings
emily observe -s warn "observation-watcher trigger latency" \
  --findings "Watcher fires within 10s of latest.json update, but claude invocation
adds 30-45s before the first file edit. Total loop latency: ~60s per observation.
Acceptable for async ops, too slow for real-time alerts." \
  --fix "Add immediate-mode flag to observation-watcher: --realtime invokes claude
  synchronously instead of polling"

# Dry run (see the JSON)
emily observe --dry-run "testing the pipe"
```

### Output (default)

```
✓ observation written
  path:     /home/fatbaby/PRRJECT_FATBABY/var/emily-observations/2026-06-07T02:40:00Z.json
  severity: error
  summary:  eps-processor dropped to 0 articles/hour
  watcher:  picks up in ~10s → invokes claude
```

### Output (--json)

```json
{
  "written": true,
  "path": "/home/fatbaby/PRRJECT_FATBABY/var/emily-observations/2026-06-07T02:40:00Z.json",
  "timestamp": "2026-06-07T02:40:00Z",
  "severity": "error",
  "summary": "eps-processor dropped to 0 articles/hour"
}
```

---

## emily apples

Interact with the IDUNA Apples log (golden documentation records).

### emily apples list

```
emily apples list [flags] [filter]

  -n, --limit int       Max apples to show (default 20)
  -t, --type string     Filter by apple_type (e.g. signal_observation, rsi_iteration)
  -r, --repo string     Filter by source_repo (e.g. TYLER, EMILY, PRRJECT_FATBABY)
  --full                Show body text (first 5 lines)
  --since string        Only apples after this timestamp (RFC3339)
  [filter]              Positional: same as --repo filter (convenience form)
```

#### Examples

```bash
emily apples list                        # last 20 apples
emily apples list TYLER                  # last 20 from TYLER repo
emily apples list -t signal_observation  # last 20 FatBaby signal observations
emily apples list --full EMILY           # last 20 from EMILY with body text
emily apples list -n 5 --json           # last 5, JSON output
```

#### Output (default)

```
◈ EMILY OS — APPLES | 2026-06-07 02:40

  #  36  2026-06-07 02:38  [EMILY       ]  session_state    Emily Prime session complete — all repos syn
  #  18  2026-06-07 02:35  [TYLER       ]  backlog_completi  TYLER/engine/tyler_hum_mechanic.md — First T
  #  16  2026-06-07 02:32  [TYLER       ]  rsi_iteration    Tyler Build 0018 — 1952 Detroit Annual Compo

  Total: 20 apple(s)
```

### emily apples post

```
emily apples post [flags] <title> [body]

  -t, --type string     apple_type (required; e.g. signal_observation, backlog_completion)
  -r, --repo string     source_repo (default: CLI)
  --run-id string       run_id tag (default: cli-<timestamp>)
  [body]                Apple body text. Reads from stdin if omitted and stdin is a pipe.
```

#### Examples

```bash
# Quick post
emily apples post -t signal_observation "redis heap is growing" "16 GB and climbing since 06:00"

# Pipe body from file or command
cat report.txt | emily apples post -t rsi_iteration "Emily cycle 47 complete"

# With repo tag
emily apples post -t backlog_completion -r EMILY "deployed systemd unit for IDUNA"
```

#### Output

```
✓ Apple #37 filed
  type:  signal_observation
  title: redis heap is growing
  id:    37
```

---

## emily status

Cross-repo system status: git state + last Apple per agent.

```
emily status [flags]

  --no-git      Skip git checks (faster if in a slow shell or remote context)
  --no-iduna    Skip IDUNA Apple query
```

### Output

Same as `EMILY/scripts/status.sh` — one screen, actionable:

```
◈ EMILY OS — SYSTEM STATUS | 2026-06-07 02:40
══════════════════════════════════════════════════

  GIT REPOS
  ─────────
  TYLER         main      eae89e5 Build 0019 — Hum mechanic spec       59 done / 3 pending
  EMILY         main      30463dc observability: backfill all 26...     11 done / 8 pending
  IDUNA         main      8540776 chore: gitignore compiled bootstrap
  PRRJECT_FATBABY main    1a1a7cf chore: gitignore .claude/
  SHANKPIT      master    a0b686a chore: remove tracked compiled binar

  IDUNA: http://localhost:8080 ✓ — 36 apples total

  LAST APPLE PER REPO
  ───────────────────
  EMILY          #36  02:38  session_state     Emily Prime session complete
  PRRJECT_FATBABY #35  02:38  signal_observation  FatBaby obs [error]: eps-processor...
  TYLER          #18  02:35  backlog_completion  TYLER/engine/tyler_hum_mechanic.md
```

---

## emily sync

Sync FatBaby observations to IDUNA as Apples (wraps `EMILY/scripts/sync-fatbaby.sh`).

```
emily sync [flags]

  --all         Backfill all observations (not just new ones)
  --dry-run     Show what would be posted
  --limit int   Max observations to process (default 10)
```

### Behavior

Reads `PRRJECT_FATBABY/var/emily-observations/*.json`, posts unsynced ones to IDUNA as `signal_observation` Apples. State tracked in `EMILY/var/fatbaby-synced.txt`.

---

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `IDUNA_BASE_URL` | `http://localhost:8080` | IDUNA server URL |
| `IDUNA_AGENT_NAME` | `EMILY-PRIME` | Agent name for auth |
| `IDUNA_AGENT_SECRET` | — | Agent M2M secret |
| `FATBABY_ROOT` | `/home/fatbaby/PRRJECT_FATBABY` | PRRJECT_FATBABY root |
| `EMILY_ROOT` | `/home/fatbaby/EMILY` | EMILY repo root |
| `IDUNA_SECRETS` | `/home/fatbaby/IDUNA/var/agent-secrets.env` | Auto-sourced secrets |

If `IDUNA_AGENT_SECRET` is missing, Emily CLI auto-reads `IDUNA_SECRETS` (as a Go file parser, not shell `source`) and extracts the relevant secret for the configured `IDUNA_AGENT_NAME`.

---

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Usage error (bad flags, missing args) |
| 2 | Auth failure (IDUNA unreachable or credentials wrong) |
| 3 | Observation write failure (FATBABY_ROOT missing or not writable) |
| 4 | IDUNA API error (non-auth) |

---

## Installation

```bash
cd /home/fatbaby/emily.cli
go build -o ~/.local/bin/emily .
```

After that: `emily observe "IDUNA is live"` from anywhere.

---

*Emily CLI Command Reference | emily observe → emily apples list → emily status*
*The commands are the protocol. The protocol is the system.*
