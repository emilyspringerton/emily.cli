## 2026-08-04

- emily iduna create-account — mint real DragonsNShit test accounts from the CLI (wraps email/register's character_name variant) (sess-20260723-2347-df115bd5)

## 2026-07-31 (2)

- fix(redgarden): `emily redgarden` failed with "Permission denied" for the founder — root
  cause: this shell's `XDG_RUNTIME_DIR` was inherited as `/run/user/0` (root's runtime dir) even
  though the process runs as uid 1000, so `systemctl --user` tried to reach root's session bus
  instead of the founder's own. `redgardenSystemctlEnv()` previously only filled in
  `XDG_RUNTIME_DIR` when it was *unset*; a wrong-but-present value passed straight through. Now
  it always strips any inherited value and sets `/run/user/<actual-uid>`. Also found and fixed a
  related correctness bug this surfaced: `bots [N]` wrote the new count to the live unit file
  *before* running `daemon-reload`/`restart`, so a failure at either of those steps (as the
  permission bug caused) left the on-disk unit pointing at a count the live pool was never
  actually restarted to match. Both steps now roll the file back to its prior content on
  failure, so a failed `bots` call can't leave the config and the running pool silently
  disagreeing. Verified against the founder's exact broken environment (reproduced
  `XDG_RUNTIME_DIR=/run/user/0`): `bots 19` now correctly writes, reloads, restarts, and lands
  the live pool at 19 (confirmed via live process count).

## 2026-07-31

- feat(redgarden): `emily redgarden bots [N]` / `emily redgarden status` — self-service control
  over the live REDGARDEN persistent bot-pool size, so the founder can scale it down to open a
  human slot at the bot-pool matchmaker (:7778) without asking Claude Code to do it each time.
  Defaults to 20 (fully self-sustaining, no open human slot) when N is omitted, matching the
  founder's explicit spec. Edits the live systemd user unit
  (`~/.config/systemd/user/redgarden-bot-pool.service`)'s `ExecStart=` line and `Description=`
  bot-count text in place, then `systemctl --user daemon-reload` + `restart
  redgarden-bot-pool.service`. Rejects out-of-range counts (0-20, since `lobby_size` is fixed at
  20). `status` reports the configured count plus live state of the bot-pool and both
  matchmaker services. Verified end-to-end against the real running pool (20 -> 19 -> 20,
  confirmed via live process count each time).

## 2026-07-25

- feat(vault): `emily vault init|unlock|lock|status|add|get|list|delete` — CLI for IDUNA Vault
  VS0 (S170-03b). Talks to IDUNA's new loopback-only `/api/v1/vault/*` endpoints; passphrases
  always read interactively with echo disabled (`golang.org/x/term`, first external dependency
  this module has needed — was pure stdlib before), never as a flag/arg, same rationale as the
  existing `cmd/mailing-list-unlock` precedent in IDUNA. `emily vault add -type <type> -name
  <name> -field k=v ...` for flexible per-item fields across the five VS0 item types (login,
  note, api_key, totp, document). Verified end-to-end against a real running IDUNA instance
  before deploying (init, unlock, add, list, get, delete, lock, re-unlock).

## 2026-07-24

- feat(saga): `emily saga lint` (HQ-SPEC-DOC-102 build-sequence step 1) — parses the restricted-YAML frontmatter (`doc_id`, `authority`, `supersedes`, `amends`, `claims`) every `EMILY/docs/hq-specs/*.md` now carries, hand-rolled parser, no new YAML dependency (stdlib-first, matching this codebase's convention elsewhere). Checks: enum validity on `authority`/claim `type`/`reality_binding`, claim-ID format + doc-of-origin ownership, ID collisions across the corpus, dangling `supersedes`/`amends` references, unenumerated inheritance (an `amends` entry naming no claims), and orphan goldens (a fully-superseded doc still marked golden). See `EMILY/docs/hq-specs/SAGA_SCHEMA.md` for the schema itself. 13 new tests (parser + all 7 lint rules + a real-corpus integration check against the actual retrofitted HQ-SPEC docs). Also fixed an unrelated pre-existing `go vet` warning surfaced while building this (`append(os.Environ())` with no values in `cmd/emilyos.go` — harmless no-op, simplified to a bare assignment).

## 2026-07-19

- Fixed emily status --fatbaby: newssite/signalapi/secwatch were incorrectly reported down (stale pgrep patterns from before their systemd migration); also fixed a pid-list collision bug in entity-graph/secwatch matching

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

