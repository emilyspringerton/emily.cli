# EMILY CLI — DEVELOPMENT BACKLOG
## Machine-readable | Git-authoritative | RSI-driven

---

> Every `[x]` completion MUST have an Apple filed to IDUNA before closure.

---

## DONE

- [x] **docs layer** — NORTHSTAR.md, COMMANDS.md, DESIGN.md. Commit 61fc266. 2026-06-07.

- [x] **v0.1.0: foundation + four commands** — go.mod, internal/config, internal/iduna,
  internal/obs, cmd/observe, cmd/apples, cmd/status, cmd/sync. All compile and pass smoke
  tests. Binary installed to ~/.local/bin/emily. Apple #37. 2026-06-07.

- [x] **v0.2.0: observe auto-Apple + status --json** — emily observe posts Apple receipt
  immediately after writing obs file (when IDUNA credentials available). emily status --json
  outputs typed JSON (repos + last Apple per agent). Apple #38. 2026-06-07.

---

## SECTION 1: CORE (immediate sprint)

- [x] **emily watch** — follow mode for Apples log. Polls IDUNA every N seconds, prints new
  Apples as they arrive. Like `tail -f` for the agent activity log. Apple #39. 2026-06-07.
  Flags: --interval/-i N, --repo/-r, --type/-t, --quiet.

- [x] **stdin input for observe** — read summary from stdin when no positional arg given
  and stdin is a pipe. `echo "msg" | emily observe -s info` now works. Apple #39. 2026-06-07.

- [x] **emily apples get <id>** — show full body of a single Apple by ID.
  `emily apples get 39` shows full body, type, repo, run, recorded_at. --json also works.
  Apple #39 receipt. 2026-06-07.

- [x] **command-specific help** — `emily help observe|apples|watch|status|sync` prints
  full flag tables, behavior notes, and examples for each command. Unknown command returns
  exit 1. Smoke tests in build.sh. 2026-06-07.

---

## SECTION 2: POLISH

- [x] **auto-build script** — `scripts/build.sh` builds, runs 4 smoke tests, and installs
  to ~/.local/bin. `./scripts/build.sh --no-install --no-test` for CI. 2026-06-07.

- [ ] **emily sync --watch** — run sync in a loop, polling for new FatBaby observation files.
  Replaces the manual `emily sync` call after each FatBaby cycle.

- [ ] **EMILY repo reference** — update EMILY/BACKLOG.md to note emily.cli exists and is
  the canonical operator CLI. Cross-link to emily.cli repo.

- [ ] **Color output** — opt-in via `EMILY_COLOR=1` or `--color`. Severity coloring for
  `emily observe` output (red=error, yellow=warn). Repo dirty tag in yellow.

---

## SECTION 3: INTEGRATION

- [ ] **observation-watcher prompt injection** — when `emily observe` fires, prepend the
  observation text to the standard observation-watcher Claude prompt so the operator's
  message is the first thing Claude reads. Currently: watcher injects the JSON file;
  operator message is inside the JSON.

- [ ] **emily install** — subcommand that writes the recommended crontab entry for
  `emily sync` and `TYLER/scripts/cron-emily.sh` to the user's crontab.
  ```
  emily install --cron     # add crontab entries
  emily install --systemd  # generate systemd unit file for IDUNA
  ```

- [ ] **Go test suite** — unit tests for internal/obs, internal/iduna (mock server),
  internal/config (env var parsing). Acceptance: `go test ./...` passes.

---

## BACKLOG PROTOCOL

1. Pick the highest-priority `[ ]` item.
2. Implement it.
3. Post an Apple: `emily apples post -t backlog_completion "<task name>"`.
4. Mark `[x]` with Apple ID and date.
5. Commit: `git commit -m "feat: <task> — Apple #N"`.
6. Push.
7. Repeat.

---

*emily.cli BACKLOG | docs first, build second, analyze third*
