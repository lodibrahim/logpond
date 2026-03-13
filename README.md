# logpond

A lightweight TUI log viewer with a built-in MCP server. Pipe JSON logs in, get a searchable terminal UI and an AI-queryable endpoint out.

```
your-app 2>&1 | logpond --config ./config.yaml
```

![logpond](https://img.shields.io/badge/go-1.25-blue)

## Features

- **TUI** — Column-based log viewer with live scrolling, search, and copy
- **MCP Server** — AI agents query your logs via `stats`, `search_logs`, `tail`, `get_schema`
- **Hub** — Auto-spawning aggregator discovers all running instances, one MCP endpoint for all services
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

The first instance auto-spawns the **hub** on port 9800. No extra setup.

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

### 4. Connect your AI agent

Point your AI agent at the hub (not the individual instance):

```json
// .mcp.json (Claude Code)
{
  "mcpServers": {
    "logpond": {
      "type": "http",
      "url": "http://localhost:9800/mcp"
    }
  }
}
```

The hub discovers all running logpond instances automatically. Start more services, they appear. Stop them, they disappear.

## Hub

The hub is an MCP aggregator that sits in front of all logpond instances.

### How it works

1. First logpond instance checks if port 9800 is open
2. If not, spawns `logpond hub` as a detached background process
3. Each instance registers itself in `~/.logpond/<name>-<pid>.json`
4. Hub discovers instances on every query (no polling)
5. Hub shuts down after 60s with no live instances

### Hub tools

All tools fan out to every live instance and merge results:

- **`list_instances`** — show all discovered instances with status
- **`stats`** — merged severity breakdown, field values, time range per instance
- **`search_logs`** — search across all instances, results interleaved by timestamp
- **`get_schema`** — all instance schemas and contexts in one call
- **`tail`** — last N entries merged across all instances

Use the `instance` parameter on `search_logs` and `tail` to target a specific service.

### Manual hub

The hub is normally auto-spawned, but you can start it manually:

```bash
logpond hub              # default port 9800
logpond hub --port 9900  # custom port
```

## Instance MCP Tools

Each logpond instance also exposes its own MCP endpoint (default port 9876):

### `stats`

Quick overview — total entries, severity breakdown, active field values, time range.

```json
{
  "instance": "my-app",
  "total_entries": 1731,
  "time_range": { "oldest": "...", "newest": "..." },
  "severity": { "INFO": 1365, "WARN": 50, "DEBUG": 316 },
  "fields": {
    "component": [{ "value": "engine", "count": 578 }]
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
| `timestamp` | -- | Parsed timestamp |
| `severity` | -- | Log level |
| `body` | -- | Log message |
| `field:<name>` | `field:service` | Top-level JSON field |
| `span_field:<name>` | `span_field:trace_id` | Field from `spans[]` array |

## CLI

```
# Instance mode (pipe logs)
app 2>&1 | logpond --config ./config.yaml [flags]

Flags:
  --config     Path to YAML config file (required)
  --buffer     Ring buffer capacity (default: 50000)
  --mcp-port   MCP server port (default: 9876)
  --name       Instance name override (default: from config)

# Hub mode (aggregator)
logpond hub [flags]

Flags:
  --port       Hub MCP server port (default: 9800)
```

## Architecture

```
                        ┌──────────────────────────┐
                        │    Hub (:9800/mcp)        │
                        │    Auto-spawned by first  │
                        │    instance. Fans out     │
                        │    queries to all live    │
                        │    instances.             │
                        └─────┬──────────┬─────────┘
                              │          │
               ┌──────────────┘          └──────────────┐
               ▼                                        ▼
  ┌─────────────────────────┐          ┌─────────────────────────┐
  │  Instance A (:9876)     │          │  Instance B (:9877)     │
  │                         │          │                         │
  │  stdin ─▶ Parser        │          │  stdin ─▶ Parser        │
  │            ▼            │          │            ▼            │
  │          Store ◀── MCP  │          │          Store ◀── MCP  │
  │            ▼            │          │            ▼            │
  │           TUI           │          │           TUI           │
  └─────────────────────────┘          └─────────────────────────┘

  ~/.logpond/app-a-1234.json           ~/.logpond/app-b-5678.json
```

- **Hub** discovers instances via `~/.logpond/*.json` registration files
- **Instances** register on startup, deregister on exit
- **AI agent** connects to hub once — sees all services

## Multiple Services

Just pipe each service through its own logpond instance:

```bash
# Terminal 1
api-server 2>&1 | logpond --config ./api.yaml

# Terminal 2
worker 2>&1 | logpond --config ./worker.yaml --mcp-port 9877
```

Each instance gets its own TUI, its own config, and its own columns. The hub merges them all — the AI agent sees everything from one endpoint.

## License

MIT
