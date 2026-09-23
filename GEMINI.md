# GEMINI.md — Guidance for Gemini / Antigravity in emily.cli

## What This Is

`emily` is the operator CLI terminal for EINHORN_INDUSTRIAL. All interaction with IDUNA, obs-watcher, the TUI, agent statuses, and the session engine flows through `emily`.

## Build and Installation

```bash
# Build binary
go build -o emily .

# Install system-wide
sudo cp emily /usr/local/bin/emily

# Run tests
go test ./...
```

## Key Commands

- `emily session new` / `emily session current`: Generates and retrieves the active session fingerprint (`sess-YYYYMMDD-HHMM-<hex>`).
- `emily observe <msg>`: Posts an observation to the FatBaby pipeline (IDUNA + signal file), auto-tagged with active session.
- `emily apples post -t <type> <title>`: Files an Apple directly to IDUNA.
- `emily apples list [filter]`: Queries IDUNA Apples log.
- `emily status`: Inspects cross-repo git and service health.
- `emily tui`: Full Bloomberg-style terminal UI.
- `emily backlog curate`: Curates FatBaby observations into `EMILY/BACKLOG.md`.

## Key Environment Variables

- `EMILY_ROOT`: Path to `EMILY` repo root (e.g. `/home/garybifrost/EMILY`).
- `IDUNA_BASE_URL`: IDUNA endpoint (default: `http://localhost:8080`).
- `IDUNA_AGENT_NAME`: Calling agent identity (`EMILY_PRIME`).
- `IDUNA_AGENT_SECRET`: M2M authentication credential.

## Operating Protocols (The Emily Way)

1. **Observations First**: Route founder instructions through `emily observe` before implementing.
2. **Apples**: File Apples to track meaningful completions.
3. **Commit Protocol**:
   - Every commit must include the active `session: <tag>` trailer (`emily session current`).
   - Push immediately to upstream upon completion (`git push origin main`).
