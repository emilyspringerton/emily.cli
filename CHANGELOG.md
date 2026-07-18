## 2026-07-18

- feat(key/S153-05): `emily key` generalized beyond hardcoded `ANTHROPIC_API_KEY` — `emily key set <NAME> <VALUE> [--target emily|iduna] [--file <path>]` writes any named secret to a target env file. `--target iduna` resolves to `~/.config/iduna/env` and writes plain `KEY=VALUE` (no `export ` prefix) since systemd's `EnvironmentFile=` doesn't understand shell export syntax — `--target emily` (default) keeps the existing export-prefixed, shell-sourced format for `EMILY/var/emily-secrets.env`. Legacy one-arg `emily key set sk-ant-...` shorthand still works unchanged. `config.WriteEmilySecret`/`RemoveEmilySecret` refactored onto new generic `WriteEnvFile`/`RemoveEnvFile`/`ReadEnvValue` (exported, reusable). Requested directly to let `MAILCHIMP_API_KEY`/`MAILCHIMP_LIST_ID` (IDUNA's new mailing-list feature, EMILY BACKLOG SECTION 153) be set without hand-editing files. 6 new tests.

## 2026-07-16

- fix(start): `emily-agent (daemon)` idempotency check could never match the real process — it runs as `go run . -- --daemon` (cwd=EMILY/emily-agent), whose `/proc/pid/cmdline` never contains the literal substring "emily-agent" (cwd isn't argv), so the old pgrep pattern `"emily-agent.*--daemon|go run.*emily-agent.*--daemon"` was structurally unmatchable. Every re-run of `emily start` (including `emily-system.service`'s reboot-time oneshot) would have spawned a duplicate daemon. Switched to a PID file (`EMILY/var/emily-agent.pid`, same convention as `tuiPIDFile`) written by `startEmilyAgent` and verified via kill-0 liveness check.
- fix(status): `collectProcesses`/`collectFatBabyProcesses` used bare `pgrep -f "observation-watcher"` / `"emily-agent"` substring patterns for the PROCESSES health display — broad enough to match *any* process whose cmdline happens to mention the name, including an unrelated interactive session. Tightened observation-watcher to the same anchored `"cmd/observation-watcher --root"` pattern already used in `start.go`, and switched emily-agent to the new PID-file check.

## 2026-07-15

- fix(start): `--all` no longer bundles `shank_go_server` — it silently started SHANKPIT's live game server + fill bots any time someone ran `emily start --iduna --all`. Gated only by the explicit `--shankpit` flag now.
- fix(start): `RunStart` returns exit 1 if any child process failed to launch; previously always returned 0 regardless, so `emily-system.service` (Type=oneshot) reported "active (exited)" success even on total failure.
- fix(start): tightened observation-watcher's pgrep idempotency pattern from bare `"observation-watcher"` to `"cmd/observation-watcher --root"` — the old pattern could match an operator's own `tail -f .../observation-watcher.log` and cause `emily start` to skip launching it, believing it already running.
- feat(start): added `--entity-graph`, `--eps-reconciler`, `--eps-processor` flags (all bundled into `--all`) — these three PRRJECT_FATBABY processes previously had zero `emily start` coverage and were always launched by hand with no idempotency guard, risking two copies double-writing to the same append-only NDJSON event stores.

## 2026-06-26

- add emily start --earnings-alert: installs systemd timer + service firing Mon 07:30 UTC, builds earnings-alert binary from PRRJECT_FATBABY if absent

## 2026-06-25

- feat(ci): GitHub Actions CI workflow — test, build, smoke test, construct bundle

## 2026-06-24
- feat: S125-08 emily memory consolidate — merge *.json fragments → consolidated.json (Apple #3667)

- feat: S126-05 emily memory digest command — prints obs digest in TUI format (Apple #3541)

## 2026-06-23

- S103-03: emily status shows ARCHETYPE ENGINE online/offline via emily-agent proxy

## 2026-06-21
- feat: S49-02 emily status shows EmilyOS posture (Apple #2347)
- feat: S48-02 emily shankpit leaderboard command (Apple #2341)

- feat: S47-04 emily emilyos command — wraps EmilyOS CLI (Apple #2336)

## 2026-06-18
- feat: emily gsync — git archive + Google Drive upload via IDUNA; emily gsync SHANKPIT uploads SHANKPIT.tar.gz to configured Drive folder (Apple #1437)
- feat: S42-A emily shankpit command — status/players/kick/observe/restart; auto-start admin on emily start --shankpit (Apple #1435)

- feat(start): emily start --shankpit — launches shank_go_server on :6969 + N emily-bot fill players (--bots N, default 2); builds binaries if absent (GOWORK=off); 1.5s warmup before bots connect; ShankpitRoot added to config (SHANKPIT_ROOT env / /home/fatbaby/SHANKPIT default)
- fix(start): S37-04 EMILY_BASE_URL default in startNewssite — injects EMILY_BASE_URL=http://localhost:8086 when env not already set, so /api/ask works without manual env configuration

## 2026-06-17

- emily gpt2 generate (POST :8088/generate, --via server|emily|proxy) + emily gpt2 health (HTTP check :8088/:8086/:8679). Apple #945.

## 2026-06-16
- feat: emily start --signalapi launches signalapi on :9091 detached

- feat(chat): `emily chat` — terminal Emily Prime chat (haiku, no port); streaming SSE response; dark ANSI UI matching web emily-agent aesthetic; multi-line input via trailing \; --session FILE persists history; loads full-system-context.md when present

## 2026-06-15

- emily gpt2 start|proxy|status|tokenizer — manage GPT-2 inference stack and FatBaby broker proxy

## 2026-06-14
- emily train build-dataset: --colab preset default (Emily operational text only, deduped, ≤1500 records / 2.2 min T4); --no-colab to get full corpus; fixes corpus explosion from auto-discovered SEC+TYLER sources
- add --fatbaby-root, --tyler-root, --max-sec-docs, --max-pr-docs flags to emily train build-dataset; pass through to prime_directive_dataset.py
- feat(key): `emily key set|show|unset` — persists ANTHROPIC_API_KEY to EMILY/var/emily-secrets.env (0600); auto-injected into process env by config.Resolve() so emily backlog promote + emily context build work without exporting the key each session
- feat(config): AnthropicKey field in Config; readEnvFile reads KEY=value from any shell env file; WriteEmilySecret/RemoveEmilySecret for emily-secrets.env; EmilySecretsFile path (EMILY_SECRETS env or EMILY/var/emily-secrets.env)
- feat(train): `emily train` command — build-dataset (runs scripts/prime_directive_dataset.py), upload (DriveUpload via IDUNA EMILY-TRAINING agent), status (lists Drive files + pipeline steps)
- feat(iduna): DriveUpload, DriveList, DriveGet methods added to internal/iduna/client.go
- feat(start): emily start --agi enables AGI loop mode; passes --continue to obs-watcher so claude RSI cycles build persistent context across invocations (the AGI loop pattern)

## 2026-06-13
- feat(backlog): `emily backlog add [--section N] "<item>"` — programmatic item insertion into any BACKLOG.md section (S22-08)
- feat(backlog): `emily backlog add-section [--title "<title>"]` — append new numbered section to BACKLOG.md (S22-08)
- feat(northstar): `emily northstar <repo>` — print NORTHSTAR.md for any repo; resolves docs/ and docs2/ (S22-09)

## 2026-06-12
- feat(context): emily context build — compresses 16 Tier 1 golden docs via haiku bilingual Chinese/English into EMILY/context/full-system-context.md

- emily tui: real token accounting from IDUNA Apple metadata (fetchTokenSpendFromIDUNA), F5 hotkey fires rsi-loop.sh iteration in background

