# EMILY CLI — DEVELOPMENT BACKLOG
## Machine-readable | Git-authoritative | RSI-driven

---

> Every `[x]` completion MUST have an Apple filed to IDUNA before closure.

---

## DONE

- [x] **docs layer** — NORTHSTAR.md, COMMANDS.md, DESIGN.md. Commit 61fc266. 2026-06-07.

- [x] **v0.1.0: foundation + four commands** — go.mod, internal/config, internal/iduna,
  internal/obs, cmd/observe, cmd/apples, cmd/status, cmd/sync. Apple #37. 2026-06-07.

- [x] **v0.2.0: observe auto-Apple + status --json** — emily observe posts Apple receipt
  immediately. emily status --json outputs typed JSON. Apple #38. 2026-06-07.

- [x] **emily watch** — IDUNA tail -f. Polls every N seconds, Ctrl-C clean exit.
  Flags: --interval/-i, --repo/-r, --type/-t, --quiet. Apple #39. 2026-06-07.

- [x] **observe stdin** — `echo "msg" | emily observe -s info` works. Apple #39. 2026-06-07.

- [x] **emily apples get <id>** — full Apple body reader, --json. Apple #39. 2026-06-07.

- [x] **command-specific help** — `emily help observe|apples|watch|status|sync`.
  Full flag tables, behavior notes, examples. Smoke tested. 2026-06-07.

- [x] **auto-build script** — `scripts/build.sh` builds + 6 smoke tests + go test + installs.
  Flags: --no-install, --no-test. 2026-06-07.

- [x] **EMILY repo reference** — EMILY/BACKLOG.md cross-links emily.cli. Apple #39. 2026-06-07.

- [x] **emily sync --watch** — daemon mode. syncPass every --interval seconds (default 10s).
  Ctrl-C clean exit. Apple #42. 2026-06-07.

- [x] **Go test suite** — 23 tests: internal/obs (8), internal/config (7), internal/iduna (8).
  httptest mock server for IDUNA. `go test ./...` wired into build.sh. Apple #42. 2026-06-07.

---

## SECTION 1: ACTIVE

- [ ] **Color output** — opt-in via `EMILY_COLOR=1` env. Severity coloring in `emily observe`
  (red=error, yellow=warn, green=info). Dirty repo count in yellow for `emily status`.
  Acceptance: `EMILY_COLOR=1 emily observe -s error "test"` shows red title line.

- [ ] **emily install --cron** — writes recommended crontab entries:
  `*/10 * * * * emily sync --watch --quiet` and `0 */4 * * * TYLER/scripts/cron-emily.sh`.
  Prints the entries; with --write appends to crontab via `crontab -l | crontab -`.
  Acceptance: `emily install --cron` prints correct entries; `--write` installs them.

---

## SECTION 2: INTEGRATION

- [ ] **observation-watcher prompt injection** — `emily observe` should write a `.prompt`
  sidecar file next to the observation JSON. The observation-watcher (in PRRJECT_FATBABY)
  checks for a `.prompt` file and prepends it to the Claude invocation context.
  Currently: operator message is buried inside the JSON body.

- [ ] **cmd tests** — `cmd/observe_test.go`, `cmd/apples_test.go` covering flag parsing
  and output format via captured stdout. Currently: cmd package has no test files.

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
