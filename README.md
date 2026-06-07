# emily — Emily Lite CLI

Operator terminal for the Einhorn Industrial agent system. Zero LLM calls, zero external
dependencies. Every command is a direct HTTP call to IDUNA or a local file write.

**Emily Lite**: super-tokenized, pipe-safe, stdin-aware, color-optional.

---

## Install

```bash
cd /home/fatbaby/emily.cli
./scripts/build.sh        # build + 69 tests + install to ~/.local/bin/emily
```

Or manually: `go build -o ~/.local/bin/emily .`

---

## Commands

### Observe — fire an observation into the FatBaby pipeline

```bash
emily observe "eps-processor dropped to 0 articles/hour"
emily observe -s error "jon-agent 504s" --fix "check redis connection pool"
emily observe -s warn "latency spike" --findings "p99 > 2s since 06:00"
git log -1 --oneline | emily observe -s info          # stdin input
emily observe --dry-run "preview without writing"
```

The observation-watcher picks it up within ~10s and invokes Claude Code on FatBaby.
Auto-posts a `signal_observation` Apple to IDUNA as a receipt (requires `IDUNA_AGENT_SECRET`).

### Apples — query and post to the IDUNA log

```bash
emily apples list                        # last 20 apples
emily apples list TYLER -n 5            # last 5 from TYLER repo
emily apples list -t rsi_iteration --full  # with body text
emily apples get 44                     # full body of Apple #44
emily apples post -t backlog_completion "deployed IDUNA unit"
cat report.txt | emily apples post -t rsi_iteration "Build 0020"
```

### Watch — IDUNA tail -f

```bash
emily watch               # all repos, 5s poll
emily watch TYLER         # filter by repo
emily watch --interval 2  # 2s poll
```

Bootstraps at current highest Apple ID — only new apples are shown. Ctrl-C to stop.

### Status — cross-repo system snapshot

```bash
emily status              # one-shot: git + IDUNA
emily status --json       # machine-readable JSON
emily status --watch      # live dashboard, refreshes every 30s
emily status --watch --interval 10 --no-git
```

### Sync — FatBaby observations → IDUNA

```bash
emily sync                          # sync up to 10 new obs files
emily sync --all --dry-run         # preview everything
emily sync --watch                 # daemon: auto-post as obs files appear
emily sync --watch --interval 5
```

State-tracked in `EMILY/var/fatbaby-synced.txt` to avoid double-posting.

### Agents — agent activity dashboard

```bash
emily agents              # all agents, last 200 apples scanned
emily agents --since 60   # only agents active in last hour
emily agents --json
```

Synthesized from the Apples log — shows last-seen age, total count, last apple type/title.

### Prime Task — direct Claude on FatBaby from the CLI

```bash
emily prime-task "add test for eps-processor Q1 edge case"
emily prime-task --priority high --type improve_signal \
  --criteria "go test ./... passes" --criteria "committed to git" \
  "entity-graph parser misses director names with Jr. suffix"
emily prime-task --dry-run "preview without writing"
```

Writes a task JSON to `EMILY/signals/tasks/`. The observation-watcher picks it up within
10s and invokes Claude Code on the FatBaby repo with the task as its prompt. Closes the
`operator → CLI → EMILY/signals/tasks → obs-watcher → Claude on FatBaby` loop.

### Install — wire up daemons

```bash
emily install --cron              # print recommended crontab entries
emily install --cron --write      # install into crontab
emily install --systemd           # generate systemd user unit
emily install --systemd --write   # install to ~/.config/systemd/user/
```

### Help

```bash
emily help observe
emily help apples
emily help watch
emily help status
emily help sync
emily help agents
emily help prime-task
```

---

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `IDUNA_BASE_URL` | `http://localhost:8080` | IDUNA server |
| `IDUNA_AGENT_NAME` | `EMILY-PRIME` | Agent name for auth |
| `IDUNA_AGENT_SECRET` | — | M2M secret (auto-read from `IDUNA/var/agent-secrets.env`) |
| `FATBABY_ROOT` | `/home/fatbaby/PRRJECT_FATBABY` | Observation pipeline root |
| `EMILY_ROOT` | `/home/fatbaby/EMILY` | EMILY repo root (for prime-task, sync state) |
| `EMILY_COLOR` | — | Set to `1` for ANSI color output |

---

## Architecture

Three feedback loops:

```
Loop A (agent → claude):
  emily-agent writes obs file → observation-watcher → Claude Code fixes FatBaby

Loop B (operator → claude, via observe):
  emily observe "msg" → obs file → watcher → Claude Code

Loop C (operator → claude, via prime-task):
  emily prime-task "task" → EMILY/signals/tasks/ → watcher → Claude Code on FatBaby
```

Full docs: [`docs/NORTHSTAR.md`](docs/NORTHSTAR.md) · [`docs/COMMANDS.md`](docs/COMMANDS.md) · [`docs/DESIGN.md`](docs/DESIGN.md)

---

## Development

```bash
./scripts/build.sh              # build + 69 tests + install
./scripts/build.sh --no-install # CI mode
go test ./...                   # unit tests only
EMILY_COLOR=1 go test ./internal/color/...  # color-mode tests
```

Tests: 69 across 5 packages (`cmd`, `internal/config`, `internal/iduna`, `internal/obs`, `internal/color`).

---

*docs/NORTHSTAR.md — the philosophy*
*The terminal is how you talk to the system when the system can't talk to you yet.*
