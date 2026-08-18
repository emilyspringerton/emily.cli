# emily — Emily Lite CLI

Operator terminal for the Einhorn Industrial agent system. Zero LLM calls, -zero external
dependencies-. Every command is a direct HTTP call to IDUNA or a local file write.

**Emily Lite**: super-tokenized, pipe-safe, stdin-aware, color-optional.

---

## Install

```bash
cd /home/fatbaby/emily.cli
./scripts/build.sh        # build + 69 tests + install to ~/.local/bin/emily
```

Or manually: `go build -o ~/.local/bin/emily .`

---

## Commands

### Observe — fire an observation into the FatBaby pipeline

```bash
emily observe "eps-processor dropped to 0 articles/hour"
emily observe -s error "jon-agent 504s" --fix "check redis connection pool"
emily observe -s warn "latency spike" --findings "p99 > 2s since 06:00"
git log -1 --oneline | emily observe -s info          # stdin input
emily observe --dry-run "preview without writing"
```

The observation-watcher picks it up within ~10s and invokes Claude Code on FatBaby.
Auto-posts a `signal_observation` Apple to IDUNA as a receipt (requires `IDUNA_AGENT_SECRET`).

### Apples — query and post to the IDUNA log

```bash
emily apples list                        # last 20 apples
emily apples list TYLER -n 5            # last 5 from TYLER repo
emily apples list -t rsi_iteration --full  # with body text
emily apples get 44                     # full body of Apple #44
emily apples post -t backlog_completion "deployed IDUNA unit"
cat report.txt | emily apples post -t rsi_iteration "Build 0020"
```

### Watch — IDUNA tail -f

```bash
emily watch               # all repos, 5s poll
emily watch TYLER         # filter by repo
emily watch --interval 2  # 2s poll
```

Bootstraps at current highest Apple ID — only new apples are shown. Ctrl-C to stop.

### Status — cross-repo system snapshot

```bash
emily status              # one-shot: git + IDUNA
emily status --json       # machine-readable JSON
emily status --watch      # live dashboard, refreshes every 30s
emily status --watch --interval 10 --no-git
```

### Sync — FatBaby observations → IDUNA

```bash
emily sync                          # sync up to 10 new obs files
emily sync --all --dry-run         # preview everything
emily sync --watch                 # daemon: auto-post as obs files appear
emily sync --watch --interval 5
```

State-tracked in `EMILY/var/fatbaby-synced.txt` to avoid double-posting.

### Agents — agent activity dashboard

```bash
emily agents              # all agents, last 200 apples scanned
emily agents --since 60   # only agents active in last hour
emily agents --json
```

Synthesized from the Apples log — shows last-seen age, total count, last apple type/title.

### Prime Task — direct Claude on FatBaby from the CLI

```bash
emily prime-task "add test for eps-processor Q1 edge case"
emily prime-task --priority high --type improve_signal \
  --criteria "go test ./... passes" --criteria "committed to git" \
  "entity-graph parser misses director names with Jr. suffix"
emily prime-task --dry-run "preview without writing"
```

Writes a task JSON to `EMILY/signals/tasks/`. The observation-watcher picks it up within
10s and invokes Claude Code on the FatBaby repo with the task as its prompt. Closes the
`operator → CLI → EMILY/signals/tasks → obs-watcher → Claude on FatBaby` loop.

### Install — wire up daemons

```bash
emily install --cron              # print recommended crontab entries
emily install --cron --write      # install into crontab
emily install --systemd           # generate systemd user unit
emily install --systemd --write   # install to ~/.config/systemd/user/
```

### Start — launch the Emily OS agent stack

```bash
emily start                # observation-watcher + emily-agent RSI daemon, detached
emily start --iduna        # also starts iduna.service via systemctl --user
emily start --dry-run      # show what would be started
```

### TUI — full-terminal dashboard

```bash
emily tui               # 3-column Bloomberg-style dashboard: roadmap | tasks | health
emily tui --fatbaby     # pre-activate the FatBaby panel in column 3 ('b' toggles at runtime)
```

### Backlog — curate observations into BACKLOG.md

```bash
emily backlog promote            # curate uncurated FatBaby observations into the INTAKE QUEUE
emily backlog curate --all       # pull everything uncurated
emily backlog add <section> <item>       # append an item to a section
emily backlog add-section <title>        # open a new SECTION
emily backlog done <item-id>             # mark an item [x]
emily backlog archive / compress         # DONE.md housekeeping
```

### Changelog — per-repo CHANGELOG.md entries

```bash
emily changelog add <repo> "<what changed>"
```

### Session — cross-context session fingerprint

```bash
emily session new        # mint a fresh sess-YYYYMMDD-HHMM-<8hex> tag
emily session current    # print the active tag
```

Auto-stamped as `run_id`/`session:` on every `apples post`, `changelog add`, and `observe` call.

### Key — store credentials in the CLI's env

```bash
emily key set GITHUB_TOKEN <token>          # target: this box's default env file
emily key set NAME VALUE --target iduna     # or --target emily
emily key set NAME VALUE --file <path>      # explicit file override
emily key show NAME
emily key unset NAME
```

### Prompt-o-verse — generate + publish gallery nodes

```bash
emily promptoverse add [<subject>] <count> [--force] [--slow] [--tag <style>]...
                       [--annotation "text" | --annotation-from-lore] [--annotation-alias NAME]
                                                      # queue <count> styles, then drain
emily promptoverse work [--force] [--slow]           # drain whatever's already queued (resume after a 429)
emily promptoverse queue                             # list pending queue entries, oldest first
emily promptoverse requeue                           # re-pick styles for everything still queued (skips --tag-forced items)
emily promptoverse styles                            # list the reusable style registry
emily promptoverse brainstorm [--target styles|subjects] [--seed "a, b, c"] [--sample N]   # prompt GPT-2 for candidates
emily promptoverse promote <label> [--rare]          # turn a candidate/name into a real persisted style
emily promptoverse promote-subject <label> [--rare]  # turn a candidate/name into a real known subject
emily promptoverse mashups [--target subjects|styles] [--provider gemini|claude|all]  # LLM-judge genuine subject+subject mashups
emily promptoverse regenerate <slug> --note "..."    # additive "regenerate with variation" -- new variant, same leaf page, never overwrites
emily promptoverse annotations [set|clear] ...       # manage subject-level prompt annotations
emily promptoverse backfill-annotation <subject>     # mark already-published nodes as pre-annotation
```

**Style hybrids** — passing `--tag` more than once does *not* force N separate generations. It
combines the tags into **one new blended style** instead (`emily promptoverse add Medusa --tag
kawaii --tag FFXI` creates one "kawaii × FFXI" style and generates exactly one Medusa image in it,
tagged `style_hybrid_of="kawaii, FFXI"` on the published node — visible via the same generic tag
table every node already renders). This is deliberately named "hybrid," not "mashup" — `mashups`
above already means two *subjects* combined; a hybrid combines two *styles*.

**Subject annotations** — for a subject whose bare name collides with real third-party IP (e.g.
"Paimon": TYLER's own Goetia-king hero vs. Genshin Impact's companion character, which risked
erroneous content-policy blocks), an annotation sticks to the *subject* and is appended only to the
real generation prompt sent to Vertex AI — never to the EZ prompt, taxonomy, or slug, which all
stay exactly "Paimon" forever. `--annotation-from-lore` auto-derives disambiguating text from
TYLER's hero compendium (`multiverse_heroes.md`) + its Goetia frequency table. A subject can carry
more than one named alias (`emily promptoverse annotations set Paimon --alias genshin-impact
--text "..."`), selectable per batch via `--annotation-alias` without ever forking the subject.
`backfill-annotation <subject>` retroactively marks nodes published before an annotation existed.

Requests are queued FIFO to a durable file (`EMILY/var/promptoverse-queue.jsonl`), not fired
immediately — `add` enqueues then drains; if a drain is already mid-flight or queued, new
requests wait their turn in arrival order. Draining stops (not retries) on a rate limit, leaving
the remainder queued for `emily promptoverse work` later. 20s between successful requests by
default — override with `PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS`. That gap also grows +15s per
successful request already made *this run* (capped at +2m) — the API's real limit behaves more
like requests/minute than seconds-since-last-request, so a flat delay alone wasn't holding up past
the 3rd or 4th generation in a run. Requires `gcloud` ADC authenticated on this box and
`IDUNA_AGENT_SECRET` for an agent with `promptoverse.write`.

`add` deduplicates: it skips any style already published *or* already queued for that exact
subject. What's left is picked by weighted random sampling *without* replacement — a "marble bag"
weighted 1/(usage+1) — so under-used styles are more *likely*, never guaranteed, and a heavily-used
style still gets an occasional pick. (An earlier version used a strict ascending-usage sort, which
always filled in whichever single style had the globally lowest count — great until that style ran
out of gas, at which point it never came back and a merely under-used style stayed just as starved
as an over-used one. `emily promptoverse requeue` re-picks every currently-queued, non-`--tag`-forced
item with the current logic — and every `add`/`work` run does this automatically before draining
anyway, so a queue that's been sitting for a while never drains stale picks; run `requeue` by hand
only if you want to force a refresh without also draining.) A handful of styles judged too subject-specific to compete for a slot every time (ice
cream novelty, 1990s glossy rookie card, 2020s Topps Chrome refractor) are excluded by default, but
the whole group gets one per-run roll to become eligible anyway — pity-adjusted (Fibonacci-scaled)
so a long drought closes out fast rather than staying locked out. If every eligible registry style
is already used for a subject, it reports that and queues nothing.  Duplicate/stale queue entries
(e.g. from a race between two concurrent `add` calls, or left over from before this dedup existed)
no longer jam the drain permanently — a "node already exists" response from IDUNA is skipped, not
treated as a fatal failure.

On the 2nd+ generation for a subject (never the first — a brand-new subject only uses the existing
registry), if the registry ran short, `add` makes one attempt to discover a genuinely new style via
Vertex AI's Gemini text model, using this box's existing `gcloud` ADC (no separate credential) —
weighing whether the subject itself has a well-known iconic style not yet in the registry (e.g.
"Aphrodite" suggesting "ancient Greek marble statue") as a real reason to propose something. Even
when the batch is already full, there's a separate low, pity-adjusted chance a brand new style
emerges anyway, swapped in for a slot — not just when `--tag` is used. The model can decline if
nothing non-frivolous comes to mind — that's the common, expected outcome, not padding for
padding's sake. Anything it does propose is persisted to
`EMILY/var/promptoverse-discovered-styles.json` and becomes part of the registry for every future
subject, not just the one that triggered it.

Adaptive backoff: consecutive API-overload failures are tracked in
`EMILY/var/promptoverse-backoff.json`. The NEXT run consults that state *before* its first
request, not just between retries mid-drain — three separate invocations in a row that each hit a
429 make the third one preemptively wait longer, scaling with the streak (capped at 5 minutes,
linear 30s/failure). A failure older than 15 minutes doesn't count against a fresh attempt. Any
success resets the streak. That same extra wait is also added to every gap for the rest of a run,
not just the first request. `--force` skips all of that for one run without turning off the
tracking. `--slow` doubles every wait the command applies (base delay, growth, and any backoff
extra) — orthogonal to `--force`, which zeroes the backoff extra before `--slow` doubles whatever
is left.

`--tag <style>` forces one specific style into the batch, whether or not it's already in the
registry — `emily promptoverse add princess 4 --tag gladiator` forces "gladiator" as slot 1
(creating and persisting it via Vertex AI if it isn't already a known style, so it becomes
reusable for every future subject too), then fills the other 3 through the normal
deduped/variety-weighted selection. A tag that would duplicate what's already published/queued
for the subject is ignored rather than force-added — dedup still wins.

`emily promptoverse brainstorm` is a separate, standalone tool from the Vertex-based discovery
above: it prompts GPT-2 (base checkpoint recommended — `emily gpt2 start --model base`; the
fine-tuned checkpoint drifts into prose almost immediately) with a random *sample* of the current
registry (`--sample`, default 5 — a different subset each run nudges completions in different
directions instead of the same always-full seed producing similar output every time; `--seed`
overrides with an exact custom list) and parses whatever plausible short tags come out of the
completion. Nothing is added to the registry automatically — it's a review-only brainstorming aid,
since base GPT-2 has no instruction-following and the vocabulary it drifts toward has no reason to
stay on-topic. Anything parsed and not already in the registry is saved to
`EMILY/var/promptoverse-candidate-tags.json` for later review.

`emily promptoverse promote <label> [--rare]` turns a candidate (or any arbitrary name) into a
real, persisted style — same Vertex AI template-writing path `--tag` uses for a name that doesn't
exist yet, just invoked directly instead of as a side effect of `add`. `--rare` marks it for the
same "sometimes, not always" treatment as the hardcoded rare tier. Matching candidate records get
marked promoted rather than deleted, so there's a durable trail of what came from brainstorm and
what happened to it.

**Subject/topic discovery** mirrors the entire style system above, applied to *subjects* instead —
one real difference: a subject is just a string (no Kind/Template to author), so there's no
hardcoded starter list the way styles needed one; the pool is every subject any published node has
ever used, plus anything discovered/promoted since.

- `emily promptoverse add <count>` (subject omitted) auto-picks one via the same weighted "marble
  bag" as styles — under-used subjects more likely, never guaranteed — and can also propose a
  brand new subject via Vertex AI on a pity-adjusted chance, exactly like styles' spontaneous
  discovery, even when the pool already has candidates.
- Subjects marked rare (via `promote-subject --rare`) are excluded by default with the same
  pity-adjusted group roll rare styles get.
- `emily promptoverse brainstorm --target subjects` seeds GPT-2 from a random sample of real used
  subjects instead of styles, saving candidates (`Kind: "subject"`) to the same
  `promptoverse-candidate-tags.json` file styles use (deduped independently per kind, so a style
  and a subject can share a literal name without colliding).
- `emily promptoverse promote-subject <label> [--rare]` turns a candidate/name into a real known
  subject, persisted to `EMILY/var/promptoverse-discovered-subjects.json`.

### Context / Northstar — golden-doc tooling

```bash
emily context build              # compile all Tier 1 golden docs → EMILY/context/full-system-context.md
emily northstar <repo>           # print <repo>/docs/NORTHSTAR.md (or docs2/NORTHSTAR.md)
```

### Chat — terminal chat with Emily Prime

```bash
emily chat                       # calls claude-haiku directly, no server
emily chat --model <model> --session <file>
```

### GPT-2 — Emily Prime inference stack

```bash
emily gpt2 start [--port N] [--model ft|base] [--dry-run]
emily gpt2 proxy
emily gpt2 status
emily gpt2 tokenizer
emily gpt2 generate "<prompt>"    # alias: gen
emily gpt2 health
```

### Train — GPT-2 fine-tuning pipeline

```bash
emily train build-dataset [--emily-root <path>] [--output <path>] [--mode lm|instruct]
emily train upload <file> [<file>...]
emily train status
emily train stats
emily train run-local
```

### Vault — founder-only password manager (loopback-only)

```bash
emily vault init
emily vault unlock / lock / status
emily vault add <name> / get <name> / list / delete <name>
```

### Backup — cloud archive for IDUNA / Prompt-o-verse / fatbaby data

```bash
emily backup run                          # archive + upload all targets to GCS
emily backup run --target iduna           # iduna | promptoverse | fatbaby | all
emily backup decrypt <encrypted-file> <output-file>
```

Tars an allowlisted set of paths per target and uploads via `gcloud storage cp` to
`gs://project-d24a71e9-2daf-4b2d-917-backups` (us-central1, 30-day retention lifecycle):

| Target | Paths | Encrypted? |
|---|---|---|
| `iduna` | IDUNA's SQLite stores (`var/*.db` — auth, Apples, blog/tyler, promptoverse gallery DB, vault, mailing list) | Yes, AES-256-GCM |
| `promptoverse` | Rendered gallery (images + HTML) + JSON state (queue, discovered styles/subjects, candidates, dead-letters, pity, backoff) | No |
| `fatbaby` | Curated cross-repo state (`BACKLOG.md`, `EMILY/var`, `PRRJECT_FATBABY/var`) | No |

The `iduna` target's AES-256-GCM key lives at `IDUNA_ROOT/var/backup-encryption.key` (0600,
generated on first use) and is **never uploaded alongside the backups it protects** — losing that
file makes every existing encrypted IDUNA backup permanently unrecoverable. Back it up yourself,
somewhere else (password manager, a second machine) — this tool cannot do that part for you.
`*.env`/credentials are deliberately excluded from every target, even encrypted ones.

### Memory — Emily Prime's observation digest

```bash
emily memory digest         # print the obs-digest from emily-memory/ in TUI format
emily memory consolidate
```

### Claire — uncompressed subconscious log

```bash
emily claire "<entry>"      # append to CLAIRE.md — tech debt, failed approaches, env quirks
```

### Saga — documentation curation lifecycle (HQ-SPEC-DOC-102)

```bash
emily saga lint                    # frontmatter schema checks
emily saga gaps
emily saga which-doc-governs <path>
emily saga status
emily saga conflicts
```

### IDUNA — account tooling

```bash
emily iduna create-account          # mint a disposable DragonsNShit test account
```

### EmilyOS / Shankpit / Survival / Redgarden / Gsync — per-repo ops helpers

```bash
emily emilyos                          # EmilyOS policy kernel helper
emily shankpit status|players|kick|observe|restart|leaderboard
emily survival logs|status|restart     # EINHORN_SURVIVAL Minecraft server
emily redgarden bots|status            # REDGARDEN bot ops
emily gsync                            # Google Drive / gsync helper
```

### Help

```bash
emily help observe
emily help apples
emily help watch
emily help status
emily help sync
emily help agents
emily help prime-task
emily help <any other command above>
```

---

## Ontology

Prompt-o-verse subject labels look like plain strings, but subject *identity* — whether two labels
name "the same subject," and whether one subject is a genuine compositional mashup of others — is
not a fixed lexical property of the string. It's context-dependent, and got tested against real
examples (2026-08-18, founder real-time) while scoping mashup/hybrid discovery
(`EMILY/docs/NORTHSTAR_PROMPT_O_VERSE.md` §9, `EMILY/BACKLOG.md` S176-29). Recorded here because
it generalizes past that one feature — any future subject-comparison logic in this codebase should
read this first, not re-derive it the hard way:

- **Compositional ambiguity.** "tuxedo duck" is plausibly a real duck breed/color-morph name — a
  single concept — not a mashup of "tuxedo" (clothing) and "duck" (animal), even though it's
  lexically a subset of a broader "tuxedo duck" superstring. A subject containing another
  subject's words is not proof it's *composed of* that subject.
- **Paraphrase equivalence, and its limits.** "tuxedo duck" and "a duck wearing a tuxedo" are the
  same subject despite sharing almost no words. "tuxedo duck" and "duck tuxedo" are *not* the same
  subject despite sharing all their words, just reordered. Lexical similarity (shared words, word
  order, edit distance) is neither necessary nor sufficient for subject equivalence.
- **Definite vs. indefinite reference.** "a president wearing a tuxedo" is usually different from
  "the president wearing a tuxedo" — the definite article can pin the subject to one specific
  real-world referent, where the indefinite article (or no article) leaves it generic. This isn't
  a stopword to normalize away; dropping it can silently merge two subjects that were never the
  same thing.
- **Stateless vs. stateful execution changes the right answer.** A stateless judgment — one
  subject label in, one verdict out, no memory of anything else — cannot recover information a
  stateful one has available: what else is in the current subject registry, what a subject was
  intended to mean when it was generated, what's already been asked in the same session. Whether a
  given piece of subject-identity logic is built stateless or stateful is a real design decision,
  not an implementation detail — defaulting to stateless because it's simpler will get context-
  dependent cases like the ones above wrong.
- **Fixed points and zero points in time.** Some referents drift with real-world state: "the
  president" resolves to a specific person *as of whenever the query is evaluated* — asking again
  years later, after (for example) a different person becomes president, does not reasonably
  render a similar response even though the label string hasn't changed. A subject's identity can
  need a **fixed point** — an explicit anchor to the moment it was generated or intended — to stay
  stable over time; without one, it has an implicit, un-anchored **zero point** (evaluate "now,"
  whenever "now" happens to be) and will silently drift. Whether a given subject should be pinned
  to a fixed point or left floating is a per-subject design choice, not a global default.

The fixed-point/zero-point distinction matters most exactly where this section started: hybrid
subject representation. A mashup like "Fractal Raccoon" is itself a compound referent, and whether
*it* stays the same subject over time depends on the same anchoring question its components do —
if "Fractal" or "Raccoon" independently drift (new discovered meaning, real-world referent change),
does the hybrid drift with them, or was it meant to freeze at the moment it was composed? Not
answered here; flagged as part of the same requirement, not a separate one.

Real, already-live example of the same drift, one layer down: a Rapunzel + ice-cream-novelty-style
query was blocked outright by Vertex AI's own content policy (`IMAGE_PROHIBITED_CONTENT`, recorded
in the content-policy dead-letter dataset — S176-23, `EMILY/var/promptoverse-content-blocked.jsonl`).
Founder: "thats a highly time context sensitive one [...] depending on the platform training [...]
current trademarks etc." Whether a given subject is even *generatable* at all is itself a
zero-point-evaluated judgment — it depends on the generation platform's training data and current
trademark/IP state at query time, not a fixed property of the label "Rapunzel." The same subject
could plausibly succeed or fail identical queries at different points in time for reasons that have
nothing to do with the taxonomy and everything to do with an external, drifting fixed point. Put
concretely, with a real horizon on it — founder: "disney owning rapunzel icecream in 2026 may not
at all be that way in 2046." Twenty years is not a hypothetical timescale for a durable taxonomy
asset (§6 of the northstar names the dataset itself, not just the gallery, as the product); the
trademark/IP state a content-policy decision depends on today is not guaranteed to hold for the
lifetime of the data this system is building.

Everything above was worked out against *subjects*, but the same ambiguities are plausible for
*styles*/tags too (paraphrase equivalence, compositional vs. coincidental overlap, drift over
time) — worth naming so it isn't rediscovered from scratch later, but deliberately **not** scoped
or built here. Deferred, same as the subject-side work.

None of this is resolved into working code yet — it's exactly why the mashup/hybrid-discovery
feature (`NORTHSTAR_PROMPT_O_VERSE.md` §9) was scoped and then explicitly deferred rather than
shipped as a lexical string-matching rule. Any future attempt at automated subject comparison,
dedup, or mashup detection should treat this section as the requirements it has to satisfy, not
just prior art to skim.

---

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `IDUNA_BASE_URL` | `http://localhost:8080` | IDUNA server |
| `IDUNA_AGENT_NAME` | `EMILY-PRIME` | Agent name for auth |
| `IDUNA_AGENT_SECRET` | — | M2M secret (auto-read from `IDUNA/var/agent-secrets.env`) |
| `FATBABY_ROOT` | `/home/fatbaby/PRRJECT_FATBABY` | Observation pipeline root |
| `EMILY_ROOT` | `/home/fatbaby/EMILY` | EMILY repo root (for prime-task, sync state) |
| `EMILY_COLOR` | — | Set to `1` for ANSI color output |

---

## Architecture

Three feedback loops:

```
Loop A (agent → claude):
  emily-agent writes obs file → observation-watcher → Claude Code fixes FatBaby

Loop B (operator → claude, via observe):
  emily observe "msg" → obs file → watcher → Claude Code

Loop C (operator → claude, via prime-task):
  emily prime-task "task" → EMILY/signals/tasks/ → watcher → Claude Code on FatBaby
```

Full docs: [`docs/NORTHSTAR.md`](docs/NORTHSTAR.md) · [`docs/COMMANDS.md`](docs/COMMANDS.md) · [`docs/DESIGN.md`](docs/DESIGN.md)

---

## Development

```bash
./scripts/build.sh              # build + tests + install
./scripts/build.sh --no-install # CI mode
go test ./...                   # unit tests only
EMILY_COLOR=1 go test ./internal/color/...  # color-mode tests
```

Tests: 236 across 5 packages (`cmd`, `internal/config`, `internal/iduna`, `internal/obs`, `internal/color`).

---

*docs/NORTHSTAR.md — the philosophy*
*The terminal is how you talk to the system when the system can't talk to you yet.*
