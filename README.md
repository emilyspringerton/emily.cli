# Emily CLI

A lightweight terminal tool for the Einhorn Industrial agent system.

`emily` is the operator's surface for the Emily OS: post observations into the FatBaby signal pipeline, query the IDUNA Apples log, and see cross-repo system state — without loading a full system prompt or opening a chat session.

**Emily Lite**: super-tokenized, pipe-safe, zero LLM calls. Every command is a direct HTTP call to IDUNA or a local file write.

---

## Quick Start

```bash
# Build and install
cd /home/fatbaby/emily.cli
go build -o ~/.local/bin/emily .

# Post an observation (triggers Claude Code via observation-watcher)
emily observe "eps-processor dropped to 0 articles/hour"
emily observe -s error "jon-agent 504s" --fix "check redis connection pool"

# See what agents have done
emily apples list
emily apples list TYLER
emily apples list -t rsi_iteration --full

# Cross-repo system state
emily status

# Post an Apple directly to IDUNA
emily apples post -t backlog_completion "deployed IDUNA systemd unit"
```

---

## How It Works

```
emily observe "something is wrong"
  → writes JSON to PRRJECT_FATBABY/var/emily-observations/<timestamp>.json
  → observation-watcher sees it (within 10s)
  → invokes Claude Code with observation as context
  → Claude reads codebase, fixes issue, commits
```

The observation pipeline is the same one used by the FatBaby emily-agent. Emily CLI observations are indistinguishable from agent-generated ones — same format, same trigger.

---

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `IDUNA_BASE_URL` | `http://localhost:8080` | IDUNA server URL |
| `IDUNA_AGENT_NAME` | `EMILY-PRIME` | Agent name for Apples auth |
| `IDUNA_AGENT_SECRET` | — | M2M secret (auto-read from `IDUNA/var/agent-secrets.env` if absent) |
| `FATBABY_ROOT` | `/home/fatbaby/PRRJECT_FATBABY` | PRRJECT_FATBABY root directory |

---

## Commands

| Command | What it does |
|---|---|
| `emily observe [flags] <message>` | Post observation to FatBaby pipeline |
| `emily apples list [filter]` | Query IDUNA Apples log |
| `emily apples post -t <type> <title>` | Post Apple to IDUNA directly |
| `emily status` | Cross-repo git + last Apple per agent |
| `emily sync` | Sync FatBaby observations → IDUNA Apples |

Full reference: [`docs/COMMANDS.md`](docs/COMMANDS.md)

Architecture: [`docs/DESIGN.md`](docs/DESIGN.md)

Philosophy: [`docs/NORTHSTAR.md`](docs/NORTHSTAR.md)

---

## Development

Built with the Emily Method — docs first, foundation up, RSI receipts:

1. NORTHSTAR → COMMANDS → DESIGN (docs layer complete)
2. `internal/config` → `internal/iduna` → `internal/obs` (foundation)
3. `cmd/observe` → `cmd/apples` → `cmd/status` (commands)
4. Each build posted as Apple to IDUNA

```bash
go run . observe "testing"
go run . apples list
go run . status
```

---

*emily observe → emily apples list → emily status*
*The terminal is how you talk to the system when the system can't talk to you yet.*
