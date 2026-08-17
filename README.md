# emily — Emily Lite CLI

Operator terminal for the Einhorn Industrial agent system. Zero LLM calls, -zero external
dependencies-. Every command is a direct HTTP call to IDUNA or a local file write.

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

### Start — launch the Emily OS agent stack

```bash
emily start                # observation-watcher + emily-agent RSI daemon, detached
emily start --iduna        # also starts iduna.service via systemctl --user
emily start --dry-run      # show what would be started
```

### TUI — full-terminal dashboard

```bash
emily tui               # 3-column Bloomberg-style dashboard: roadmap | tasks | health
emily tui --fatbaby     # pre-activate the FatBaby panel in column 3 ('b' toggles at runtime)
```

### Backlog — curate observations into BACKLOG.md

```bash
emily backlog promote            # curate uncurated FatBaby observations into the INTAKE QUEUE
emily backlog curate --all       # pull everything uncurated
emily backlog add <section> <item>       # append an item to a section
emily backlog add-section <title>        # open a new SECTION
emily backlog done <item-id>             # mark an item [x]
emily backlog archive / compress         # DONE.md housekeeping
```

### Changelog — per-repo CHANGELOG.md entries

```bash
emily changelog add <repo> "<what changed>"
```

### Session — cross-context session fingerprint

```bash
emily session new        # mint a fresh sess-YYYYMMDD-HHMM-<8hex> tag
emily session current    # print the active tag
```

Auto-stamped as `run_id`/`session:` on every `apples post`, `changelog add`, and `observe` call.

### Key — store credentials in the CLI's env

```bash
emily key set GITHUB_TOKEN <token>          # target: this box's default env file
emily key set NAME VALUE --target iduna     # or --target emily
emily key set NAME VALUE --file <path>      # explicit file override
emily key show NAME
emily key unset NAME
```

### Prompt-o-verse — generate + publish gallery nodes

```bash
emily promptoverse add <subject> <count>   # queue <count> styles applied to <subject>, then drain
emily promptoverse work                    # drain whatever's already queued (resume after a 429)
emily promptoverse queue                   # list pending queue entries, oldest first
emily promptoverse styles                  # list the reusable style registry
```

Requests are queued FIFO to a durable file (`EMILY/var/promptoverse-queue.jsonl`), not fired
immediately — `add` enqueues then drains; if a drain is already mid-flight or queued, new
requests wait their turn in arrival order. Draining stops (not retries) on a rate limit, leaving
the remainder queued for `emily promptoverse work` later. Requires `gcloud` ADC authenticated on
this box and `IDUNA_AGENT_SECRET` for an agent with `promptoverse.write`.

`add` deduplicates: it skips any style already published *or* already queued for that exact
subject, and picks from what's left by ascending global usage (least-used styles across the whole
gallery first), so repeated runs don't just keep re-rolling whichever style sits first in the
registry. If every registry style is already used for a subject, it reports that and queues
nothing.

### Context / Northstar — golden-doc tooling

```bash
emily context build              # compile all Tier 1 golden docs → EMILY/context/full-system-context.md
emily northstar <repo>           # print <repo>/docs/NORTHSTAR.md (or docs2/NORTHSTAR.md)
```

### Chat — terminal chat with Emily Prime

```bash
emily chat                       # calls claude-haiku directly, no server
emily chat --model <model> --session <file>
```

### GPT-2 — Emily Prime inference stack

```bash
emily gpt2 start [--port N] [--model ft|base] [--dry-run]
emily gpt2 proxy
emily gpt2 status
emily gpt2 tokenizer
emily gpt2 generate "<prompt>"    # alias: gen
emily gpt2 health
```

### Train — GPT-2 fine-tuning pipeline

```bash
emily train build-dataset [--emily-root <path>] [--output <path>] [--mode lm|instruct]
emily train upload <file> [<file>...]
emily train status
emily train stats
emily train run-local
```

### Vault — founder-only password manager (loopback-only)

```bash
emily vault init
emily vault unlock / lock / status
emily vault add <name> / get <name> / list / delete <name>
```

### Memory — Emily Prime's observation digest

```bash
emily memory digest         # print the obs-digest from emily-memory/ in TUI format
emily memory consolidate
```

### Claire — uncompressed subconscious log

```bash
emily claire "<entry>"      # append to CLAIRE.md — tech debt, failed approaches, env quirks
```

### Saga — documentation curation lifecycle (HQ-SPEC-DOC-102)

```bash
emily saga lint                    # frontmatter schema checks
emily saga gaps
emily saga which-doc-governs <path>
emily saga status
emily saga conflicts
```

### IDUNA — account tooling

```bash
emily iduna create-account          # mint a disposable DragonsNShit test account
```

### EmilyOS / Shankpit / Survival / Redgarden / Gsync — per-repo ops helpers

```bash
emily emilyos                          # EmilyOS policy kernel helper
emily shankpit status|players|kick|observe|restart|leaderboard
emily survival logs|status|restart     # EINHORN_SURVIVAL Minecraft server
emily redgarden bots|status            # REDGARDEN bot ops
emily gsync                            # Google Drive / gsync helper
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
emily help <any other command above>
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
./scripts/build.sh              # build + tests + install
./scripts/build.sh --no-install # CI mode
go test ./...                   # unit tests only
EMILY_COLOR=1 go test ./internal/color/...  # color-mode tests
```

Tests: 109 across 5 packages (`cmd`, `internal/config`, `internal/iduna`, `internal/obs`, `internal/color`).

---

*docs/NORTHSTAR.md — the philosophy*
*The terminal is how you talk to the system when the system can't talk to you yet.*
