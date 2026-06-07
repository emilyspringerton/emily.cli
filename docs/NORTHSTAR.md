# Emily CLI — Northstar
## Emily Lite: The Operator's Terminal

*Last updated: 2026-06-07*

---

## Three-Sentence Version

Emily CLI (`emily`) is a lightweight terminal tool for the Einhorn Industrial agent system. It lets a human operator — or an automated process — post observations into the FatBaby signal pipeline, query the IDUNA Apples log, and see cross-repo system state, without loading the full Emily Prime system prompt or spinning up a chat session. It is the terminal half of the operator→agent feedback loop.

---

## What Problem This Solves

The Einhorn Industrial agent system has two feedback loops:

**Loop A: Agent → Observation → Claude Code**
```
PRRJECT_FATBABY emily-agent → var/emily-observations/latest.json → observation-watcher → claude
```

**Loop B: Human → Observation → Claude Code**
```
Human types: emily observe "the eps-processor is eating press releases again"
→ writes structured JSON to PRRJECT_FATBABY/var/emily-observations/
→ observation-watcher sees new file
→ invokes Claude Code with observation as context
→ Claude Code reads codebase, fixes issue, commits
```

Right now Loop B requires the human to manually craft JSON and write it to the right directory. Emily CLI automates that entirely — one command, one observation, Claude gets to work.

**Loop C: Emily CLI → IDUNA Apples → Emily Prime**
```
emily apples list --type signal_observation
→ Emily Prime sees what FatBaby observed in the last 24h
→ Emily Prime issues priority tasks
```

The CLI is not a chat interface. It is a structured command tool — like `git` for the agent system.

---

## Design Principles

**1. Super-tokenized.** No system prompt is loaded, no LLM is called, no streaming. Every command is a direct HTTP call to IDUNA or a local file write. Sub-100ms round trips.

**2. Pipe-friendly.** Every command outputs clean text (default) or JSON (`--json`). Scriptable from cron, observation-watcher, or CI.

**3. Auth from environment.** Reads `IDUNA_BASE_URL`, `IDUNA_AGENT_NAME`, `IDUNA_AGENT_SECRET` from env. Auto-sources `IDUNA/var/agent-secrets.env` if present and vars are missing. No config files required.

**4. Observation-first.** The primary use case is Loop B above: human posts observation, Claude Code picks it up. Everything else is supporting infrastructure.

**5. PRRJECT_FATBABY-native.** Observations write to the same `var/emily-observations/` directory that the FatBaby emily-agent uses. Observation-watcher picks up Emily CLI observations exactly as it picks up agent-generated ones — same trigger, same path.

---

## Architecture

```
emily (binary)
├── observe <message>       → writes to PRRJECT_FATBABY/var/emily-observations/
│                             (triggers observation-watcher → claude)
├── apples
│   ├── list [filter]       → queries IDUNA /api/v1/apples
│   └── post <type> <title> → posts Apple to IDUNA as EMILY-PRIME
├── status                  → cross-repo git + last Apple per agent
└── sync                    → runs sync-fatbaby.sh (FatBaby obs → IDUNA)
```

### Auth flow

```
emily apples list
  → read IDUNA_BASE_URL, IDUNA_AGENT_NAME, IDUNA_AGENT_SECRET from env
  → if missing, try source IDUNA/var/agent-secrets.env
  → POST /api/v1/auth/agent → ES256 JWT
  → GET /api/v1/apples?limit=20
  → print table
```

### Observation flow

```
emily observe "something is wrong"
  → resolve PRRJECT_FATBABY root (env FATBABY_ROOT or default /home/fatbaby/PRRJECT_FATBABY)
  → write JSON to var/emily-observations/<timestamp>.json
  → symlink latest.json → that file
  → print: ✓ Observation written — observation-watcher will pick it up within 10s
```

---

## What Emily CLI Is Not

- Not a chat interface (use `claude` or the emily-agent web UI for that)
- Not a deployment tool (use `systemd`, `cron`, `kubectl`)
- Not a replacement for Emily Prime (she still runs the agent loop)
- Not a log viewer (use `tail -f var/logs/*.log` for that)

---

## The Name

"Emily Lite" is the working name. The binary is `emily`. When someone types `emily observe`, they are sending a signal into the Emily OS — the same system that runs the TV show's universe engine, the IDUNA governance layer, and the FatBaby signal pipeline. The CLI is the operator's surface. Emily is the OS underneath.

---

## RSI Model

Emily CLI is itself an RSI artifact. Its NORTHSTAR drives implementation. Its implementation generates FatBaby observations. Those observations trigger Claude Code to improve the CLI. The CLI improves itself by being used.

This is intentional.

---

*Emily CLI Northstar | docs first, build second, analyze third*
*The terminal is how you talk to the system when the system can't talk to you yet.*
