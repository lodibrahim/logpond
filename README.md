# logpond

A lightweight TUI log viewer with a built-in MCP server. Pipe JSON logs in, get a searchable terminal UI and an AI-queryable endpoint out.

```
your-app 2>&1 | logpond --config ./config.yaml
```

![logpond](https://img.shields.io/badge/go-1.25-blue)

## Features

- **TUI** — Column-based log viewer with live scrolling, search, and copy
- **MCP Server** — AI agents query your logs via `stats`, `search_logs`, `tail`, `get_schema`
- **Config-driven** — YAML defines columns, field mappings, and MCP behavior
- **Ring buffer** — Fixed-capacity circular buffer (default 50k entries), zero-copy iteration
- **Generic** — Works with any JSON log format, not tied to any specific app or framework

## Install

```bash
go install github.com/lodibrahim/logpond/cmd/logpond@latest
```

## Quick Start

### 1. Create a config

```yaml
# config.yaml
name: my-app
type: json

mapping:
  timestamp:
    field: ts
    time_format: rfc3339
  severity:
    field: level
  body:
    field: msg

columns:
  - name: Time
    source: timestamp
    format: time_short
    width: 8

  - name: Level
    source: severity
    width: 5

  - name: Message
    source: body
    flex: true
```

### 2. Pipe your logs

```bash
my-app 2>&1 | logpond --config ./config.yaml
```

### 3. Use the TUI

| Key | Action |
|-----|--------|
| `j` / `k` | Scroll down / up |
| `G` / `g` | Jump to bottom / top |
| `/` | Live search (filters as you type) |
| `Esc` | Clear search |
| `y` | Copy visible entries to clipboard |
| `c` | Clear all logs |
| `q` | Quit |

Mouse wheel scrolling is also supported.

### 4. Query via MCP

While logpond is running, AI agents can connect to `http://localhost:9876/mcp`:

```bash
# Claude Code — add to .mcp.json in your project root:
{
  "mcpServers": {
    "logpond": {
      "type": "http",
      "url": "http://localhost:9876/mcp"
    }
  }
}
```

## MCP Tools

### `stats`

Quick overview — total entries, severity breakdown, active field values, time range. An agent should call this first.

```json
{
  "instance": "my-app",
  "total_entries": 1731,
  "time_range": { "oldest": "...", "newest": "..." },
  "severity": { "INFO": 1365, "WARN": 50, "DEBUG": 316 },
  "fields": {
    "component": [{ "value": "engine", "count": 578 }, ...],
    "symbol": [{ "value": "NVDA", "count": 1670 }]
  }
}
```

### `search_logs`

Search with regex, field filters, severity, and time range. All filters are AND-ed.

| Param | Type | Description |
|-------|------|-------------|
| `text` | string | Regex against body and all field values (case-insensitive) |
| `fields` | object | Exact field matches, e.g. `{"symbol": "NVDA"}` |
| `level` | string | `INFO`, `WARN`, `ERROR`, `DEBUG` |
| `after` / `before` | string | ISO 8601 time range |
| `limit` | int | Max results (default from config) |
| `count_only` | bool | Return just the count, no entries |

### `tail`

Last N entries (default 10).

### `get_schema`

Returns column definitions, sample values, and the `context` string from your config — gives AI agents operational context about your app.

## Config Reference

```yaml
name: my-app                          # Instance name (shown in MCP responses)
type: json

mapping:
  timestamp:
    field: timestamp                   # JSON path to timestamp
    time_format: rfc3339
  severity:
    field: level                       # JSON path to log level
  body:
    field: fields.message              # Supports nested paths (dot-separated)
  auto_map_remaining: true             # Append unmapped fields to message body

mcp:
  exclude_fields:                      # Fields to strip from MCP responses
    - span
    - target
  default_limit: 100                   # Default search result limit
  context: |                           # Operational context for AI agents
    my-app is a web server.
    "Connection reset" warnings are normal during deploys.

columns:
  - name: Time
    source: timestamp
    format: time_short                 # HH:MM:SS
    width: 8

  - name: Level
    source: severity
    width: 5

  - name: Service
    source: field:service              # Extract from top-level JSON field
    width: 10

  - name: Trace
    source: span_field:trace_id        # Extract from spans[] array
    width: 12

  - name: Message
    source: body
    flex: true                         # Exactly one column must be flex
    exclude:                           # Fields to hide from auto_map body
      - target
```

### Column Source Types

| Source | Example | Description |
|--------|---------|-------------|
| `timestamp` | — | Parsed timestamp |
| `severity` | — | Log level |
| `body` | — | Log message |
| `field:<name>` | `field:service` | Top-level JSON field |
| `span_field:<name>` | `span_field:trace_id` | Field from `spans[]` array |

## CLI Flags

```
--config     Path to YAML config file (required)
--buffer     Ring buffer capacity (default: 50000)
--mcp-port   MCP server port (default: 9876)
--name       Instance name override (default: from config)
```

## Architecture

```
stdin (JSON lines)
    │
    ▼
┌─────────┐     ┌───────────┐     ┌─────────────┐
│  Parser  │────▶│   Store   │◀────│  MCP Server  │
│          │     │ (ring buf)│     │  :9876/mcp   │
└─────────┘     └─────┬─────┘     └─────────────┘
                      │
                      ▼
                ┌───────────┐
                │    TUI    │
                │ (alt screen)│
                └───────────┘
```

- **Parser** reads JSON from stdin, extracts fields per config
- **Store** holds entries in a thread-safe ring buffer
- **TUI** renders columns with live search and scrolling (reads keyboard from `/dev/tty`)
- **MCP Server** exposes search, stats, tail, and schema tools over HTTP

## Multiple Instances

Run multiple logpond instances for different services:

```bash
app-a 2>&1 | logpond --config ./a.yaml --mcp-port 9876
app-b 2>&1 | logpond --config ./b.yaml --mcp-port 9877
```

Each instance is identified by its config `name` in MCP responses, so AI agents know which service they're querying.

## License

MIT
