## 2026-09-02

- `emily email send --to <address> --subject <text> (--body <text> | --body-file <path>)` shipped -- a real, general "send one plain-text email" CLI command, extracted from a one-off Go program written in-session to deliver a real PAPERCRAFT account's login credentials. Reuses the exact SMTP path (STARTTLS, Gmail App Password) emily-agent/gmail.go's own "Path 2" already established, via new `config.Config.GmailSMTPAddress/GmailSMTPPassword` fields auto-resolved from `GMAIL_SMTP_ADDRESS`/`GMAIL_SMTP_PASSWORD` (env, or `EMILY/var/emily-secrets.env` via `emily key set`) the same way `AnthropicKey` already is. The specific send this was built for (to garybifrost@gmail.com) failed from this Claude Code sandbox -- outbound SMTP (587 and 465) both time out here even though plain HTTPS works, a real, confirmed egress restriction, not a code/credential bug -- so this command exists to retry that send from an environment that does have SMTP egress. 4 new tests (arg validation, missing-credentials path); real send itself untestable without a live SMTP endpoint, verified instead by an actual live send attempt this session (hit the same confirmed network wall, not a code error). session: sess-20260902-2008-ed50169e

## 2026-08-26

- emily kanban {list,add,move,rm} shipped -- agent/CLI half of the priority layer over BACKLOG.md ('i can ask the ai agent to work from the priority or cruise backlog'). New iduna.Client kanban methods, 8 new tests. Also documented emily golden (S200-04, shipped last session but missing from README) alongside it. Apple #16053. (sess-20260825-1938-f6bd411e)

## 2026-08-25
- S200-04: 'emily golden {list,enable,disable,status}' -- toggleable golden-repo config + real GitHub Actions CI-status polling, replacing manual curl+python one-liners. Deliberately pure Go (cgo would regress the pure-Go cross-compile release pipeline S202-20 just shipped), flagged not silently decided. 10 new tests, go test ./... green. Live-verified: first run correctly surfaced a real, currently-broken PRRJECT_FATBABY CI outage. Apple #16037. (sess-20260825-1938-f6bd411e)
- emily context build rewritten pure-CLI (no LLM, no ANTHROPIC_API_KEY): deterministic header+lead-line extractive summary replaces the old claude-haiku compression call. Live-verified with the key unset: 47/47 golden sources compile. Commit 79bc797. (sess-20260825-1938-f6bd411e)

- added auto-release CI job: cross-compiled linux/darwin binaries, real non-prerelease GitHub release on every push to main (sess-20260825-1938-f6bd411e)

## 2026-08-20
- docs(README): documented `emily key set GMAIL_SMTP_ADDRESS`/`GMAIL_SMTP_PASSWORD` — the founder
  asked directly to make sure the README has real instructions for adding the Gmail app password.
  Added a concrete example under the existing `emily key` section (no new subcommand needed, the
  generic form already covers it) pointing at both the SMTP send path (`emily-agent/gmail.go`) and
  the IMAP read script (`EMILY/scripts/gmail_imap_fetch.py`), plus an explicit note not to paste
  the password into chat. (sess-20260813-2154-dda37e8b)

## 2026-08-18
- Replaced vacuum-based spontaneous subject discovery with style-anchored discovery: pick a style via the weighted scheme, ask Vertex for its archetypal subject, retry with a different style if declined (sess-20260813-2154-dda37e8b)
- Fixed vertexTextGenerate's 30s HTTP timeout (bumped to 90s) causing silent failures when creating new forced/hybrid Prompt-o-verse styles (sess-20260813-2154-dda37e8b)
- Added Prompt-o-verse style sweeps: add <count> --tag X with no subject locks <count> different auto-picked subjects to one style (sess-20260813-2154-dda37e8b)
- Documented emily backup in README.md + docs/COMMANDS.md (previously entirely undocumented); fixed stale Prompt-o-verse section and test count in README (sess-20260813-2154-dda37e8b)
- Documented all emily promptoverse subcommands in docs/COMMANDS.md (incl. style hybrids, subject annotations); synced --help text; fixed build.sh install ETXTBSY (sess-20260813-2154-dda37e8b)
- Added subject-level Prompt-o-verse prompt annotations (multi-alias, TYLER-lore auto-derivation, backfill-annotation) and style hybrids via repeated --tag; fixed a slugifyPO double-hyphen bug (sess-20260813-2154-dda37e8b)
- Added Prompt-o-verse 'game sprite' style + ImageMagick chroma-key strip in drainQueue, producing real alpha-transparent sprite PNGs (sess-20260813-2154-dda37e8b)
- emily promptoverse regenerate <slug> --note - client-side counterpart to IDUNA's variants endpoint (sess-20260813-2154-dda37e8b)
- --tag forced add now jumps to the front of the queue (not just appended behind pending items); subject-with-no-count shape added for --tag (sess-20260813-2154-dda37e8b)
- emily promptoverse mashups: LLM-judgment mashup/hybrid detection (Gemini active, Claude parity), internal/mashupjudge, hourly systemd timer, live-verified against real Vertex AI (sess-20260813-2154-dda37e8b)
- README.md Ontology section: requirementizes subject-identity semantics (compositional ambiguity, paraphrase equivalence, definite/indefinite reference, stateless vs stateful, fixed/zero points in time) surfaced while scoping mashup discovery (sess-20260813-2154-dda37e8b)
- emily backup run/decrypt: Google Cloud backup tooling for iduna (client-side AES-256-GCM encrypted)/promptoverse/fatbaby data, secrets/logs excluded from archives, live-verified against gs://project-d24a71e9-2daf-4b2d-917-backups (sess-20260813-2154-dda37e8b)
- fix(iduna): Client.Auth() now fetches a fresh JWT on every call instead of reusing a cached one until its exp claim says <5min left -- fixes UNAUTHENTICATED errors from a token invalidated some other way mid-run (e.g. across an iduna.service restart) (sess-20260813-2154-dda37e8b)

- feat(promptoverse): topic/subject discovery mirroring the entire style-discovery system (marble bag, rare tier, pity, Vertex AI discovery, GPT-2 brainstorm --target subjects, promote-subject). emily promptoverse add <count> (subject omitted) auto-picks via the same weighted selection styles use, or can propose a brand new subject via Vertex AI on a pity-adjusted chance (sess-20260813-2154-dda37e8b)

## 2026-08-17
- fix(promptoverse): vertexGenerateImage now surfaces finishReason/finishMessage instead of a generic 'no image data in response' -- diagnosed a real live failure that looked like a broken API key but was actually Vertex's IMAGE_PROHIBITED_CONTENT filter blocking Rapunzel (Disney IP). New errVertexContentBlocked sentinel: drainQueue skips permanently-blocked items instead of jamming the queue behind them, same treatment as the earlier duplicate-entry fix. (sess-20260813-2154-dda37e8b)
- feat(promptoverse): --slow flag doubles all inter-request/backoff waits for add and work (sess-20260813-2154-dda37e8b)
- feat(promptoverse): new 'brainstorm' subcommand prompts GPT-2 (base checkpoint) with the current style registry as a seed list and parses candidate tags from the completion, review-only, nothing auto-added. feat(promptoverse): add gained --tag <style> to force a specific style (creating + persisting it via Vertex AI if new) as slot 1, filling the rest via the existing dedup/variety selection. (sess-20260813-2154-dda37e8b)
- feat(promptoverse): added Whiteboard, Paper-craft, Anime, Kawaii to the reusable style registry (14 -> 18 total) (sess-20260813-2154-dda37e8b)
- feat(promptoverse): inter-request delay now grows +15s per successful request already made this run (capped +2m) on top of the 20s base, since API overload was still hitting around the 3rd-4th generation with a flat delay; cross-invocation backoff extra now applies to every gap in a run, not just the first request (sess-20260813-2154-dda37e8b)
- feat(promptoverse): adaptive backoff persisted to EMILY/var/promptoverse-backoff.json -- drainQueue checks consecutive-failure history BEFORE its first request of a new run (not just between retries), waiting longer the more times in a row it's recently failed on API overload; --force skips the preemptive wait without disabling the bookkeeping (sess-20260813-2154-dda37e8b)
- tune(promptoverse): bumped default inter-request delay 6s -> 20s (still hitting 429s at 6s), overridable via PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS so future tuning doesn't need a code change (sess-20260813-2154-dda37e8b)
- fix(promptoverse): drainQueue now skips (not stops on) a 409 'already exists' response, so a stale/duplicate queue entry no longer permanently jams every request behind it; appendQueue dedupes on write, closing a race between concurrent add calls. feat(promptoverse): add now makes one attempt to discover a genuinely new style via Vertex AI's Gemini text model when the registry ran short for a subject's 2nd+ generation -- the model can decline rather than being forced to pad with something frivolous. Discovered styles persist to EMILY/var/promptoverse-discovered-styles.json. (sess-20260813-2154-dda37e8b)
- feat(promptoverse): add dedupes against published+queued styles for the exact subject and orders remaining candidates by ascending global usage (least-used styles first) instead of fixed registry order; promoted outer space/underwater/robot/made of candy from the original baseball-card batch into the reusable registry for more variety to draw from (sess-20260813-2154-dda37e8b)
- feat(promptoverse): durable FIFO queue for gen requests -- emily promptoverse add/work now enqueue to EMILY/var/promptoverse-queue.jsonl and drain strictly front-to-back instead of firing Vertex AI calls immediately per invocation, addressing repeated 429s plausibly worsened by unordered concurrent add invocations. Stops (not retries) on failure, preserving remaining queue order; rewrites queue file after every success for crash-safety. emily promptoverse queue lists pending items. (sess-20260813-2154-dda37e8b)

- Added `emily promptoverse add <subject> <count>` + `emily promptoverse styles` — formalizes the
  ad-hoc Python generation scripts written by hand while building Prompt-o-verse VS0 into real CLI
  infrastructure. 10-style reusable registry (only genuine subject-agnostic art styles — tobacco
  card, claymation, Renaissance oil painting, pixel art, LEGO, stained glass, Art Deco, pop art,
  woodcut, watercolor — transformation concepts that only made sense for their original subject,
  like "ice cream novelty," deliberately left out). Generates via Vertex AI's `gemini-2.5-flash-
  image` using this box's existing `gcloud` ADC (no `GEMINI_API_KEY` needed), publishes each result
  to IDUNA's `promptoverse.write` API. New `iduna.Client.PostPromptOVerseNode` +
  `GetPromptOVerseLabels`. 4 new tests (slug normalization, style-registry integrity, bad-arg
  handling). Live-verified: `emily promptoverse add "a red panda" 1` succeeded end to end on the
  first real run, real image published at okemily.com/prompt-o-verse/. (sess-20260813-2154-dda37e8b)

## 2026-08-13

- Added 'emily claire' command -- real but auditable log for CLAIRE.md's entropy/debris concept (git-tracked EMILY/claire-log.md + Apple type 'claire'), not the hidden/unaudited channel the doc's own text describes (sess-20260813-2154-dda37e8b)

## 2026-08-09
- fix(truncate): rune-aware, not byte-slice — the shared truncate() helper (17+ call sites incl. BACKLOG.md INTAKE QUEUE bullets) could cut mid-multibyte-UTF-8-char on any non-ASCII text, corrupting BACKLOG.md and making grep silently treat it as binary on some invocations. Found live while curating SKULDMARK observations today. (sess-20260809-1420-e9d3d7f8)
- fix(git): 讓 emily changelog add 和 BACKLOG.md 自動 commit（curate/promote/archive 共用的 gitCommitBacklog）也在 commit message 裡帶上 session tag，之前只有 CHANGELOG.md 那行文字有標，git commit message 本身沒有 (sess-20260809-1420-e9d3d7f8)

- fix(observe): 修復 emily observe 檔案名稱碰撞導致觀察紀錄靜默遺失的問題（同一秒內多次呼叫會覆寫前一筆），並為 observe/apples/changelog 補上一致的 session 標籤機制 (sess-20260809-1420-e9d3d7f8)

## 2026-08-06
- S143-04 (SAGA agent v0, deterministic parts): `emily saga which-doc-governs <claim-id>`, `emily saga status [doc-id]`, `emily saga conflicts` -- query tools atop the existing claim ledger parser. Governance resolution walks the amends/supersedes graph (partial amendment takes priority over full supersession) to find which doc currently backs a given claim. `conflicts` adds two structural checks lint doesn't already catch as hard errors: amends referencing a claim ID that doesn't exist anywhere, and a claim whose governance has moved to a doc with lower authority than its original owner (e.g. a verified claim now backed only by a draft). Semantic conflict detection stays out of scope (S143-05, NORN-gated). 8 new tests, `go test ./...` clean, live-verified against the real `EMILY/docs/hq-specs` corpus (0 structural conflicts found).

## 2026-08-05
- emily survival logs/status/restart -- first-class CLI support for EINHORN_SURVIVAL (Paper Minecraft server), same systemd --user pattern as shankpit/redgarden

- emily saga gaps --json -- machine-readable output for IDUNA's new Back Office divergence-queue page (sess-20260723-2347-df115bd5)

- emily saga gaps --repo <path> -- saga.manifest.yaml format + CI gaps report (S143-02): claim-without-code (vaporware debt) + code-without-claim (dark matter) detection (sess-20260723-2347-df115bd5)

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

