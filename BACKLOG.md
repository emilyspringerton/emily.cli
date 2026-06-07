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

- [x] **Color output** — `EMILY_COLOR=1` env. internal/color pkg. Severity in observe
  (red=error, yellow=warn, green=info). Dirty count in yellow for status. 2026-06-07.

- [x] **emily install --cron** — prints 2 recommended crontab entries (sync + Tyler cron).
  `--write` installs via `crontab -l | crontab -`, skipping already-present entries.
  Binary path auto-resolved via `which emily` or $HOME/.local/bin/emily. 2026-06-07.

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
