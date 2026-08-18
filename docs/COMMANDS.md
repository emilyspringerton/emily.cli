# Emily CLI — Command Reference
## Full Specification for `emily` Binary

*Last updated: 2026-08-18 — v1.0.0 (added `emily promptoverse` in full)*

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
echo "message" | emily observe [flags]

  -s, --severity string   Observation severity: info|warn|error (default: info)
  -f, --fix string        Suggested fix (optional)
  --findings string       Detailed findings (longer form; message becomes summary)
  --dry-run               Print the JSON that would be written, don't write it
  --no-apple              Skip IDUNA Apple receipt (observation file still written)
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

# Pipe from another command (stdin mode — v0.3.0)
git log -1 --oneline | emily observe -s info
echo "IDUNA health check failed" | emily observe -s error --fix "restart iduna.service"

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

## emily watch

Tail IDUNA Apples log in real-time. Bootstraps at the current highest Apple ID and
prints new Apples as they arrive. Like `tail -f` for the agent activity log.

```
emily watch [flags] [repo]

  -i, --interval int    Poll interval in seconds (default 5)
  -r, --repo string     Filter by source_repo
  -t, --type string     Filter by apple_type
  --quiet               Suppress header and poll messages (good for scripting)
  [repo]                Positional: same as --repo filter
```

### Behavior

1. Fetches current Apples to determine the latest ID (bootstrap).
2. Polls IDUNA every `--interval` seconds.
3. Prints any Apple with ID > last seen ID, oldest first.
4. Exits cleanly on Ctrl-C (SIGINT/SIGTERM).

The bootstrap step means `emily watch` never re-prints old Apples — you only see
activity that happened after the command started.

### Examples

```bash
emily watch                    # watch all repos, 5s poll
emily watch TYLER              # only TYLER apples
emily watch --interval 2       # 2s poll for lower latency
emily watch -t rsi_iteration   # only RSI iteration apples
emily watch --quiet TYLER      # no header, machine-readable output stream
```

### Output

```
◈ EMILY OS — WATCH | http://localhost:8080 | poll every 5s
  filter: repo=TYLER
  ctrl-c to stop

  ─────────────────────────────────────────────────────────────
  bootstrapped at Apple #39 — watching for new apples...

  +#  40  2026-06-07 08:12:03  [TYLER       ]  rsi_iteration         Tyler Build 0019 — Eastwind Shard reopen
  +#  41  2026-06-07 08:15:47  [CLI         ]  backlog_completion     emily watch + observe stdin — v0.3.0
```

---

## emily status

Cross-repo system status: git state + last Apple per agent.

```
emily status [flags]

  --no-git           Skip git checks (faster if in a slow shell or remote context)
  --no-iduna         Skip IDUNA Apple query
  --watch            Live-updating dashboard — clears terminal, reprints every --interval seconds
  --interval int     Refresh interval for --watch (default 30)
  --json             Output JSON (one-shot only; not compatible with --watch)
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

Sync FatBaby observations to IDUNA as Apples.

```
emily sync [flags]

  --all              Backfill all observations (not just new ones)
  --dry-run          Show what would be posted
  --limit int        Max observations to process per pass (default 10)
  --watch            Daemon mode — poll obsDir every --interval seconds until Ctrl-C
  --interval int     Poll interval for --watch (default 10)
  --json             Output one JSON line per Apple posted
```

### Behavior

Reads `PRRJECT_FATBABY/var/emily-observations/*.json`, posts unsynced ones to IDUNA as
`signal_observation` Apples. State tracked in `EMILY/var/fatbaby-synced.txt`.

With `--watch`: runs `syncPass` on startup, then polls every `--interval` seconds. Prints
a timestamped line when new observations are posted. Ctrl-C exits cleanly.

### Examples

```bash
emily sync                      # sync up to 10 new observations
emily sync --all --dry-run      # preview all backlogged observations
emily sync --watch              # daemon: auto-post as obs files appear
emily sync --watch --interval 5 # 5s poll for lower latency
```

---

## emily install

Print (and optionally install) recommended crontab entries and systemd user units.

```
emily install [--cron|--systemd] [--write]

  --cron      Print recommended crontab entries
  --systemd   Generate a systemd user unit for `emily sync --watch --quiet`
  --write     Install (crontab entries or systemd unit file)
```

### --cron

Prints two entries:

```cron
*/10 * * * * /home/fatbaby/.local/bin/emily sync --quiet 2>/dev/null
0 */4 * * * /home/fatbaby/TYLER/scripts/cron-emily.sh 2>/dev/null
```

The first syncs new FatBaby observations to IDUNA every 10 minutes.
The second runs Tyler's RSI loop every 4 hours.

`--write` installs idempotently — skips entries already in the crontab.

### --systemd

Generates a systemd user unit (`~/.config/systemd/user/emily-sync.service`) that runs
`emily sync --watch --quiet` as a persistent daemon. Restarts on failure with 30s delay.

`--write` creates the file. After writing:

```bash
systemctl --user daemon-reload
systemctl --user enable --now emily-sync.service
systemctl --user status emily-sync.service
```

### Examples

```bash
emily install --cron                    # print crontab entries
emily install --cron --write            # install into crontab (idempotent)
emily install --systemd                 # print unit file + next steps
emily install --systemd --write         # write to ~/.config/systemd/user/
```

---

## emily agents

Agent activity dashboard — synthesized from the IDUNA Apples log.

```
emily agents [flags]

  -n int        Apples to scan (default 200; higher = more complete history)
  --since int   Only show agents active in the last N minutes
  --json        Output JSON array
```

### Behavior

Fetches the last `-n` Apples, groups by `source_repo`, and shows each agent's:
- **LAST SEEN** — how long ago the last Apple was posted (just now / Nm ago / Nh ago)
- **TOTAL** — Apple count within the scanned window
- **LAST TYPE** — `apple_type` of the most recent Apple
- **LAST TITLE** — title of the most recent Apple

Sorted newest-active first. Note: reflects Apple activity, not a heartbeat registration.
Agents that have never posted an Apple won't appear.

### Examples

```bash
emily agents              # all agents, scan last 200 apples
emily agents --since 60   # only agents active in the last hour
emily agents --json       # JSON array for scripts
```

### Output

```
◈ EMILY OS — AGENTS | 2026-06-07 09:16

  REPO                LAST SEEN     TOTAL  LAST TYPE             LAST TITLE
  ──────────────────────────────────────────────────────────────────────────────
  CLI                 1m ago            8  signal_observation    [INFO] emily.cli v0.5.0 RSI loop closed
  EMILY               6h ago            6  session_state         Emily Prime session complete
  PRRJECT_FATBABY     6h ago           26  signal_observation    FatBaby obs [error]: eps-processor…
  TYLER               6h ago            3  backlog_completion    TYLER/engine/tyler_hum_mechanic.md
```

---

## emily prime-task

Write a directed task to `EMILY/signals/tasks/` for the observation-watcher's prime task poller.

```
emily prime-task [flags] <description>

  --type string       task_type field (default: operator_directive)
  --priority string   low|normal|high|critical (default: normal)
  --context string    strategic context for Claude (optional)
  --criteria string   acceptance criterion (repeatable)
  --deadline string   optional deadline (free text)
  --dry-run           print task JSON without writing it
  --no-apple          skip IDUNA Apple receipt
  --json              output JSON confirmation
```

### Behavior

Writes a JSON task file to `EMILY/signals/tasks/<timestamp>-<id>.json`. The observation-watcher
(in PRRJECT_FATBABY) polls this directory every 10 seconds and invokes Claude Code on the
FatBaby repo when it finds a new task file.

This closes the **operator → CLI → EMILY/signals/tasks → obs-watcher → Claude on FatBaby**
directed loop without requiring Emily Prime LLM invocation.

Auto-posts a `prime_task` Apple to IDUNA as a receipt.

### Examples

```bash
# Quick directive
emily prime-task "add test for eps-processor edge case in Q1 earnings"

# With acceptance criteria and priority
emily prime-task --priority high --type improve_signal \
  --criteria "go test ./... passes" --criteria "committed to git" \
  "entity-graph parser misses director names with Jr. suffix"

# Dry run to preview the task JSON
emily prime-task --dry-run "preview without writing"

# Multiple positional words are joined into the description
emily prime-task improve the entity graph parser for suffix variants
```

---

## emily promptoverse

Generate and publish nodes to the Prompt-o-verse gallery on okemily.com (real images via Vertex
AI's `gemini-2.5-flash-image`, published through IDUNA's `promptoverse.write` API). Requires
`gcloud` ADC already authenticated on this box, `IDUNA_AGENT_SECRET` for an agent with
`promptoverse.write` (EMILY-PRIME has it), and `iduna.service` running.

Requests are queued FIFO to a durable file (`EMILY_ROOT/var/promptoverse-queue.jsonl`), not fired
immediately — `add` enqueues then drains; `work` just drains whatever's already queued (e.g. to
resume after a rate limit without enqueueing anything new). Draining stops (doesn't retry forever)
on a 429, leaving the remainder queued.

### emily promptoverse add

```
emily promptoverse add [<subject>] <count> [--force] [--slow] [--tag <style>]...
                        [--annotation "text" | --annotation-from-lore] [--annotation-alias NAME]

  <subject>                 What to generate (omit to auto-pick, weighted across every subject
                             ever published/discovered, occasionally proposing a brand new one)
  <count>                   How many styles to generate for this subject
  --force                   Skip adaptive backoff wait for this run (bookkeeping still happens)
  --slow                    Double every wait this command applies (base delay, growth, backoff)
  --tag <style>              Force a specific style into this batch (created via Vertex AI if not
                             already a known style). Repeatable — see "Style hybrids" below.
  --annotation "text"        Set/overwrite this subject's default prompt annotation (see
                             "Subject annotations" below)
  --annotation-from-lore     Same, but auto-derived from TYLER's hero compendium instead of typed
  --annotation-alias NAME    Use a specific stored annotation alias for this batch only, without
                             changing the subject's default
```

#### Style hybrids (two or more `--tag` flags)

Passing `--tag` more than once does **not** force N separate generations — it combines the tags
into **one new blended style** ("style hybrid"), created (via Vertex AI, same as a single `--tag`)
and applied once. This is how `emily promptoverse add Medusa --tag kawaii --tag FFXI` produced a
single "kawaii × FFXI" style applied to Medusa, not two separate Medusa images.

Vocabulary note: **"mashup"** already means something else in this codebase — two *subjects*
combined (`emily promptoverse mashups`, the social nomination feature). A **"hybrid"** is two or
more *styles* combined. Keeping these distinct avoids confusing which axis a given record is on.

Internally: the combined tags are joined into one label (`"kawaii × FFXI"`), expanded through the
same Vertex-AI style-template flow as any forced `--tag`, and the resulting `discoveredStyle`
records which styles it was built from (`ComponentStyles`). The published node gets a
`style_hybrid_of` tag (e.g. `"kawaii, FFXI"`) — visible on the gallery page via the same generic
tag table every node already renders, no separate hybrid-specific page or endpoint.

With no explicit `<count>`, a hybrid request defaults to exactly 1 (one blended generation); an
explicit count forces the hybrid into one of that many slots, same as a single `--tag` would.

#### Subject annotations (`--annotation`, `--annotation-from-lore`, `--annotation-alias`)

Some subjects collide with real third-party IP under the same bare name (e.g. "Paimon" — TYLER's
own Goetia-king hero vs. Genshin Impact's companion character), which can trigger erroneous
content-policy blocks. Renaming the *subject* itself (e.g. "Paimon (demon)") is not the fix — it
would fragment the EZ prompt/taxonomy, which must stay exactly "Paimon" everywhere. Instead, an
**annotation** sticks to the subject itself and is appended only to the real generation prompt sent
to Vertex AI — never to the EZ prompt, the taxonomy subject, or the slug.

```bash
# Hand-write the annotation text, make it this subject's default
emily promptoverse add Paimon --annotation "this refers to Paimon, the Court Voice, a Goetia king..."

# Auto-derive it instead, from TYLER/multiverse_heroes.md's matching hero entry
# (hook + Field signature line from the Goetia frequency table)
emily promptoverse add Paimon --annotation-from-lore
```

A subject can carry more than one *named* annotation (alias), one marked default. Manage them
directly with `emily promptoverse annotations` (below); apply a non-default one to a single batch
with `--annotation-alias NAME` without changing what the subject uses by default.

#### Examples

```bash
emily promptoverse add ducks 6

# Force one style into a 4-style batch; the other 3 fill via normal selection
emily promptoverse add princess 4 --tag gladiator

# Style hybrid: exactly one blended "kawaii × FFXI" generation
emily promptoverse add Medusa --tag kawaii --tag FFXI

# Auto-pick the subject, no --tag
emily promptoverse add 6
```

---

### emily promptoverse work

```
emily promptoverse work [--force] [--slow]
```

Drains whatever's already queued without enqueueing anything new — e.g. to resume after a rate
limit stopped a previous `add`/`work` run. Same `--force`/`--slow` semantics as `add`.

---

### emily promptoverse queue

```
emily promptoverse queue
```

Lists pending queue entries, oldest first (`style x subject (queued <RFC3339>)`).

---

### emily promptoverse requeue

```
emily promptoverse requeue
```

Re-picks styles for everything still queued using the *current* marble-bag selection logic, skipping
any `--tag`-forced item. Useful when the registry/usage has shifted since items were queued.

---

### emily promptoverse styles

```
emily promptoverse styles
```

Lists the reusable style registry (hardcoded + discovered).

---

### emily promptoverse brainstorm

```
emily promptoverse brainstorm [--target styles|subjects] [--seed "a, b, c"] [--sample N]
                               [--max-tokens N] [--temperature F] [--via server|proxy|emily]
```

Prompts the GPT-2 fine-tune (gpt2-alpine-c) for style/subject candidates, seeded either explicitly
(`--seed`) or from a random sample of `--sample` existing items. `--via` selects which endpoint
serves the model (`server` :8088, `proxy` :8679, or `emily` :8086).

---

### emily promptoverse promote / promote-subject

```
emily promptoverse promote <label> [--rare]
emily promptoverse promote-subject <label> [--rare]
```

Turns a candidate/free-text label into a real, persisted style (`promote`) or known subject
(`promote-subject`) — `promote` expands it into a full style template via Vertex AI the same way a
forced `--tag` does. `--rare` marks it as competing for a selection slot only occasionally rather
than every run (see `promptoverse_pity.go`).

---

### emily promptoverse mashups

```
emily promptoverse mashups [--target subjects|styles] [--provider gemini|claude|all]
                            [--subject <label>]
```

LLM-judges which subjects/styles in the registry are genuine compositional **mashups** (subject +
subject; distinct from the "hybrid" style-combination concept documented under `add` above) or
paraphrase-equivalent duplicates — a real judgment call, not string matching. See
`internal/mashupjudge` and `NORTHSTAR_PROMPT_O_VERSE.md` §9.

---

### emily promptoverse regenerate

```
emily promptoverse regenerate <slug> --note "what should be different"
```

"Regenerate with variation" — e.g. a correction like "red hoodie instead of grey." **Additive, never
an overwrite**: posts a new variant image via IDUNA's `.../nodes/{slug}/variants` endpoint,
rendered alongside the original on the *same* leaf page (never a separate page, for SEO).

---

### emily promptoverse annotations

```
emily promptoverse annotations                                        # list every stored annotation
emily promptoverse annotations set <subject> [--alias NAME]
                                  [--text "..." | --from-lore] [--default]
emily promptoverse annotations clear <subject> [--alias NAME]
```

Direct management of subject-level annotations (see `add`'s `--annotation*` flags above) without
doing a generation at the same time. `--alias` names the entry (default alias name is `manual` for
hand-written text, `tyler-lore` for `--from-lore`); `--default` makes it the subject's default even
if another alias already holds that role. `clear` with no `--alias` removes every alias for the
subject; with `--alias`, removes just that one (promoting another remaining alias to default if the
one removed was it).

```bash
emily promptoverse annotations set Paimon --alias tyler-lore --default \
  --text "this refers to Paimon, the Court Voice, a Goetia king from TYLER's hero compendium..."

emily promptoverse annotations set Paimon --alias genshin-impact \
  --text "deliberately invoking the Genshin Impact companion character..."

emily promptoverse annotations clear Paimon --alias genshin-impact
```

---

### emily promptoverse backfill-annotation

```
emily promptoverse backfill-annotation <subject> [--alias NAME]
```

Marks every already-published node for `<subject>` as pre-annotation, linked to the annotation now
attached to that subject — for generations that existed before an annotation was set. Merges tags
(`pre_annotation`, `annotation_subject`, `annotation_alias`) onto each matching node via IDUNA's
`PATCH /api/v1/promptoverse/nodes/{slug}/tags`, without touching the node's image or prompt data.
Requires the subject to already have a stored annotation (`annotations set` first).

---

## emily backup

```
emily backup run [--target iduna|promptoverse|fatbaby|all]
emily backup decrypt <encrypted-file> <output-file>
```

Cloud backup for IDUNA / Prompt-o-verse / fatbaby data. Tars an allowlisted set of paths per
target and uploads via `gcloud storage cp` to `gs://project-d24a71e9-2daf-4b2d-917-backups`
(us-central1, 30-day retention lifecycle). Requires `gcloud` on `PATH`.

| Target | Paths | Encrypted? |
|---|---|---|
| `iduna` | IDUNA's SQLite stores (`var/*.db`) | Yes — AES-256-GCM |
| `promptoverse` | Rendered gallery (images + HTML) + JSON state | No |
| `fatbaby` | Curated cross-repo state (`BACKLOG.md`, `EMILY/var`, `PRRJECT_FATBABY/var`) | No |

The `iduna` target's encryption key lives at `IDUNA_ROOT/var/backup-encryption.key` (0600,
generated on first use) and is never uploaded alongside the backups it protects — back it up
yourself, elsewhere; losing it makes existing encrypted backups permanently unrecoverable.
`*.env`/credentials are excluded from every target regardless of encryption.

### Examples

```bash
emily backup run                         # all three targets
emily backup run --target iduna          # just the encrypted IDUNA target
emily backup decrypt iduna-143022.tar.gz.enc iduna-143022.tar.gz
tar xzf iduna-143022.tar.gz
```

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
| `EMILY_COLOR` | — | Set to `1` to enable ANSI color output |

If `IDUNA_AGENT_SECRET` is missing, Emily CLI auto-reads `IDUNA_SECRETS` (as a Go file parser, not shell `source`) and extracts the relevant secret for the configured `IDUNA_AGENT_NAME`.

---

## `emily tui` — Bloomberg Terminal Dashboard

```
emily tui
```

Bloomberg-style live terminal dashboard for the Einhorn Industrial agent stack.

**Layout (three-column grid):**
- **Left**: Repos (branch, dirty count, commit hash) + pending prime-tasks + token budget
- **Center**: Live Apple feed — auto-refreshes every 15s, shows newest first
- **Right**: Process health (obs-watcher, emily-agent, iduna.service) + RSI loop state + action hotkeys

**Hotkeys:**

| Key | Action |
|-----|--------|
| F1  | Fire `emily prime-task --preset rsi-token-report` |
| F2  | Start Tyler RSI loop (2 iterations, background) |
| F3  | Run `emily start` (bring up obs-watcher + emily-agent) |
| F4  | Tail `EMILY/var/logs/rsi-loop.log` (suspends TUI) |
| r   | Force refresh all panels |
| h   | Show hotkey help in status bar |
| q   | Quit |

**RSI loop state panel** reads `EMILY/var/rsi-loop-state.json` written by `rsi-loop.sh` to
show live iteration count, phase (tic/tock/entropy/analyze/complete), and active task ID.

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
