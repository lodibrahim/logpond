# logpond — Lightweight TUI Log Viewer with Built-in MCP Server

## Overview

logpond is a standalone Go binary that reads JSON log lines from stdin, displays them in a configurable TUI table, and exposes them for AI querying via an MCP (Model Context Protocol) server. It is generic — any project producing JSON logs can use it with a YAML config file.

**Problem:** Debugging structured log output requires either copy-pasting logs into AI assistants or using heavy observability platforms. There is no lightweight local tool that both displays logs and lets AI agents query them.

**Solution:** A single binary (~1,200 lines) that pipes in JSON logs, renders a filterable table, and serves an MCP endpoint on localhost — so both the developer and Claude can query the same live log stream.

---

## Architecture

```
stdin (JSON logs)
  ↓
┌─────────────────────────────────────────┐
│ logpond                                  │
│                                          │
│  Reader ──→ Parser ──→ Store (ring buf)  │
│                           ↑       ↓      │
│                        Search   TUI      │
│                           ↑              │
│                      MCP Server          │
│                   (localhost:9876)        │
└─────────────────────────────────────────┘
```

Five components:

| Component | Responsibility |
|-----------|---------------|
| **Reader** | Reads lines from stdin, sends to parser |
| **Parser** | Parses JSON using YAML config, extracts fields into structured entries |
| **Store** | Thread-safe ring buffer of parsed entries |
| **TUI** | Bubbletea table with search/filter, reads from store |
| **MCP** | Streamable HTTP server on localhost, queries the store |

Parser and Store are the shared core. TUI and MCP are independent consumers.

---

## Config Format

YAML-driven, accepts a subset of gonzo's config format. A gonzo config will work if it only uses features logpond supports. Unsupported fields (`transform`, `pattern`) are silently ignored with a startup warning printed to stderr. Example:

```yaml
name: my-app
type: json

mapping:
  timestamp:
    field: timestamp
    time_format: rfc3339
  severity:
    field: level
  body:
    field: fields.message
  auto_map_remaining: true

columns:
  - name: Time
    source: timestamp
    format: time_short
    width: 8
  - name: Level
    source: severity
    width: 5
  - name: Symbol
    source: span_field:symbol
    width: 8
  - name: Message
    source: body
    flex: true
    exclude:
      - target
```

### Column sources

| Source | Description | Example |
|--------|------------|---------|
| `timestamp` | Mapped timestamp field | `source: timestamp` |
| `severity` | Mapped level field | `source: severity` |
| `body` | Mapped message field | `source: body` |
| `field:<name>` | Top-level JSON field | `source: field:service` |
| `span_field:<name>` | Field from `spans[]` array | `source: span_field:component` |

### Span field extraction

For `span_field:<name>`, the parser walks the `spans[]` array from last to first (innermost span wins — matches tracing-subscriber behavior). The first span containing the named field provides the value. Example JSON:

```json
{
  "spans": [
    {"name": "session", "symbol": "NVDA", "component": "coordinator"},
    {"name": "strategy", "component": "strategy"}
  ]
}
```

`span_field:component` resolves to `"strategy"` (innermost). `span_field:symbol` resolves to `"NVDA"` (only the session span has it).

### `auto_map_remaining`

When `true`, all JSON fields not consumed by `mapping` or `columns` are flattened into the entry's `Fields` map. Nested objects are serialized as JSON strings. These fields are appended as `key=value` pairs to the `body` column display (matching gonzo's behavior). Fields listed in a column's `exclude` list are suppressed from the body append.

### What is NOT supported (vs gonzo)

- No `transform` (first_segment, last_segment, map)
- No `pattern` config (regex/template parsing)
- No auto-detect — config is required
- JSON only (no logfmt, no plain text)

---

## Store & Search

### Entry structure

```go
type Entry struct {
    Timestamp time.Time
    Severity  string
    Body      string
    Fields    map[string]string  // all extracted fields, stringified
    Raw       string             // original JSON line
}
```

All field values are stored as strings. Numbers are formatted with `strconv.FormatFloat` (no trailing zeros). Booleans become `"true"`/`"false"`. Nested objects are serialized as compact JSON strings. This keeps the store simple and the MCP query interface uniform — all field matching is string-based.

### Ring buffer

- Fixed-size, default capacity 50,000 entries (configurable via `--buffer`)
- Thread-safe: `sync.RWMutex` — writer takes write lock, TUI and MCP take read locks
- Oldest entries evicted when full

### Query

Single query struct used by both TUI filter and MCP tools:

```go
type Query struct {
    Text   string            // regex match against body + raw line
    Fields map[string]string // exact match on field values
    Level  string            // severity filter
    After  time.Time         // entries after this time (zero = no bound)
    Before time.Time         // entries before this time (zero = no bound)
    Limit  int               // max results (0 = all)
}
```

- `Text` compiles to `regexp.Regexp`, matches body first; if body matches, raw is skipped (short-circuit)
- `Fields` matches against keys in `Entry.Fields` (original JSON field names, not column display names). Mapped fields (timestamp, severity, body) are excluded from `Entry.Fields` — use `Level`, `After`/`Before`, and `Text` to query those
- `After`/`Before` filter by entry timestamp
- All filters are AND — must match all specified criteria
- Returns `[]Entry` in chronological order
- In the TUI, regex is compiled on Enter (not per-keystroke) — filter mode captures input until Enter/Esc

---

## TUI

Minimal bubbletea app — one view, no modals, no charts.

### Layout

```
┌──────────────────────────────────────────────┐
│ Time     Level Symbol   Component  Message    │  ← column headers
│──────────────────────────────────────────────│
│ 00:13:30 INFO  NVDA     strategy   PM: Flat…  │
│ 00:13:30 INFO  NVDA     strategy   Paper fi…  │
│ 00:13:31 WARN  SOXL     strategy   Entry si…  │
│ ...                                           │
│──────────────────────────────────────────────│
│ Filter: /NVDA_[0-9]+          50,000 entries  │  ← status bar
└──────────────────────────────────────────────┘
```

### Keybindings

| Key | Action |
|-----|--------|
| `j/k` | Scroll down/up |
| `G` | Jump to bottom (latest) |
| `g` | Jump to top |
| `/` | Enter filter mode (regex, hides non-matching rows) |
| `Esc` | Clear filter |
| `Enter` | Expand selected row to see all fields |
| `q` | Quit |

### Behaviors

- Auto-scrolls to bottom when new logs arrive (if already at bottom)
- Stops auto-scroll when user scrolls up
- Filter input captures all keys until Enter/Esc
- Color coding: WARN=yellow, ERROR=red, DEBUG=dim

---

## MCP Server

Streamable HTTP on `localhost:9876` (configurable via `--mcp-port`). Starts automatically alongside the TUI in a separate goroutine — always on, no extra flag.

Uses the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`) which handles JSON-RPC framing, capability negotiation, and the initialize/initialized handshake. logpond only registers tool handlers.

### Tools

**`search_logs`** — query logs by any combination of filters

Input:
```json
{
  "text": "Paper fill",
  "fields": {"symbol": "NVDA", "component": "strategy"},
  "level": "INFO",
  "after": "2026-02-19T14:14:00Z",
  "limit": 20
}
```
All fields optional. Returns matching entries with all fields.

**`tail`** — last N entries

Input:
```json
{
  "n": 10
}
```
Returns the most recent N log entries.

**`get_schema`** — discover available columns

Input: `{}`

Returns column names, sources, and up to 10 unique values per column. Values are collected by walking the buffer backwards from newest, stopping after 10 unique values are found or 1,000 entries have been scanned, whichever comes first.

### Claude Code configuration

```json
{
  "mcpServers": {
    "logpond": {
      "url": "http://localhost:9876/mcp"
    }
  }
}
```

---

## CLI Interface

```bash
# Basic usage
app --log-format json 2>&1 | logpond --config ./config.yaml

# Custom buffer size and MCP port
app 2>&1 | logpond --config ./config.yaml --buffer 100000 --mcp-port 8888
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | (required) | Path to YAML config file |
| `--buffer` | 50000 | Ring buffer capacity |
| `--mcp-port` | 9876 | MCP server port |

### Dependencies

| Package | Purpose |
|---------|---------|
| `charmbracelet/bubbletea` | TUI framework |
| `charmbracelet/lipgloss` | Styling |
| `gopkg.in/yaml.v3` | Config parsing |
| `modelcontextprotocol/go-sdk` | MCP server (Streamable HTTP) |
| stdlib `regexp` | Search |

---

## Edge Cases

### Stdin handling

- **EOF / pipe close:** TUI stays alive showing buffered logs. Status bar shows "input closed". User can still scroll, filter, and query via MCP. Press `q` to quit.
- **Non-JSON lines:** Silently skipped. No error, no warning.
- **No pipe (tty stdin):** Print usage message to stderr and exit.

### Startup errors

- **Missing `--config`:** Exit with error message.
- **Invalid YAML:** Exit with parse error.
- **Port in use:** Exit with error: `"MCP server failed to bind to port 9876: address already in use"`.

### Shutdown

When user presses `q`: TUI exits → context cancellation → MCP HTTP server graceful shutdown (1s timeout for in-flight requests) → process exits.

### TUI refresh

The Reader goroutine appends entries to the Store and sends a `tea.Msg` via a channel to trigger TUI re-render. The TUI's `Update` function reads new entries from the Store on each message. This keeps bubbletea's single-threaded model intact.

---

## File Structure

```
logpond/
├── cmd/logpond/
│   └── main.go              ← entry point, flag parsing, wiring (~50 lines)
├── internal/
│   ├── config/
│   │   └── config.go        ← YAML parsing, field mapping (~80 lines)
│   ├── parser/
│   │   └── parser.go        ← JSON parsing, field extraction (~100 lines)
│   ├── store/
│   │   └── store.go         ← ring buffer, thread-safe insert/query (~120 lines)
│   ├── search/
│   │   └── search.go        ← filter logic, regex, field match (~100 lines)
│   ├── tui/
│   │   ├── model.go         ← bubbletea model, keybindings (~200 lines)
│   │   └── view.go          ← table rendering, column layout (~150 lines)
│   └── mcp/
│       ├── server.go        ← Streamable HTTP handler (~100 lines)
│       └── tools.go         ← tool definitions + implementations (~100 lines)
└── go.mod
```

**Estimated total: ~1,200 lines of Go.**

---

## What logpond is NOT

- Not a log aggregation platform (no persistence, no disk storage)
- Not a replacement for Datadog/Grafana/Loki (no remote backends)
- Not a multi-format parser (JSON only, config required)
- Not a metrics/charting tool (no charts, no stats panels)
- Not an AI analysis tool (no built-in LLM — Claude queries via MCP)

It is a sharp, single-purpose tool: pipe JSON logs in, see them in a table, let Claude query them.
