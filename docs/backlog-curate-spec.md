# emily backlog curate — Golden Specification
## Owner: Emily Prime | 2026-06-11

---

## Purpose

Close the observation → backlog gap. Every FatBaby observation that carries a product
or system idea must eventually land in `EMILY/BACKLOG.md`. Currently there is no
instrumented path — curation is manual or via Claude roleplay. This command is the
first automated step.

---

## Command

```
emily backlog curate [flags]
```

**Aliases:** `emily backlog` (default subcommand is `curate`)

---

## Flags

| Flag | Default | Description |
|---|---|---|
| `--all` | false | Process all uncurated observations; without, only the most recent N |
| `--limit N` | 10 | Max observations to curate in one pass |
| `--dry-run` | false | Print what would be appended; write nothing |
| `--no-commit` | false | Write BACKLOG.md but skip git commit |
| `--no-apple` | false | Skip IDUNA Apple receipt |
| `--json` | false | Output one JSON object with summary |
| `--section` | "INTAKE" | Section label for new backlog items (default: INTAKE QUEUE) |

---

## Behaviour

1. Load curated timestamps from `EMILY/var/backlog-curated.txt` (one per line).
2. List `FATBABY_ROOT/var/emily-observations/` sorted in reverse chronological order.
   Skip `latest.json`.
3. For each observation not in the curated-set, up to `--limit`:
   a. Parse `timestamp` and `summary` from the JSON file.
   b. Append a `[ ]` item to the INTAKE QUEUE section of `EMILY/BACKLOG.md`.
      Create the section if it does not exist.
4. If any items were appended (and `--dry-run` is false):
   a. Write updated `BACKLOG.md`.
   b. `git add BACKLOG.md && git commit` in the EMILY repo (unless `--no-commit`).
   c. Post a `curation` Apple to IDUNA with title summarising what was added
      (unless `--no-apple`).
   d. Append each newly-curated timestamp to `backlog-curated.txt`.
5. Print a summary to stdout.

---

## Backlog Item Format

```
- [ ] **<summary>** — obs `<timestamp>`. CURATED: <YYYY-MM-DD>.
```

Example:
```
- [ ] **emily os mobility edition bare metal exokernel ISO2424242** — obs `2026-06-11T00:54:39Z`. CURATED: 2026-06-11.
```

---

## State File

`EMILY/var/backlog-curated.txt` — append-only, one RFC3339 timestamp per line.
Idempotent: running curate twice on the same set is a no-op.
Parallel to `fatbaby-synced.txt` (which tracks obs→IDUNA Apple sync).

---

## Intake Queue Section

Appended to `EMILY/BACKLOG.md` when first needed:

```markdown
## INTAKE QUEUE (curated by emily backlog curate)

Items here have been auto-curated from FatBaby observations. Emily Prime reviews
and promotes them into the appropriate section when she plans the next sprint.
```

---

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success (even if 0 items curated — no new obs) |
| 1 | Usage error |
| 2 | Auth error (IDUNA unavailable with --no-apple is fine) |
| 3 | Write failure (BACKLOG.md or state file) |
| 4 | API error |
