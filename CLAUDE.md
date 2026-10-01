# emily.cli — Operator Terminal for EINHORN_INDUSTRIAL

`emily` is the operator CLI — the human-facing control surface for Emily Prime and the
FatBaby signal pipeline. All interaction with IDUNA, obs-watcher, the TUI, and agent status
flows through `emily` commands.

## Listening on

CLI binary, no server port. Reads `IDUNA_BASE_URL` + `IDUNA_AGENT_SECRET` from env or
`IDUNA/var/agent-secrets.env` (auto-discovered).

## Key Commands

| Command | Description |
|---|---|
| `emily observe <msg>` | Post observation to FatBaby pipeline (IDUNA + signal file). Auto-tags with the active `emily session`; general-purpose founder-input intake, not FatBaby-only despite the name |
| `emily obs amend <key> <correction>` | Append correction to an existing observation |
| `emily apples list [filter]` | Query IDUNA Apples log |
| `emily apples post -t <type> <title>` | File an Apple to IDUNA |
| `emily watch [repo]` | Tail IDUNA Apples in real-time |
| `emily status [--fatbaby] [--watch]` | Cross-repo git + IDUNA + process state |
| `emily start [all]` | Start FatBaby processes (via systemd or direct) |
| `emily sync [--all] [--apples-git-dir <dir>]` | Sync observations → IDUNA, Apples → git |
| `emily tui [--fatbaby]` | Full-terminal TUI (v0.8.0); 'b' toggles FatBaby panel |
| `emily backlog [promote]` | Curate/promote INTAKE QUEUE items via haiku |
| `emily agents list` | List registered IDUNA agents |
| `emily primetask [create|list]` | Interact with Emily Prime RSI task queue |
| `emily session new` / `emily session current` | Generate/retrieve the session fingerprint (`sess-YYYYMMDD-HHMM-<8hex>`) auto-stamped onto Apples, CHANGELOG entries, and observations |

## Directory Layout

```
cmd/
  tui.go          — full-terminal TUI (col 1: roadmap, col 2: tasks, col 3: health/fatbaby)
  observe.go      — emily observe + obs amend
  apples.go       — emily apples list/post
  status.go       — emily status --fatbaby --watch
  sync.go         — emily sync --apples-git-dir
  start.go        — emily start (all/individual process)
  backlog.go      — emily backlog promote
  watch.go        — emily watch (Apple tail)
  agents.go       — emily agents list
  primetask.go    — emily primetask
```

## TUI Layout (v0.8.0)

```
┌──────────────────────────────────────────────────────────┐
│ col 1: RSI Roadmap   │ col 2: Active Tasks  │ col 3: Health │
│  (task list)         │  (task detail)       │  or FatBaby   │
│                      │                      │  ('b' toggle) │
└──────────────────────────────────────────────────────────┘
```

`--fatbaby` flag pre-activates FatBaby panel in col 3. `'b'`/`'B'` toggles at runtime.
TUI writes PID to `/tmp/emily-tui.pid` for `emily status` liveness check.

## Key Env Vars

```
IDUNA_BASE_URL        — default: http://localhost:8080
IDUNA_AGENT_NAME      — EMILY_PRIME (or override for other agents)
IDUNA_AGENT_SECRET    — M2M credential (auto-loaded from IDUNA/var/agent-secrets.env)
ANTHROPIC_API_KEY     — required for emily backlog promote (haiku)
APPLES_GIT_DIR        — /home/fatbaby/APPLES (for emily sync --apples-git-dir)
```

## Related Repos

- `EMILY` — Emily Prime agent (`:8086`); emily.cli is its human interface
- `IDUNA` — IAM + Apples store (`:8080`); all auth and Apple calls go here
- `PRRJECT_FATBABY` — Signal pipeline; obs-watcher reads files emily observe creates
- `APPLES` — Apple git backup synced via `emily sync --apples-git-dir`

## Apple Filing Protocol

After any meaningful change, file an Apple:
```bash
emily apples post -t completion "<title>" "<body with commit hash>"
```
Then mark the item done in EMILY/BACKLOG.md and commit.

## CHANGELOG Protocol

After any meaningful change, update CHANGELOG.md:
```bash
emily changelog add emily.cli "<what changed>"
# or manually: append a dated bullet under ## YYYY-MM-DD in emily.cli/CHANGELOG.md
```

## Golden Doc Registration

If you create a new NORTHSTAR.md, architecture spec, or mission-critical design doc in this repo,
append a row to `EMILY/context/golden-docs-index.md` so Emily Prime picks it up on the next cycle:
```
| NAME | <repo>/path/to/doc.md | 1 | <budget-or-0> | one-line description |
```
Then commit and push EMILY:
```bash
cd /home/fatbaby/EMILY && git add context/golden-docs-index.md && git commit -m "golden-index: add NAME" && git push
```

## README Reality — SAGA reconciliation (standing instruction, monorepo-wide)

Founder real-time, 2026-09-18: if a change of yours **substantially changes the claim of this project's core README**,
then per SAGA protocols (`EMILY/docs/SAGA_SYSTEM_AUDIT_2026-07-18.md`, HQ-SPEC-DOC-102: intent ↔ claim ledger ↔ reality)
you **must update `README.md` in the same unit of work** so it reflects current reality. The README is the project's public
claim; it must not lag behind the code.

- **When it applies:** a capability is added or removed; status moves ("design only" → "working", "planned" → "shipped");
  the stack, build, run or install steps change; a claim in the README is now false or stale; or you add a **meaningful,
  genuinely interesting piece of kit** (a new tool, engine capability, protocol, pipeline, game system). For that last case
  especially: put it in the README — what it is, how to run it, and its honest status and limits.
- **When it does not:** ordinary fixes, refactors and small features that leave the README's claims true.
- **How:** re-read the README against what you just changed; fix or delete stale lines (including "not built yet" notes that
  are now built); verify any new claim by actually running it, and mark anything untested as untested; commit the README
  with (or immediately after) the change, and mention it in the CHANGELOG entry.

## Frame-Break Reframing

Founder-sourced prompting technique (REDGARDEN/NORTHSTAR.md §28, full origin in
REDGARDEN/docs2/MULTI_AGENT_RD_RESEARCH_NOTES.md §5): given a request, name the underlying
structural/systemic pattern it's one instance of — one level of abstraction up — as an added
lens during planning/triage/judgment calls. Use it to spot the general case behind a specific
ask. It augments judgment, it does not replace doing the work: direct, concrete execution of
the literal task asked for still happens every time.

## Commit Protocol (standing instruction)

Always commit and push completed work immediately — don't wait to be asked. This is the default for every repo in this monorepo.

Every commit — human-written or produced by automated code paths (git-commit helpers in emily-agent, emily.cli, IDUNA handlers, etc.) — must carry the active `emily session` fingerprint as a `session: <tag>` trailer (blank line, then the trailer). This was silently missing from several independently-implemented automated commit helpers across the monorepo until an audit on 2026-08-10 (founder, real-time: "where in the fuck is my llm session id anywhere"). If you add a new automated git-commit code path anywhere, wire in the session tag the same way — don't assume an existing helper already does it.

## Core Deps Are PARENA-First (standing, monorepo-wide)

Founder real-time, 2026-10-01: *"always implement core deps in PARENA — when core deps are missing
always implement the core deps in PARENA first."*

- **When a core dependency is missing** (a codec, a protocol client, an inference engine, a
  parser, a data structure — anything this repo's own functionality stands on), implement it in
  PARENA (`PARENA/stdlib/...`) **first**, before building the feature that needs it. Deps first,
  feature second.
- **If PARENA itself can't express the dep yet**, that gap is the real first task: fix or extend
  PARENA (compiler, emitter, or stdlib), with tests, then build the dep on top. Don't route around it.
- **Third-party tools/binaries are stopgaps, not the answer.** Shelling out to or FFI-binding an
  existing tool is allowed only to unblock a demo, and must be labeled as a stopgap in the code and
  in `EMILY/BACKLOG.md` with a PARENA replacement item. (Example: Piper via subprocess for
  MODE_TYLER TTS, 2026-10-01 — stopgap; the PARENA-native synthesis stack is the real work.)
- **Not a license to reimplement the OS.** Core deps = what the product's own behavior depends on.
  Compilers, kernels, system libraries and the like stay as-is; a repo's own CLAUDE.md may record a
  considered, specific exception (same standard as the LZ4 compression convention).
