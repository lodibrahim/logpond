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
			Timestamp: config.FieldRef{Field: "ts"},
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
			Timestamp:        config.FieldRef{Field: "timestamp"},
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
	if entry.Fields["component"] != "strategy" {
		t.Errorf("component = %q, want %q", entry.Fields["component"], "strategy")
	}
	if entry.Fields["symbol"] != "NVDA" {
		t.Errorf("symbol = %q, want %q", entry.Fields["symbol"], "NVDA")
	}
}

func TestAutoMapSpanFields(t *testing.T) {
	// When auto_map_remaining is true, span fields should be extracted
	// even without explicit span_field column definitions.
	p := New(algoBotConfig())
	line := `{"timestamp":"2026-02-19T14:14:12Z","level":"INFO","fields":{"message":"Ring buffer created","capacity":4194304},"spans":[{"name":"","component":"engine","symbol":"TQQQ","sid":3,"date":"2026-02-19"}]}`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// sid and date should be auto-extracted from spans (no column needed)
	if entry.Fields["sid"] != "3" {
		t.Errorf("sid = %q, want %q", entry.Fields["sid"], "3")
	}
	if entry.Fields["date"] != "2026-02-19" {
		t.Errorf("date = %q, want %q", entry.Fields["date"], "2026-02-19")
	}
	// component and symbol should still work (have column definitions)
	if entry.Fields["component"] != "engine" {
		t.Errorf("component = %q, want %q", entry.Fields["component"], "engine")
	}
	if entry.Fields["symbol"] != "TQQQ" {
		t.Errorf("symbol = %q, want %q", entry.Fields["symbol"], "TQQQ")
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
			Timestamp:        config.FieldRef{Field: "ts"},
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
	if cells[0] == "" {
		t.Error("Time cell should not be empty")
	}
	if cells[1] != "INFO" {
		t.Errorf("Level cell = %q, want %q", cells[1], "INFO")
	}
	if cells[2] != "NVDA" {
		t.Errorf("Symbol cell = %q, want %q", cells[2], "NVDA")
	}
	if cells[3] != "strategy" {
		t.Errorf("Component cell = %q, want %q", cells[3], "strategy")
	}
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
	if _, ok := entry.Fields["target"]; !ok {
		t.Error("target should be in Fields map")
	}
	cells := p.ResolveColumns(entry)
	bodyCell := cells[4]
	if strings.Contains(bodyCell, "target=") {
		t.Errorf("Body cell should exclude target, got: %s", bodyCell)
	}
	if !strings.Contains(bodyCell, "client_ref=") {
		t.Errorf("Body cell should include client_ref, got: %s", bodyCell)
	}
}

func TestTimestampParsing(t *testing.T) {
	p := New(simpleConfig())
	entry, _ := p.Parse(`{"ts":"2026-02-19T14:14:12.345Z","level":"INFO","msg":"test"}`)
	expected := time.Date(2026, 2, 19, 14, 14, 12, 345000000, time.UTC)
	if !entry.Timestamp.Equal(expected) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, expected)
	}
	if entry.RawTimestamp != "2026-02-19T14:14:12.345Z" {
		t.Errorf("RawTimestamp = %q, want %q", entry.RawTimestamp, "2026-02-19T14:14:12.345Z")
	}
}

func TestRawTimestampPassthrough(t *testing.T) {
	// No format specified — ResolveColumns should return the raw timestamp string
	cfg := &config.Config{
		Mapping: config.MappingConfig{
			Timestamp: config.FieldRef{Field: "ts"},
			Severity:  config.FieldRef{Field: "level"},
			Body:      config.FieldRef{Field: "msg"},
		},
		Columns: []config.ColumnConfig{
			{Name: "Time", Source: "timestamp", Flex: true},
		},
	}
	p := New(cfg)
	entry, _ := p.Parse(`{"ts":"2026-02-19T14:14:12.345678Z","level":"INFO","msg":"test"}`)
	cells := p.ResolveColumns(entry)
	if cells[0] != "2026-02-19T14:14:12.345678Z" {
		t.Errorf("Time cell = %q, want raw timestamp %q", cells[0], "2026-02-19T14:14:12.345678Z")
	}
}

func TestRawTimestampLogfmt(t *testing.T) {
	p := New(logfmtConfig())
	entry, _ := p.Parse(`ts=2026-02-19T14:14:12.999Z level=INFO msg="test"`)
	if entry.RawTimestamp != "2026-02-19T14:14:12.999Z" {
		t.Errorf("RawTimestamp = %q, want %q", entry.RawTimestamp, "2026-02-19T14:14:12.999Z")
	}
}

// --- logfmt tests ---

func logfmtConfig() *config.Config {
	return &config.Config{
		Type: "logfmt",
		Mapping: config.MappingConfig{
			Timestamp: config.FieldRef{Field: "ts"},
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

func logfmtConfigWithFields() *config.Config {
	return &config.Config{
		Type: "logfmt",
		Mapping: config.MappingConfig{
			Timestamp:        config.FieldRef{Field: "ts"},
			Severity:         config.FieldRef{Field: "level"},
			Body:             config.FieldRef{Field: "msg"},
			AutoMapRemaining: true,
		},
		Columns: []config.ColumnConfig{
			{Name: "Time", Source: "timestamp", Width: 8},
			{Name: "Level", Source: "severity", Width: 5},
			{Name: "Service", Source: "field:service", Width: 10},
			{Name: "Message", Source: "body", Flex: true},
		},
	}
}

func TestLogfmtParseSimple(t *testing.T) {
	p := New(logfmtConfig())
	entry, err := p.Parse(`ts=2026-02-19T14:14:12Z level=INFO msg="hello world"`)
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

func TestLogfmtUnquotedValues(t *testing.T) {
	p := New(logfmtConfig())
	entry, err := p.Parse(`ts=2026-02-19T14:14:12Z level=WARN msg=timeout`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Severity != "WARN" {
		t.Errorf("Severity = %q, want %q", entry.Severity, "WARN")
	}
	if entry.Body != "timeout" {
		t.Errorf("Body = %q, want %q", entry.Body, "timeout")
	}
}

func TestLogfmtFieldExtraction(t *testing.T) {
	p := New(logfmtConfigWithFields())
	entry, err := p.Parse(`ts=2026-02-19T14:14:12Z level=INFO msg="request handled" service=api duration=23ms`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Fields["service"] != "api" {
		t.Errorf("service = %q, want %q", entry.Fields["service"], "api")
	}
}

func TestLogfmtAutoMapRemaining(t *testing.T) {
	p := New(logfmtConfigWithFields())
	entry, err := p.Parse(`ts=2026-02-19T14:14:12Z level=INFO msg="test" service=api duration=23ms status=200`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Fields["duration"] != "23ms" {
		t.Errorf("duration = %q, want %q", entry.Fields["duration"], "23ms")
	}
	if entry.Fields["status"] != "200" {
		t.Errorf("status = %q, want %q", entry.Fields["status"], "200")
	}
	// service should be consumed by column, not auto-mapped
	if entry.Fields["service"] != "api" {
		t.Errorf("service = %q, want %q", entry.Fields["service"], "api")
	}
}

func TestLogfmtQuotedWithEscapes(t *testing.T) {
	p := New(logfmtConfig())
	entry, err := p.Parse(`ts=2026-02-19T14:14:12Z level=ERROR msg="failed to connect: \"timeout\""`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Body != `failed to connect: "timeout"` {
		t.Errorf("Body = %q, want %q", entry.Body, `failed to connect: "timeout"`)
	}
}

func TestLogfmtEmpty(t *testing.T) {
	p := New(logfmtConfig())
	_, err := p.Parse("")
	if err == nil {
		t.Error("Parse should fail for empty line")
	}
}

func TestLogfmtRawPreserved(t *testing.T) {
	p := New(logfmtConfig())
	line := `ts=2026-02-19T14:14:12Z level=INFO msg="test"`
	entry, err := p.Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Raw != line {
		t.Errorf("Raw = %q, want %q", entry.Raw, line)
	}
}

func TestLogfmtResolveColumns(t *testing.T) {
	p := New(logfmtConfigWithFields())
	entry, err := p.Parse(`ts=2026-02-19T14:14:12Z level=INFO msg="request ok" service=api`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	cells := p.ResolveColumns(entry)
	if len(cells) != 4 {
		t.Fatalf("len(cells) = %d, want 4", len(cells))
	}
	if cells[1] != "INFO" {
		t.Errorf("Level cell = %q, want %q", cells[1], "INFO")
	}
	if cells[2] != "api" {
		t.Errorf("Service cell = %q, want %q", cells[2], "api")
	}
	if cells[3] != "request ok" {
		t.Errorf("Body cell = %q, want %q", cells[3], "request ok")
	}
}

func TestLogfmtMissingFields(t *testing.T) {
	p := New(logfmtConfig())
	entry, err := p.Parse(`level=INFO msg="no timestamp here"`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if entry.Timestamp.IsZero() != true {
		t.Error("Timestamp should be zero when missing")
	}
	if entry.Body != "no timestamp here" {
		t.Errorf("Body = %q, want %q", entry.Body, "no timestamp here")
	}
}

// Verify JSON still works after adding logfmt
func TestJSONStillWorksAfterLogfmt(t *testing.T) {
	p := New(simpleConfig())
	entry, err := p.Parse(`{"ts":"2026-02-19T14:14:12Z","level":"ERROR","msg":"db timeout"}`)
	if err != nil {
		t.Fatalf("JSON Parse failed: %v", err)
	}
	if entry.Severity != "ERROR" {
		t.Errorf("Severity = %q, want %q", entry.Severity, "ERROR")
	}
	if entry.Body != "db timeout" {
		t.Errorf("Body = %q, want %q", entry.Body, "db timeout")
	}
}
