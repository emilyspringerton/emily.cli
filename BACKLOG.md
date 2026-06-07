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

- [x] **cmd tests** — 16 tests across cmd/observe (7), cmd/install (3), cmd/sync (4) + shared
  testutil. Captured stdout/stderr via os.Pipe(). Stdin injection via pipeStdin(). 2026-06-07.

- [ ] **observation-watcher: already rich** — watcher already puts Summary, Findings,
  SuggestedFix at the top of its Claude prompt. The `.prompt` sidecar is deprioritized.
  Future: if we want operator-custom prompts, add a `--prompt-prefix` flag to `emily observe`
  that writes an extra field the watcher can pick up.

---

## SECTION 3: FUTURE

- [ ] **emily agents list** — query IDUNA `/api/v1/agents` to show registered agents,
  their last heartbeat, and recent Apple count. Good for system health overview.

- [x] **emily prime-task <description>** — writes task JSON to EMILY/signals/tasks/.
  Flags: --type, --priority, --context, --criteria (repeatable), --deadline, --dry-run.
  Obs-watcher picks it up within 10s → invokes Claude on FatBaby. Apple #44. 2026-06-07.

- [ ] **color: internal/color tests** — test that Severity/Warn/Bold/Cyan return plain
  strings when EMILY_COLOR is unset, and return ANSI sequences when set.

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
