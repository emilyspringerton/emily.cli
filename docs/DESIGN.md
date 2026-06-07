# Emily CLI — Design Document
## Architecture, Data Flow, and Implementation Notes

*Last updated: 2026-06-07*

---

## Package Structure

```
emily.cli/
├── main.go                    # entrypoint: parse global flags, dispatch subcommand
├── go.mod                     # module: github.com/emilyspringerton/emily-cli
├── go.sum
│
├── docs/
│   ├── NORTHSTAR.md           # why this exists, design principles
│   ├── COMMANDS.md            # full command reference (the spec)
│   └── DESIGN.md              # this file — architecture and implementation notes
│
├── internal/
│   ├── config/
│   │   └── config.go          # env var resolution, secrets file parser
│   ├── iduna/
│   │   └── client.go          # IDUNA HTTP client (auth + apples + agents)
│   └── obs/
│       └── writer.go          # observation writer (PRRJECT_FATBABY var/ format)
│
└── cmd/
    ├── observe.go             # emily observe
    ├── apples.go              # emily apples list|post
    └── status.go              # emily status
```

---

## Data Flows

### 1. emily observe

```
CLI args → obs.Writer.Write(ObservationPayload)
        → generates RFC3339 filename
        → marshals JSON to var/emily-observations/<ts>.json
        → atomic symlink: latest.json → new file
        → prints confirmation

PRRJECT_FATBABY observation-watcher:
        → polls latest.json every 10s
        → detects new timestamp (content hash)
        → invokes: claude --dangerously-skip-permissions "<obs prompt>"
        → Claude reads codebase, fixes issue, commits
```

The observation JSON schema (must match PRRJECT_FATBABY format):

```json
{
  "timestamp": "2026-06-07T02:40:00Z",
  "summary": "short description (becomes Apple title if synced)",
  "severity": "info|warn|error",
  "findings": "detailed free-text analysis (optional)",
  "suggested_fix": "what to do about it (optional)"
}
```

**Source**: `internal/obs/writer.go`

### 2. emily apples list

```
config.Resolve() → gets IDUNA_BASE_URL, IDUNA_AGENT_NAME, IDUNA_AGENT_SECRET
iduna.Client.Auth() → POST /api/v1/auth/agent → JWT cached in-process
iduna.Client.ListApples(limit, filters) → GET /api/v1/apples?limit=N[&source_repo=X]
format.PrintApples(apples, flags) → table or JSON
```

**Source**: `cmd/apples.go`, `internal/iduna/client.go`

### 3. emily apples post

```
config.Resolve()
iduna.Client.Auth()
iduna.Client.PostApple(ApplePayload) → POST /api/v1/apples
print: ✓ Apple #N filed
```

**Source**: `cmd/apples.go`, `internal/iduna/client.go`

### 4. emily status

```
parallel:
  status.GitRepos() → git log + git status + BACKLOG.md parse for 5 repos
  status.IDUNAApples() → iduna.Client.ListApples(limit=50) → group by source_repo
format.PrintStatus(gitState, appleState)
```

**Source**: `cmd/status.go`

---

## IDUNA Client Design

The `internal/iduna` client is intentionally minimal — NOT imported from the emily-agent package. Dependencies in `go.mod` are zero (only stdlib). This keeps the binary small and the build fast.

```go
// internal/iduna/client.go

type Client struct {
    BaseURL     string
    AgentName   string
    AgentSecret string
    httpClient  *http.Client
    token       string
    tokenExp    time.Time
}

type ApplePayload struct {
    AppleType  string `json:"apple_type"`
    Title      string `json:"title"`
    Body       string `json:"body,omitempty"`
    SourceRepo string `json:"source_repo"`
    RunID      string `json:"run_id,omitempty"`
}

type Apple struct {
    ID         int64  `json:"id"`
    AgentID    string `json:"agent_id"`
    SourceRepo string `json:"source_repo"`
    RunID      string `json:"run_id"`
    AppleType  string `json:"apple_type"`
    Title      string `json:"title"`
    Body       string `json:"body,omitempty"`
    RecordedAt string `json:"recorded_at"`
}

// Methods: Auth(), PostApple(), ListApples()
```

### Token caching

The client caches the JWT in-process and re-authenticates when `time.Until(exp) < 5m`. For a CLI binary that runs and exits, this means one auth call per invocation. That's fine.

---

## Config Resolution

```go
// internal/config/config.go

type Config struct {
    IDUNABaseURL    string
    IDUNAAgentName  string
    IDUNAAgentSecret string
    FatBabyRoot     string
    EmilyRoot       string
}

func Resolve() (*Config, error) {
    // 1. Read env vars
    // 2. If IDUNA_AGENT_SECRET missing, parse IDUNA/var/agent-secrets.env
    //    as text (not shell source) — look for IDUNA_SECRET_<NAME> line
    // 3. Return populated Config or error
}
```

The secrets file parser (step 2) reads lines of the form:
```
export IDUNA_SECRET_EMILY_PRIME=abc123...
```
and extracts the value for the configured agent name. This works without `source` and is safe from injection.

---

## Observation Writer

```go
// internal/obs/writer.go

type ObservationPayload struct {
    Timestamp    string `json:"timestamp"`
    Summary      string `json:"summary"`
    Severity     string `json:"severity"`
    Findings     string `json:"findings,omitempty"`
    SuggestedFix string `json:"suggested_fix,omitempty"`
}

func Write(root string, payload ObservationPayload) (path string, err error) {
    // 1. MkdirAll root/var/emily-observations/
    // 2. fname = RFC3339 timestamp (colons replaced with dashes for fs compat)
    // 3. Write JSON to root/var/emily-observations/<fname>.json
    // 4. Atomic symlink: root/var/emily-observations/latest.json → new file
    // 5. Return written path
}
```

**Atomic symlink pattern** (avoids observation-watcher race):

```go
tmpLink := path + ".tmp"
os.Symlink(fname+".json", tmpLink)
os.Rename(tmpLink, latestPath) // atomic on Linux
```

---

## Output Formatting

All commands support `--json` flag for machine-readable output.

Default output is human-readable with:
- `◈` prefix on section headers (Emily OS visual identity)
- Fixed-width columns for table data
- `✓` / `✗` for success/failure

No color by default (stdout pipe-safe). Color opt-in: `--color` (or `EMILY_COLOR=1`).

---

## Error Handling

```
exit 0  success
exit 1  bad flags / missing required args  (print usage)
exit 2  IDUNA auth failure                 (print: IDUNA not reachable at <url>)
exit 3  observation write failure          (print: cannot write to <path>)
exit 4  IDUNA API error                    (print: API error <status>: <message>)
```

Errors print to stderr. Normal output to stdout. This makes `emily apples list | jq` work without interference.

---

## Build and Install

```bash
cd /home/fatbaby/emily.cli
go build -o ~/.local/bin/emily .
```

No CGO. Single static binary (aside from libc on Linux).

### Cross-compile (for server deployment)

```bash
GOOS=linux GOARCH=amd64 go build -o emily-linux-amd64 .
```

---

## RSI Loop for This Codebase

Emily CLI is self-referential: `emily observe` can file observations about Emily CLI itself.

```bash
emily observe -s warn "emily status is missing the IDUNA Apple count in the git section"
# → observation-watcher invokes Claude Code
# → Claude reads emily.cli/ source
# → Claude fixes emily status output
# → Claude commits
# → Emily CLI improves itself
```

The loop is: `emily observe` → observation-watcher → claude → fix → commit → `emily status` shows improvement.

This is the RSI model applied to the CLI's own development.

---

*Emily CLI Design | zero stdlib deps, pipe-safe, observation-first*
*Build from the foundation: config → client → observe → apples → status*
