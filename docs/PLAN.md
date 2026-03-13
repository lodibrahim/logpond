# logpond Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lightweight TUI log viewer with built-in MCP server that reads JSON logs from stdin, displays them in a configurable table, and exposes them for AI querying.

**Architecture:** Reader goroutine reads stdin → Parser extracts fields per YAML config → Store (thread-safe ring buffer) holds entries → TUI (bubbletea) and MCP server (Streamable HTTP) both query the Store independently.

**Tech Stack:** Go 1.22+, bubbletea, lipgloss, gopkg.in/yaml.v3, modelcontextprotocol/go-sdk

**Spec:** `docs/superpowers/specs/2026-03-13-logpond-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `cmd/logpond/main.go` | Entry point, flag parsing, wiring |
| Create | `internal/config/config.go` | YAML config parsing, field mapping structs |
| Create | `internal/config/config_test.go` | Config parsing tests |
| Create | `internal/parser/parser.go` | JSON log parsing, field extraction, span resolution |
| Create | `internal/parser/parser_test.go` | Parser tests |
| Create | `internal/store/store.go` | Thread-safe ring buffer |
| Create | `internal/store/store_test.go` | Store tests |
| Create | `internal/search/search.go` | Query struct, filter logic |
| Create | `internal/search/search_test.go` | Search tests |
| Create | `internal/tui/model.go` | Bubbletea model, keybindings, update loop |
| Create | `internal/tui/view.go` | Table rendering, column layout, status bar |
| Create | `internal/mcp/server.go` | MCP server setup, Streamable HTTP |
| Create | `internal/mcp/tools.go` | Tool definitions (search_logs, tail, get_schema) |
| Create | `go.mod` | Module definition |
| Create | `testdata/algo-bot.yaml` | Test config (algo-bot format) |
| Create | `testdata/simple.yaml` | Test config (minimal) |

---

## Chunk 1: Scaffolding, Config, and Parser

### Task 1: Repo scaffolding

**Files:**
- Create: `go.mod`
- Create: `cmd/logpond/main.go`

- [ ] **Step 1: Create the repository on GitHub**

```bash
gh repo create lodibrahim/logpond --private --clone
cd logpond
```

- [ ] **Step 2: Initialize Go module**

```bash
go mod init github.com/lodibrahim/logpond
```

- [ ] **Step 3: Create directory structure**

```bash
mkdir -p cmd/logpond internal/{config,parser,store,search,tui,mcp} testdata
```

- [ ] **Step 4: Write minimal main.go**

Create `cmd/logpond/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	configPath := flag.String("config", "", "Path to YAML config file (required)")
	bufferSize := flag.Int("buffer", 50000, "Ring buffer capacity")
	mcpPort := flag.Int("mcp-port", 9876, "MCP server port")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config is required")
		os.Exit(1)
	}

	// Check stdin is a pipe, not a terminal
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		fmt.Fprintln(os.Stderr, "usage: app | logpond --config ./config.yaml")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "logpond: config=%s buffer=%d mcp-port=%d\n", *configPath, *bufferSize, *mcpPort)
}
```

- [ ] **Step 5: Verify it builds and runs**

Run: `go build ./cmd/logpond/`
Expected: Binary created, no errors.

Run: `echo '{}' | ./logpond --config test.yaml`
Expected: Prints startup info to stderr.

Run: `./logpond`
Expected: `error: --config is required`, exit code 1.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Scaffold project with main.go and directory structure"
```

---

### Task 2: Config parsing

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `testdata/simple.yaml`
- Create: `testdata/algo-bot.yaml`

- [ ] **Step 1: Create test configs**

Create `testdata/simple.yaml`:

```yaml
name: simple
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

Create `testdata/algo-bot.yaml`:

```yaml
name: algo-bot
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
  - name: Component
    source: span_field:component
    width: 12
  - name: Message
    source: body
    flex: true
    exclude:
      - target
```

- [ ] **Step 2: Write failing tests**

Create `internal/config/config_test.go`:

```go
package config

import (
	"testing"
)

func TestLoadSimple(t *testing.T) {
	cfg, err := Load("../../testdata/simple.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Name != "simple" {
		t.Errorf("Name = %q, want %q", cfg.Name, "simple")
	}
	if cfg.Mapping.Timestamp.Field != "ts" {
		t.Errorf("Timestamp.Field = %q, want %q", cfg.Mapping.Timestamp.Field, "ts")
	}
	if cfg.Mapping.Severity.Field != "level" {
		t.Errorf("Severity.Field = %q, want %q", cfg.Mapping.Severity.Field, "level")
	}
	if cfg.Mapping.Body.Field != "msg" {
		t.Errorf("Body.Field = %q, want %q", cfg.Mapping.Body.Field, "msg")
	}
	if len(cfg.Columns) != 3 {
		t.Fatalf("len(Columns) = %d, want 3", len(cfg.Columns))
	}
	if cfg.Columns[2].Flex != true {
		t.Error("Message column should have flex=true")
	}
}

func TestLoadAlgoBot(t *testing.T) {
	cfg, err := Load("../../testdata/algo-bot.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Name != "algo-bot" {
		t.Errorf("Name = %q, want %q", cfg.Name, "algo-bot")
	}
	if cfg.Mapping.Body.Field != "fields.message" {
		t.Errorf("Body.Field = %q, want %q", cfg.Mapping.Body.Field, "fields.message")
	}
	if !cfg.Mapping.AutoMapRemaining {
		t.Error("AutoMapRemaining should be true")
	}
	// Check span_field column
	if cfg.Columns[2].Source != "span_field:symbol" {
		t.Errorf("Column[2].Source = %q, want %q", cfg.Columns[2].Source, "span_field:symbol")
	}
	// Check exclude
	if len(cfg.Columns[4].Exclude) != 1 || cfg.Columns[4].Exclude[0] != "target" {
		t.Errorf("Message column exclude = %v, want [target]", cfg.Columns[4].Exclude)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("nonexistent.yaml")
	if err == nil {
		t.Error("Load should fail for missing file")
	}
}

func TestLoadInvalid(t *testing.T) {
	// Write invalid YAML to a temp file
	_, err := Load("/dev/null")
	if err == nil {
		t.Error("Load should fail for empty file")
	}
}

func TestFlexColumnRequired(t *testing.T) {
	cfg := &Config{
		Columns: []ColumnConfig{
			{Name: "A", Source: "body", Width: 10},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate should fail when no flex column exists")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL — `Load` not defined.

- [ ] **Step 4: Implement config.go**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level YAML configuration.
type Config struct {
	Name    string         `yaml:"name"`
	Type    string         `yaml:"type"`
	Mapping MappingConfig  `yaml:"mapping"`
	Columns []ColumnConfig `yaml:"columns"`
}

// MappingConfig maps JSON fields to semantic roles.
type MappingConfig struct {
	Timestamp        FieldRef `yaml:"timestamp"`
	Severity         FieldRef `yaml:"severity"`
	Body             FieldRef `yaml:"body"`
	AutoMapRemaining bool     `yaml:"auto_map_remaining"`
}

// FieldRef points to a JSON field.
type FieldRef struct {
	Field      string `yaml:"field"`
	TimeFormat string `yaml:"time_format,omitempty"`
}

// ColumnConfig defines a display column.
type ColumnConfig struct {
	Name    string   `yaml:"name"`
	Source  string   `yaml:"source"`
	Format  string   `yaml:"format,omitempty"`
	Width   int      `yaml:"width,omitempty"`
	Flex    bool     `yaml:"flex,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

// SourceType returns the column source type (timestamp, severity, body, field, span_field).
func (c *ColumnConfig) SourceType() string {
	switch {
	case c.Source == "timestamp" || c.Source == "severity" || c.Source == "body":
		return c.Source
	case strings.HasPrefix(c.Source, "field:"):
		return "field"
	case strings.HasPrefix(c.Source, "span_field:"):
		return "span_field"
	default:
		return "unknown"
	}
}

// SourceField returns the field name for field: and span_field: sources.
func (c *ColumnConfig) SourceField() string {
	if i := strings.IndexByte(c.Source, ':'); i >= 0 {
		return c.Source[i+1:]
	}
	return ""
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Warn about unsupported fields on stderr
	warnUnsupported(data)

	return &cfg, nil
}

// Validate checks the config for required fields.
func (c *Config) Validate() error {
	if len(c.Columns) == 0 {
		return fmt.Errorf("config: no columns defined")
	}

	hasFlex := false
	for _, col := range c.Columns {
		if col.Flex {
			hasFlex = true
			break
		}
	}
	if !hasFlex {
		return fmt.Errorf("config: exactly one column must have flex: true")
	}

	return nil
}

func warnUnsupported(data []byte) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return
	}
	unsupported := []string{"pattern", "transform"}
	for _, key := range unsupported {
		if _, ok := raw[key]; ok {
			fmt.Fprintf(os.Stderr, "logpond: warning: unsupported config field %q (ignored)\n", key)
		}
	}
}
```

- [ ] **Step 5: Install dependency and run tests**

Run: `go get gopkg.in/yaml.v3 && go test ./internal/config/`
Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Add config parsing with YAML support and validation"
```

---

### Task 3: JSON parser

**Files:**
- Create: `internal/parser/parser.go`
- Create: `internal/parser/parser_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/parser/parser_test.go`:

```go
package parser

import (
	"strings"
	"testing"
	"time"

	"github.com/lodibrahim/logpond/internal/config"
)

func simpleConfig() *config.Config {
	return &config.Config{
		Mapping: config.MappingConfig{
			Timestamp: config.FieldRef{Field: "ts", TimeFormat: "rfc3339"},
			Severity:  config.FieldRef{Field: "level"},
			Body:      config.FieldRef{Field: "msg"},
		},
		Columns: []config.ColumnConfig{
			{Name: "Time", Source: "timestamp", Width: 8},
			{Name: "Level", Source: "severity", Width: 5},
			{Name: "Message", Source: "body", Flex: true},
		},
	}
}

func algoBotConfig() *config.Config {
	return &config.Config{
		Mapping: config.MappingConfig{
			Timestamp:        config.FieldRef{Field: "timestamp", TimeFormat: "rfc3339"},
			Severity:         config.FieldRef{Field: "level"},
			Body:             config.FieldRef{Field: "fields.message"},
			AutoMapRemaining: true,
		},
		Columns: []config.ColumnConfig{
			{Name: "Time", Source: "timestamp", Width: 8},
			{Name: "Level", Source: "severity", Width: 5},
			{Name: "Symbol", Source: "span_field:symbol", Width: 8},
			{Name: "Component", Source: "span_field:component", Width: 12},
			{Name: "Message", Source: "body", Flex: true, Exclude: []string{"target"}},
		},
	}
}

func TestParseSimple(t *testing.T) {
	p := New(simpleConfig())
	entry, err := p.Parse(`{"ts":"2026-02-19T14:14:12Z","level":"INFO","msg":"hello world"}`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Severity != "INFO" {
		t.Errorf("Severity = %q, want %q", entry.Severity, "INFO")
	}
	if entry.Body != "hello world" {
		t.Errorf("Body = %q, want %q", entry.Body, "hello world")
	}
	if entry.Timestamp.Year() != 2026 {
		t.Errorf("Timestamp year = %d, want 2026", entry.Timestamp.Year())
	}
}

func TestParseNestedBody(t *testing.T) {
	p := New(algoBotConfig())
	entry, err := p.Parse(`{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"Paper fill","client_ref":"NVDA_0"}}`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Body != "Paper fill" {
		t.Errorf("Body = %q, want %q", entry.Body, "Paper fill")
	}
}

func TestParseSpanFields(t *testing.T) {
	p := New(algoBotConfig())
	line := `{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"test"},"spans":[{"name":"session","symbol":"NVDA","component":"coordinator"},{"name":"strategy","component":"strategy"}]}`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Innermost span wins for component
	if entry.Fields["component"] != "strategy" {
		t.Errorf("component = %q, want %q", entry.Fields["component"], "strategy")
	}
	// symbol only in outer span
	if entry.Fields["symbol"] != "NVDA" {
		t.Errorf("symbol = %q, want %q", entry.Fields["symbol"], "NVDA")
	}
}

func TestParseAutoMapRemaining(t *testing.T) {
	p := New(algoBotConfig())
	line := `{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"test","client_ref":"NVDA_0","price":186.45},"target":"execution::paper"}`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Fields["client_ref"] != "NVDA_0" {
		t.Errorf("client_ref = %q, want %q", entry.Fields["client_ref"], "NVDA_0")
	}
	if entry.Fields["price"] != "186.45" {
		t.Errorf("price = %q, want %q", entry.Fields["price"], "186.45")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	p := New(simpleConfig())
	_, err := p.Parse("not json")
	if err == nil {
		t.Error("Parse should fail for invalid JSON")
	}
}

func TestParseBoolAndNestedFields(t *testing.T) {
	cfg := &config.Config{
		Mapping: config.MappingConfig{
			Timestamp:        config.FieldRef{Field: "ts", TimeFormat: "rfc3339"},
			Severity:         config.FieldRef{Field: "level"},
			Body:             config.FieldRef{Field: "msg"},
			AutoMapRemaining: true,
		},
		Columns: []config.ColumnConfig{
			{Name: "Message", Source: "body", Flex: true},
		},
	}
	p := New(cfg)
	entry, err := p.Parse(`{"ts":"2026-01-01T00:00:00Z","level":"INFO","msg":"test","active":true,"meta":{"k":"v"}}`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Fields["active"] != "true" {
		t.Errorf("active = %q, want %q", entry.Fields["active"], "true")
	}
	// Nested object serialized as JSON string
	if entry.Fields["meta"] != `{"k":"v"}` {
		t.Errorf("meta = %q, want %q", entry.Fields["meta"], `{"k":"v"}`)
	}
}

func TestResolveColumns(t *testing.T) {
	p := New(algoBotConfig())
	line := `{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"PM: Flat"},"spans":[{"name":"session","symbol":"NVDA","component":"coordinator"},{"name":"strategy","component":"strategy"}]}`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	cells := p.ResolveColumns(entry)
	if len(cells) != 5 {
		t.Fatalf("len(cells) = %d, want 5", len(cells))
	}
	// Time column (index 0) — check it's not empty
	if cells[0] == "" {
		t.Error("Time cell should not be empty")
	}
	// Level column (index 1)
	if cells[1] != "INFO" {
		t.Errorf("Level cell = %q, want %q", cells[1], "INFO")
	}
	// Symbol column (index 2)
	if cells[2] != "NVDA" {
		t.Errorf("Symbol cell = %q, want %q", cells[2], "NVDA")
	}
	// Component column (index 3)
	if cells[3] != "strategy" {
		t.Errorf("Component cell = %q, want %q", cells[3], "strategy")
	}
	// Body column (index 4) — should contain the message
	if cells[4] != "PM: Flat" {
		t.Errorf("Body cell = %q, want %q", cells[4], "PM: Flat")
	}
}

func TestResolveColumnsExclude(t *testing.T) {
	p := New(algoBotConfig())
	line := `{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"test","client_ref":"NVDA_0"},"target":"execution::paper"}`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// target should be in Fields (stored)
	if _, ok := entry.Fields["target"]; !ok {
		t.Error("target should be in Fields map")
	}
	// But excluded from body display
	cells := p.ResolveColumns(entry)
	bodyCell := cells[4] // Message column
	if strings.Contains(bodyCell, "target=") {
		t.Errorf("Body cell should exclude target, got: %s", bodyCell)
	}
	if !strings.Contains(bodyCell, "client_ref=") {
		t.Errorf("Body cell should include client_ref, got: %s", bodyCell)
	}
}

// Verify that timestamp is parsed correctly
func TestTimestampParsing(t *testing.T) {
	p := New(simpleConfig())
	entry, _ := p.Parse(`{"ts":"2026-02-19T14:14:12.345Z","level":"INFO","msg":"test"}`)
	expected := time.Date(2026, 2, 19, 14, 14, 12, 345000000, time.UTC)
	if !entry.Timestamp.Equal(expected) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, expected)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/parser/`
Expected: FAIL — package not found.

- [ ] **Step 3: Implement parser.go**

Create `internal/parser/parser.go`:

```go
package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lodibrahim/logpond/internal/config"
)

// Entry is a parsed log line.
type Entry struct {
	Timestamp time.Time
	Severity  string
	Body      string
	Fields    map[string]string
	Raw       string
}

// Parser extracts structured entries from JSON log lines.
type Parser struct {
	cfg *config.Config
}

// New creates a parser from config.
func New(cfg *config.Config) *Parser {
	return &Parser{cfg: cfg}
}

// Parse parses a single JSON log line into an Entry.
func (p *Parser) Parse(line string) (*Entry, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	entry := &Entry{
		Fields: make(map[string]string),
		Raw:    line,
	}

	// Extract mapped fields
	consumed := make(map[string]bool)

	// Timestamp
	if ts := getNestedString(raw, p.cfg.Mapping.Timestamp.Field); ts != "" {
		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		markConsumed(consumed, p.cfg.Mapping.Timestamp.Field)
	}

	// Severity
	if sev := getNestedString(raw, p.cfg.Mapping.Severity.Field); sev != "" {
		entry.Severity = sev
		markConsumed(consumed, p.cfg.Mapping.Severity.Field)
	}

	// Body
	if body := getNestedString(raw, p.cfg.Mapping.Body.Field); body != "" {
		entry.Body = body
		markConsumed(consumed, p.cfg.Mapping.Body.Field)
	}

	// Extract span fields
	if spans, ok := raw["spans"]; ok {
		consumed["spans"] = true
		p.extractSpanFields(spans, entry)
	}

	// Extract column-sourced fields (field:<name>)
	for _, col := range p.cfg.Columns {
		if col.SourceType() == "field" {
			name := col.SourceField()
			if val := getNestedString(raw, name); val != "" {
				entry.Fields[name] = val
				markConsumed(consumed, name)
			}
		}
	}

	// Auto-map remaining
	if p.cfg.Mapping.AutoMapRemaining {
		p.autoMapRemaining(raw, consumed, entry)
	}

	return entry, nil
}

// ResolveColumns returns cell values for each configured column.
func (p *Parser) ResolveColumns(entry *Entry) []string {
	cells := make([]string, len(p.cfg.Columns))
	for i, col := range p.cfg.Columns {
		switch col.SourceType() {
		case "timestamp":
			if col.Format == "time_short" {
				cells[i] = entry.Timestamp.Format("15:04:05")
			} else {
				cells[i] = entry.Timestamp.Format(time.RFC3339)
			}
		case "severity":
			cells[i] = entry.Severity
		case "body":
			cells[i] = p.buildBody(entry, col)
		case "field", "span_field":
			cells[i] = entry.Fields[col.SourceField()]
		}
	}
	return cells
}

// buildBody constructs the body cell with appended remaining fields.
func (p *Parser) buildBody(entry *Entry, col config.ColumnConfig) string {
	if !p.cfg.Mapping.AutoMapRemaining {
		return entry.Body
	}

	// Collect fields already displayed in other columns
	usedFields := make(map[string]bool)
	for _, c := range p.cfg.Columns {
		if c.SourceType() == "field" || c.SourceType() == "span_field" {
			usedFields[c.SourceField()] = true
		}
	}
	for _, ex := range col.Exclude {
		usedFields[ex] = true
	}

	var extras []string
	for k, v := range entry.Fields {
		if !usedFields[k] {
			extras = append(extras, k+"="+v)
		}
	}
	sort.Strings(extras)

	if len(extras) == 0 {
		return entry.Body
	}
	return entry.Body + " " + strings.Join(extras, " ")
}

// extractSpanFields walks spans array from last to first (innermost wins).
func (p *Parser) extractSpanFields(spans interface{}, entry *Entry) {
	spanSlice, ok := spans.([]interface{})
	if !ok {
		return
	}

	// Collect needed span field names from config
	needed := make(map[string]bool)
	for _, col := range p.cfg.Columns {
		if col.SourceType() == "span_field" {
			needed[col.SourceField()] = true
		}
	}

	// Walk from last (innermost) to first
	for i := len(spanSlice) - 1; i >= 0; i-- {
		spanMap, ok := spanSlice[i].(map[string]interface{})
		if !ok {
			continue
		}
		for key, val := range spanMap {
			if needed[key] {
				if _, already := entry.Fields[key]; !already {
					entry.Fields[key] = stringify(val)
				}
			}
		}
	}
}

// autoMapRemaining flattens unconsumed fields into Entry.Fields.
// Partially-consumed nested objects (e.g., "fields" when "fields.message" was consumed)
// have their remaining sub-keys mapped individually.
// Fully unconsumed nested objects are serialized as JSON strings per spec.
func (p *Parser) autoMapRemaining(raw map[string]interface{}, consumed map[string]bool, entry *Entry) {
	for key, val := range raw {
		if consumed[key] {
			continue
		}
		switch v := val.(type) {
		case map[string]interface{}:
			if consumed["__partial:"+key] {
				// Partially consumed — map remaining sub-keys individually
				for subKey, subVal := range v {
					fullKey := key + "." + subKey
					if consumed[fullKey] {
						continue
					}
					if _, exists := entry.Fields[subKey]; !exists {
						entry.Fields[subKey] = stringify(subVal)
					}
				}
			} else {
				// Fully unconsumed nested object — serialize as JSON string
				if _, exists := entry.Fields[key]; !exists {
					entry.Fields[key] = stringify(val)
				}
			}
		default:
			if _, exists := entry.Fields[key]; !exists {
				entry.Fields[key] = stringify(val)
			}
		}
	}
}

// getNestedString resolves a dotted path (e.g., "fields.message") from a JSON map.
func getNestedString(m map[string]interface{}, path string) string {
	parts := strings.Split(path, ".")
	var current interface{} = m
	for _, part := range parts {
		cm, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = cm[part]
	}
	return stringify(current)
}

// markConsumed marks a dotted path as consumed and tracks partially-consumed roots.
func markConsumed(consumed map[string]bool, path string) {
	consumed[path] = true
	// For dotted paths like "fields.message", mark root as partially consumed
	// (not fully consumed — other sub-keys should still be auto-mapped)
	if i := strings.IndexByte(path, '.'); i >= 0 {
		consumed["__partial:"+path[:i]] = true
	}
}

// stringify converts any JSON value to a string.
func stringify(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/parser/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Add JSON parser with span field extraction and auto-map"
```

---

## Chunk 2: Store and Search

### Task 4: Ring buffer store

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/store/store_test.go`:

```go
package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/lodibrahim/logpond/internal/parser"
)

func makeEntry(i int, severity, body string) *parser.Entry {
	return &parser.Entry{
		Timestamp: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
		Severity:  severity,
		Body:      body,
		Fields:    map[string]string{"symbol": fmt.Sprintf("SYM%d", i%3)},
		Raw:       fmt.Sprintf(`{"i":%d}`, i),
	}
}

func TestAppendAndLen(t *testing.T) {
	s := New(10)
	s.Append(makeEntry(1, "INFO", "hello"))
	s.Append(makeEntry(2, "WARN", "world"))
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
}

func TestEviction(t *testing.T) {
	s := New(3)
	for i := 0; i < 5; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	if s.Len() != 3 {
		t.Errorf("Len = %d, want 3", s.Len())
	}
	// Oldest should be evicted — first entry should be msg2
	entries := s.Tail(3)
	if entries[0].Body != "msg2" {
		t.Errorf("First entry = %q, want %q", entries[0].Body, "msg2")
	}
}

func TestTail(t *testing.T) {
	s := New(100)
	for i := 0; i < 10; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	entries := s.Tail(3)
	if len(entries) != 3 {
		t.Fatalf("Tail(3) len = %d, want 3", len(entries))
	}
	if entries[0].Body != "msg7" {
		t.Errorf("Tail[0] = %q, want %q", entries[0].Body, "msg7")
	}
	if entries[2].Body != "msg9" {
		t.Errorf("Tail[2] = %q, want %q", entries[2].Body, "msg9")
	}
}

func TestTailMoreThanBuffer(t *testing.T) {
	s := New(100)
	for i := 0; i < 5; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	entries := s.Tail(100)
	if len(entries) != 5 {
		t.Errorf("Tail(100) len = %d, want 5", len(entries))
	}
}

func TestAll(t *testing.T) {
	s := New(100)
	for i := 0; i < 5; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	entries := s.All()
	if len(entries) != 5 {
		t.Fatalf("All len = %d, want 5", len(entries))
	}
	if entries[0].Body != "msg0" {
		t.Errorf("All[0] = %q, want %q", entries[0].Body, "msg0")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New(1000)
	done := make(chan bool, 2)

	// Writer
	go func() {
		for i := 0; i < 500; i++ {
			s.Append(makeEntry(i, "INFO", "concurrent"))
		}
		done <- true
	}()

	// Reader
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.Tail(10)
			_ = s.Len()
		}
		done <- true
	}()

	<-done
	<-done
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL — package not found.

- [ ] **Step 3: Implement store.go**

Create `internal/store/store.go`:

```go
package store

import (
	"sync"

	"github.com/lodibrahim/logpond/internal/parser"
)

// Store is a thread-safe ring buffer of parsed log entries.
type Store struct {
	mu       sync.RWMutex
	entries  []*parser.Entry
	capacity int
	head     int // next write position
	count    int
}

// New creates a store with the given capacity.
func New(capacity int) *Store {
	return &Store{
		entries:  make([]*parser.Entry, capacity),
		capacity: capacity,
	}
}

// Append adds an entry, evicting the oldest if full.
func (s *Store) Append(entry *parser.Entry) {
	s.mu.Lock()
	s.entries[s.head] = entry
	s.head = (s.head + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}
	s.mu.Unlock()
}

// Len returns the number of entries in the buffer.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// Tail returns the last n entries in chronological order.
func (s *Store) Tail(n int) []*parser.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tailLocked(n)
}

// All returns all entries in chronological order.
func (s *Store) All() []*parser.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tailLocked(s.count)
}

// tailLocked is the lock-free inner implementation of Tail.
// Caller must hold at least a read lock.
func (s *Store) tailLocked(n int) []*parser.Entry {
	if n > s.count {
		n = s.count
	}
	if n == 0 {
		return nil
	}
	result := make([]*parser.Entry, n)
	start := (s.head - n + s.capacity) % s.capacity
	for i := 0; i < n; i++ {
		result[i] = s.entries[(start+i)%s.capacity]
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Add thread-safe ring buffer store"
```

---

### Task 5: Search/query engine

**Files:**
- Create: `internal/search/search.go`
- Create: `internal/search/search_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/search/search_test.go`:

```go
package search

import (
	"fmt"
	"testing"
	"time"

	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/store"
)

func populatedStore() *store.Store {
	s := store.New(100)
	entries := []struct {
		i    int
		sev  string
		body string
		sym  string
	}{
		{0, "INFO", "PM: Flat → Entering", "NVDA"},
		{1, "INFO", "Paper fill", "NVDA"},
		{2, "WARN", "Entry signal rejected", "SOXL"},
		{3, "DEBUG", "Entry fill applied", "NVDA"},
		{4, "INFO", "PM: Exiting → Flat", "SOXL"},
	}
	for _, e := range entries {
		s.Append(&parser.Entry{
			Timestamp: time.Date(2026, 2, 19, 14, 14, e.i, 0, time.UTC),
			Severity:  e.sev,
			Body:      e.body,
			Fields:    map[string]string{"symbol": e.sym, "component": "strategy"},
			Raw:       fmt.Sprintf(`{"i":%d}`, e.i),
		})
	}
	return s
}

func TestSearchByText(t *testing.T) {
	s := populatedStore()
	results, err := Run(s, Query{Text: "Paper"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Body != "Paper fill" {
		t.Errorf("Body = %q, want %q", results[0].Body, "Paper fill")
	}
}

func TestSearchByRegex(t *testing.T) {
	s := populatedStore()
	results, err := Run(s, Query{Text: "PM:.*Flat"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestSearchByLevel(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{Level: "WARN"})
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Body != "Entry signal rejected" {
		t.Errorf("Body = %q", results[0].Body)
	}
}

func TestSearchByField(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{Fields: map[string]string{"symbol": "SOXL"}})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestSearchCombined(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{
		Level:  "INFO",
		Fields: map[string]string{"symbol": "NVDA"},
	})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2 (PM:Flat + Paper fill)", len(results))
	}
}

func TestSearchWithLimit(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{Limit: 2})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	// Limit returns the newest N matches
	if results[1].Body != "PM: Exiting → Flat" {
		t.Errorf("Last result = %q, want newest entry", results[1].Body)
	}
}

func TestSearchByTimeRange(t *testing.T) {
	s := populatedStore()
	// After is strictly greater-than, so second=1 means entries at 2,3,4 match
	after := time.Date(2026, 2, 19, 14, 14, 1, 0, time.UTC)
	results, _ := Run(s, Query{After: after})
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
}

func TestSearchByBeforeTime(t *testing.T) {
	s := populatedStore()
	// Before is strictly less-than, so second=3 means entries at 0,1,2 match
	before := time.Date(2026, 2, 19, 14, 14, 3, 0, time.UTC)
	results, _ := Run(s, Query{Before: before})
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
}

func TestSearchInvalidRegex(t *testing.T) {
	s := populatedStore()
	_, err := Run(s, Query{Text: "["})
	if err == nil {
		t.Error("Should fail for invalid regex")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{})
	if len(results) != 5 {
		t.Fatalf("len = %d, want 5 (all entries)", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/search/`
Expected: FAIL.

- [ ] **Step 3: Implement search.go**

Create `internal/search/search.go`:

```go
package search

import (
	"fmt"
	"regexp"
	"time"

	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/store"
)

// Query defines search criteria. All fields are AND-ed.
type Query struct {
	Text   string            // regex match against body (short-circuits raw)
	Fields map[string]string // exact match against Entry.Fields keys
	Level  string            // severity filter
	After  time.Time         // entries after this time
	Before time.Time         // entries before this time
	Limit  int               // max results (0 = all)
}

// Run executes a query against the store.
func Run(s *store.Store, q Query) ([]*parser.Entry, error) {
	var re *regexp.Regexp
	if q.Text != "" {
		var err error
		re, err = regexp.Compile(q.Text)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}

	all := s.All()
	var results []*parser.Entry

	for _, entry := range all {
		if !matches(entry, q, re) {
			continue
		}
		results = append(results, entry)
	}

	// Limit returns the newest (last) N matches in chronological order
	if q.Limit > 0 && len(results) > q.Limit {
		results = results[len(results)-q.Limit:]
	}

	return results, nil
}

func matches(entry *parser.Entry, q Query, re *regexp.Regexp) bool {
	// Level filter
	if q.Level != "" && entry.Severity != q.Level {
		return false
	}

	// Time range
	if !q.After.IsZero() && !entry.Timestamp.After(q.After) {
		return false
	}
	if !q.Before.IsZero() && !entry.Timestamp.Before(q.Before) {
		return false
	}

	// Field filter
	for k, v := range q.Fields {
		if entry.Fields[k] != v {
			return false
		}
	}

	// Text regex — body first, short-circuit
	if re != nil {
		if !re.MatchString(entry.Body) && !re.MatchString(entry.Raw) {
			return false
		}
	}

	return true
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/search/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: All packages PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Add search engine with regex, field, level, and time filtering"
```

---

## Chunk 3: TUI

### Task 6: TUI model and view

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/view.go`

Note: TUI code is primarily visual and tested manually. No unit tests for this task — verified by running the binary with piped logs.

- [ ] **Step 1: Create model.go**

Create `internal/tui/model.go`:

```go
package tui

import (
	"github.com/charmbracelet/bubbletea"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/search"
	"github.com/lodibrahim/logpond/internal/store"
)

// NewEntryMsg signals new log entries arrived.
type NewEntryMsg struct{}

// InputClosedMsg signals stdin has closed.
type InputClosedMsg struct{}

// Model is the bubbletea TUI model.
type Model struct {
	cfg    *config.Config
	parser *parser.Parser
	store  *store.Store

	// View state
	width, height int
	offset        int // scroll offset from bottom
	cursor        int // selected row index within visible entries (0 = bottom)
	atBottom      bool

	// Filter
	filterMode  bool
	filterInput string
	filterQuery *search.Query
	filtered    []*parser.Entry

	// Expand
	expandIdx int // -1 = none
	expanded  bool

	// Status
	inputClosed bool
	lastCount   int
}

// New creates a new TUI model.
func New(cfg *config.Config, p *parser.Parser, s *store.Store) *Model {
	return &Model{
		cfg:      cfg,
		parser:   p,
		store:    s,
		atBottom: true,
		expandIdx: -1,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case NewEntryMsg:
		m.refreshEntries()
		return m, nil

	case InputClosedMsg:
		m.inputClosed = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filterMode {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.scrollDown()
	case "k", "up":
		m.scrollUp()
	case "G":
		m.scrollToBottom()
	case "g":
		m.scrollToTop()
	case "/":
		m.filterMode = true
		m.filterInput = ""
	case "esc":
		m.clearFilter()
	case "enter":
		m.toggleExpand()
	}

	return m, nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.applyFilter()
		m.filterMode = false
		return m, nil
	case tea.KeyEscape:
		m.filterMode = false
		m.filterInput = ""
		return m, nil
	case tea.KeyBackspace:
		runes := []rune(m.filterInput)
		if len(runes) > 0 {
			m.filterInput = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyRunes:
		m.filterInput += string(msg.Runes)
		return m, nil
	case tea.KeySpace:
		m.filterInput += " "
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) refreshEntries() {
	if m.filterQuery != nil {
		results, _ := search.Run(m.store, *m.filterQuery)
		m.filtered = results
	} else {
		m.filtered = nil
	}
	if m.atBottom {
		m.offset = 0
	}
	m.lastCount = m.store.Len()
}

func (m *Model) visibleEntries() []*parser.Entry {
	if m.filtered != nil {
		return m.filtered
	}
	return m.store.All()
}

func (m *Model) tableHeight() int {
	// header(1) + separator(1) + status(1) = 3 lines overhead
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) scrollDown() {
	if m.cursor > 0 {
		m.cursor--
	} else if m.offset > 0 {
		m.offset--
	}
	if m.offset == 0 && m.cursor == 0 {
		m.atBottom = true
	}
}

func (m *Model) scrollUp() {
	entries := m.visibleEntries()
	tableH := m.tableHeight()
	if m.cursor < tableH-1 && m.cursor < len(entries)-1 {
		m.cursor++
		m.atBottom = false
	} else {
		maxOffset := len(entries) - tableH
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.offset < maxOffset {
			m.offset++
			m.atBottom = false
		}
	}
}

func (m *Model) scrollToBottom() {
	m.offset = 0
	m.atBottom = true
}

func (m *Model) scrollToTop() {
	entries := m.visibleEntries()
	maxOffset := len(entries) - m.tableHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.offset = maxOffset
	m.atBottom = false
}

func (m *Model) applyFilter() {
	if m.filterInput == "" {
		m.clearFilter()
		return
	}
	q := &search.Query{Text: m.filterInput}
	m.filterQuery = q
	results, err := search.Run(m.store, *q)
	if err != nil {
		// Invalid regex — just clear
		m.filterQuery = nil
		m.filtered = nil
		return
	}
	m.filtered = results
	m.offset = 0
	m.atBottom = true
}

func (m *Model) clearFilter() {
	m.filterQuery = nil
	m.filtered = nil
	m.filterInput = ""
	m.offset = 0
	m.atBottom = true
}

func (m *Model) toggleExpand() {
	if m.expanded {
		m.expanded = false
		m.expandIdx = -1
		return
	}
	entries := m.visibleEntries()
	if len(entries) == 0 {
		return
	}
	// Expand the row at current cursor position
	idx := len(entries) - m.offset - 1 - m.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(entries) {
		idx = len(entries) - 1
	}
	m.expandIdx = idx
	m.expanded = true
}

func (m *Model) View() string {
	return renderView(m)
}
```

- [ ] **Step 2: Create view.go**

Create `internal/tui/view.go`:

```go
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lodibrahim/logpond/internal/parser"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	debugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	expandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

func renderView(m *Model) string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	if m.expanded && m.expandIdx >= 0 {
		return renderExpanded(m)
	}

	var b strings.Builder

	// Header row
	b.WriteString(renderHeaderRow(m))
	b.WriteByte('\n')

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteByte('\n')

	// Log rows
	entries := m.visibleEntries()
	tableH := m.tableHeight()

	startIdx := len(entries) - m.offset - tableH
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := len(entries) - m.offset
	if endIdx > len(entries) {
		endIdx = len(entries)
	}

	for i := startIdx; i < endIdx; i++ {
		b.WriteString(renderRow(m, entries[i]))
		b.WriteByte('\n')
	}

	// Pad empty rows
	rendered := endIdx - startIdx
	for i := rendered; i < tableH; i++ {
		b.WriteByte('\n')
	}

	// Status bar
	b.WriteString(renderStatusBar(m))

	return b.String()
}

func renderHeaderRow(m *Model) string {
	var cells []string
	for _, col := range m.cfg.Columns {
		w := col.Width
		if col.Flex {
			w = flexWidth(m)
		}
		name := padOrTrunc(col.Name, w)
		cells = append(cells, headerStyle.Render(name))
	}
	return strings.Join(cells, " ")
}

func renderRow(m *Model, entry *parser.Entry) string {
	cells := m.parser.ResolveColumns(entry)
	var parts []string
	for i, col := range m.cfg.Columns {
		w := col.Width
		if col.Flex {
			w = flexWidth(m)
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		cell = padOrTrunc(cell, w)
		parts = append(parts, cell)
	}

	line := strings.Join(parts, " ")
	line = colorBySeverity(entry.Severity, line)
	return line
}

func renderExpanded(m *Model) string {
	entries := m.visibleEntries()
	if m.expandIdx >= len(entries) {
		return ""
	}
	entry := entries[m.expandIdx]

	var b strings.Builder
	b.WriteString(expandStyle.Render("── Log Entry Detail ──"))
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  Time:     %s\n", entry.Timestamp.Format("15:04:05.000")))
	b.WriteString(fmt.Sprintf("  Level:    %s\n", entry.Severity))
	b.WriteString(fmt.Sprintf("  Body:     %s\n", entry.Body))
	b.WriteByte('\n')

	// Sort fields for deterministic output
	keys := make([]string, 0, len(entry.Fields))
	for k := range entry.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString(expandStyle.Render("  Fields:"))
	b.WriteByte('\n')
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("    %s = %s\n", k, entry.Fields[k]))
	}
	b.WriteByte('\n')
	b.WriteString(statusStyle.Render("  Press Enter to close"))
	return b.String()
}

func renderStatusBar(m *Model) string {
	var left string
	if m.filterMode {
		left = fmt.Sprintf("Filter: /%s█", m.filterInput)
	} else if m.filterQuery != nil {
		left = fmt.Sprintf("Filter: /%s", m.filterQuery.Text)
	}

	total := m.store.Len()
	right := fmt.Sprintf("%d entries", total)
	if m.inputClosed {
		right = fmt.Sprintf("%d entries (input closed)", total)
	}
	if m.filtered != nil {
		right = fmt.Sprintf("%d/%d entries", len(m.filtered), total)
	}

	leftW := len([]rune(left))
	rightW := len([]rune(right))
	gap := m.width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func flexWidth(m *Model) int {
	fixed := 0
	for _, col := range m.cfg.Columns {
		if !col.Flex {
			fixed += col.Width
		}
	}
	// One space gap between each pair of columns
	gaps := len(m.cfg.Columns) - 1
	w := m.width - fixed - gaps
	if w < 10 {
		w = 10
	}
	return w
}

func padOrTrunc(s string, w int) string {
	runes := []rune(s)
	if len(runes) > w {
		if w > 1 {
			return string(runes[:w-1]) + "…"
		}
		return string(runes[:w])
	}
	return s + strings.Repeat(" ", w-len(runes))
}

func colorBySeverity(severity, line string) string {
	switch severity {
	case "WARN":
		return warnStyle.Render(line)
	case "ERROR", "FATAL":
		return errorStyle.Render(line)
	case "DEBUG", "TRACE":
		return debugStyle.Render(line)
	default:
		return line
	}
}
```

- [ ] **Step 3: Install TUI dependencies**

Run: `go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss`

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Add TUI with table rendering, filter, scroll, and expand"
```

---

## Chunk 4: MCP Server and Integration

### Task 7: MCP server and tools

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/tools.go`

- [ ] **Step 1: Install MCP SDK**

Run: `go get github.com/modelcontextprotocol/go-sdk`

- [ ] **Step 2: Create server.go**

Create `internal/mcp/server.go`:

```go
package mcpsvr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpserver "github.com/modelcontextprotocol/go-sdk/server"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/store"
)

// Server wraps the MCP server and HTTP listener.
type Server struct {
	httpServer *http.Server
	port       int
}

// New creates an MCP server with registered tools.
func New(cfg *config.Config, st *store.Store, port int) *Server {
	mcpSrv := mcpserver.NewServer(
		mcp.Implementation{
			Name:    "logpond",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(mcpSrv, cfg, st)

	handler := mcpserver.NewStreamableHTTPHandler(
		func(r *http.Request) *mcpserver.Server { return mcpSrv },
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	return &Server{
		httpServer: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
		port: port,
	}
}

// Listen binds the port synchronously. Call Serve() after to start serving.
func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return nil, fmt.Errorf("MCP server failed to bind to port %d: %w", s.port, err)
	}
	return ln, nil
}

// Serve starts serving on the given listener. Blocks until context is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutCtx)
	}()

	return s.httpServer.Serve(ln)
}
```

Note: The exact `go-sdk` API may differ slightly. The implementer should check `pkg.go.dev/github.com/modelcontextprotocol/go-sdk` for the current API and adjust import paths/function calls accordingly. The structure and wiring pattern is correct. The package is named `mcpsvr` to avoid collision with the SDK's `mcp` package.

- [ ] **Step 3: Create tools.go**

Create `internal/mcp/tools.go`:

```go
package mcpsvr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpserver "github.com/modelcontextprotocol/go-sdk/server"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/search"
	"github.com/lodibrahim/logpond/internal/store"
)

func registerTools(srv *mcpserver.Server, cfg *config.Config, st *store.Store) {
	// search_logs
	srv.AddTool(mcp.Tool{
		Name:        "search_logs",
		Description: "Search log entries by text regex, field values, severity level, and time range. All filters are AND-ed.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]map[string]interface{}{
				"text":   {"type": "string", "description": "Regex to match against log body"},
				"fields": {"type": "object", "description": "Field name → value exact matches"},
				"level":  {"type": "string", "description": "Severity level filter (INFO, WARN, etc.)"},
				"after":  {"type": "string", "description": "ISO 8601 timestamp — return entries after this time"},
				"before": {"type": "string", "description": "ISO 8601 timestamp — return entries before this time"},
				"limit":  {"type": "integer", "description": "Max results to return (default: all)"},
			},
		},
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params struct {
			Text   string            `json:"text"`
			Fields map[string]string `json:"fields"`
			Level  string            `json:"level"`
			After  string            `json:"after"`
			Before string            `json:"before"`
			Limit  int               `json:"limit"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}

		q := search.Query{
			Text:   params.Text,
			Fields: params.Fields,
			Level:  params.Level,
			Limit:  params.Limit,
		}
		if params.After != "" {
			q.After, _ = time.Parse(time.RFC3339, params.After)
		}
		if params.Before != "" {
			q.Before, _ = time.Parse(time.RFC3339, params.Before)
		}

		results, err := search.Run(st, q)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return toToolResult(results)
	})

	// tail
	srv.AddTool(mcp.Tool{
		Name:        "tail",
		Description: "Return the last N log entries.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]map[string]interface{}{
				"n": {"type": "integer", "description": "Number of entries to return (default: 10)"},
			},
		},
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if params.N <= 0 {
			params.N = 10
		}

		entries := st.Tail(params.N)
		return toToolResult(entries)
	})

	// get_schema
	srv.AddTool(mcp.Tool{
		Name:        "get_schema",
		Description: "Returns available columns and sample values from recent log entries.",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]map[string]interface{}{},
		},
	}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		schema := buildSchema(cfg, st)
		b, _ := json.MarshalIndent(schema, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})
}

type columnSchema struct {
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	SampleValues []string `json:"sample_values"`
}

func buildSchema(cfg *config.Config, st *store.Store) []columnSchema {
	entries := st.Tail(1000)
	var schema []columnSchema

	for _, col := range cfg.Columns {
		cs := columnSchema{Name: col.Name, Source: col.Source}
		seen := make(map[string]bool)

		for i := len(entries) - 1; i >= 0 && len(cs.SampleValues) < 10; i-- {
			val := fieldValueForColumn(entries[i], col)
			if val != "" && !seen[val] {
				seen[val] = true
				cs.SampleValues = append(cs.SampleValues, val)
			}
		}
		schema = append(schema, cs)
	}

	return schema
}

func fieldValueForColumn(entry *parser.Entry, col config.ColumnConfig) string {
	switch col.SourceType() {
	case "timestamp":
		return entry.Timestamp.Format("15:04:05")
	case "severity":
		return entry.Severity
	case "field", "span_field":
		return entry.Fields[col.SourceField()]
	case "body":
		return entry.Body
	default:
		return ""
	}
}

func toToolResult(entries []*parser.Entry) (*mcp.CallToolResult, error) {
	type entryJSON struct {
		Timestamp string            `json:"timestamp"`
		Severity  string            `json:"severity"`
		Body      string            `json:"body"`
		Fields    map[string]string `json:"fields"`
	}

	result := make([]entryJSON, len(entries))
	for i, e := range entries {
		result[i] = entryJSON{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Severity:  e.Severity,
			Body:      e.Body,
			Fields:    e.Fields,
		}
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
```

Note: The `mcp.Tool`, `mcp.CallToolRequest`, `mcp.CallToolResult`, and `mcp.NewToolResultText` types and functions follow the go-sdk API. The implementer should verify the exact API shape from `pkg.go.dev/github.com/modelcontextprotocol/go-sdk` and adjust as needed. The tool registration pattern and logic are correct. The package is `mcpsvr` (not `mcp`) to avoid collision with the SDK's `mcp` package.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: No errors. (May need API adjustments — see notes above.)

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Add MCP server with search_logs, tail, and get_schema tools"
```

---

### Task 8: Wire everything together in main.go

**Files:**
- Modify: `cmd/logpond/main.go`

- [ ] **Step 1: Update main.go with full wiring**

Replace `cmd/logpond/main.go`:

```go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lodibrahim/logpond/internal/config"
	mcpsvr "github.com/lodibrahim/logpond/internal/mcp"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/store"
	"github.com/lodibrahim/logpond/internal/tui"
)

func main() {
	configPath := flag.String("config", "", "Path to YAML config file (required)")
	bufferSize := flag.Int("buffer", 50000, "Ring buffer capacity")
	mcpPort := flag.Int("mcp-port", 9876, "MCP server port")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config is required")
		os.Exit(1)
	}

	// Check stdin is a pipe
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		fmt.Fprintln(os.Stderr, "usage: app | logpond --config ./config.yaml")
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Create components
	p := parser.New(cfg)
	st := store.New(*bufferSize)

	// Create TUI
	model := tui.New(cfg, p, st)
	program := tea.NewProgram(model, tea.WithAltScreen())

	// Context for shutdown coordination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Bind MCP server port synchronously (fail fast on port conflict)
	mcp := mcpsvr.New(cfg, st, *mcpPort)
	ln, err := mcp.Listen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "logpond: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "logpond: MCP server on http://localhost:%d/mcp\n", *mcpPort)

	// Start MCP server in background
	go func() {
		if err := mcp.Serve(ctx, ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "logpond: MCP server error: %v\n", err)
		}
	}()

	// Start stdin reader
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		// Increase buffer for long JSON lines
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			entry, err := p.Parse(line)
			if err != nil {
				continue // skip non-JSON lines
			}
			st.Append(entry)
			program.Send(tui.NewEntryMsg{})
		}
		program.Send(tui.InputClosedMsg{})
	}()

	// Run TUI (blocks until quit)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Shutdown
	cancel()
}
```

- [ ] **Step 2: Verify full build**

Run: `go build ./cmd/logpond/`
Expected: Binary created, no errors.

- [ ] **Step 3: Manual smoke test**

Run:
```bash
echo '{"ts":"2026-01-01T00:00:00Z","level":"INFO","msg":"hello world"}' | ./logpond --config testdata/simple.yaml
```
Expected: TUI displays one log row with Time, Level, Message columns. Press `q` to quit.

- [ ] **Step 4: Test with algo-bot config**

Run:
```bash
echo '{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"Paper fill","client_ref":"NVDA_0"},"spans":[{"name":"session","symbol":"NVDA","component":"coordinator"},{"name":"strategy","component":"strategy"}]}' | ./logpond --config testdata/algo-bot.yaml
```
Expected: TUI shows Time, Level, Symbol=NVDA, Component=strategy, Message=Paper fill.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: All packages PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Wire all components in main.go — logpond is functional"
```

---

### Task 9: Final integration test with algo-bot

- [ ] **Step 1: Install logpond**

```bash
go install ./cmd/logpond/
```

- [ ] **Step 2: Test with live algo-bot logs**

In the algo-bot repo:
```bash
./target/debug/algo-bot --log-format json start 2>&1 | logpond --config ./gonzo.yaml
```

Then add sessions:
```bash
./target/debug/algo-bot add NVDA --date 2026-02-19 --speed 10 --strategy two_red --from 9:30
```

Expected: logpond TUI shows live logs with correct Symbol, Component, Message columns.

- [ ] **Step 3: Test MCP endpoint**

```bash
curl -X POST http://localhost:9876/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
```

Expected: JSON-RPC response with server capabilities.

- [ ] **Step 4: Test filter**

In the TUI, press `/`, type `NVDA`, press Enter.
Expected: Only NVDA logs visible. Status bar shows filtered count.

- [ ] **Step 5: Push and tag**

```bash
git push origin main
git tag v0.1.0
git push origin v0.1.0
```
