# EMILY CLI — DEVELOPMENT BACKLOG
## Machine-readable | Git-authoritative | RSI-driven

---

> Every `[x]` completion MUST have an Apple filed to IDUNA before closure.

---

## DONE

- [x] **docs layer** — NORTHSTAR.md, COMMANDS.md, DESIGN.md. Commit 61fc266. 2026-06-07.
- [x] **v0.1.0: foundation + four commands** — observe, apples, status, sync. Apple #37. 2026-06-07.
- [x] **v0.2.0: observe auto-Apple + status --json**. Apple #38. 2026-06-07.
- [x] **emily watch** — IDUNA tail -f. Apple #39. 2026-06-07.
- [x] **observe stdin** — `echo "msg" | emily observe -s info`. Apple #39. 2026-06-07.
- [x] **emily apples get <id>** — full Apple body reader. Apple #39. 2026-06-07.
- [x] **command-specific help** — `emily help <cmd>` per-command rich text. 2026-06-07.
- [x] **auto-build script** — scripts/build.sh: build + smoke tests + go test + install. 2026-06-07.
- [x] **EMILY repo reference** — EMILY/BACKLOG.md cross-links emily.cli. 2026-06-07.
- [x] **emily sync --watch** — daemon mode, 10s poll. Apple #42. 2026-06-07.
- [x] **Go test suite v1** — 23 tests: internal/obs (8), config (7), iduna (8). Apple #42. 2026-06-07.
- [x] **color output** — internal/color, EMILY_COLOR=1 env. Severity in observe, dirty tag in status. 2026-06-07.
- [x] **emily install --cron** — prints + installs 2 crontab entries. 2026-06-07.
- [x] **cmd test suite** — 16 tests: observe (7), install (3), sync (4) + testutil. 2026-06-07.
- [x] **emily prime-task** — writes directed task to EMILY/signals/tasks/. Apple #44. 2026-06-07.
- [x] **emily agents** — agent activity dashboard from Apples log (no /agents endpoint exists).
  Groups by source_repo, shows age/count/last-apple. --since N filter. Apple #45. 2026-06-07.
- [x] **internal/color tests** — 7 tests (disabled + EMILY_COLOR=1 modes). 2026-06-07.
- [x] **cmd/agents_test + cmd/primetask_test** — 11 tests. Total suite: 55 tests. 2026-06-07.

---

## SECTION 1: ACTIVE

- [x] **emily status --watch** — live-updating dashboard. Clears terminal (ANSI \033[H\033[2J),
  reprints full status every --interval seconds (default 30). Ctrl-C exits cleanly. 2026-06-07.

- [x] **emily install --systemd** — generates systemd user unit for `emily sync --watch --quiet`.
  `--write` installs to `~/.config/systemd/user/emily-sync.service`. Prints next steps.
  2 new tests in install_test.go. 2026-06-07.

---

## SECTION 2: POLISH

- [x] **scripts/build.sh --color** — `EMILY_COLOR=1 go test ./internal/color/... -run "_enabled_"`
  already present in build.sh step 2b. Confirmed. Apple #49. 2026-06-07.

- [x] **COMMANDS.md: agents + prime-task sections** — both sections present and complete.
  Updated COMMANDS.md to v0.5.0: status --watch flags, install --systemd section.
  Apple #49. 2026-06-07.

---

## SECTION 3: NEXT HORIZON

- [x] **emily status PROCESSES section** — shows observation-watcher running state (pgrep -f),
  emily-sync.service systemd unit state, and latest observation file path + age.
  Both one-shot and --watch modes. Apple #63. 2026-06-07.

- [ ] **emily status: obs-watcher tasks cursor** — show age of last task file in EMILY/signals/tasks/
  and whether the tasks dir is empty. Gives visibility on prime-task delivery.

- [ ] **emily apples list --since N** — filter by recency (minutes) not just count.
  Useful for "what happened in the last hour" queries without scrolling.

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
