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

type Entry struct {
	Timestamp time.Time
	Severity  string
	Body      string
	Fields    map[string]string
	Raw       string
}

type Parser struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Parser {
	return &Parser{cfg: cfg}
}

func (p *Parser) Parse(line string) (*Entry, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	entry := &Entry{
		Fields: make(map[string]string),
		Raw:    line,
	}

	consumed := make(map[string]bool)

	if ts := getNestedString(raw, p.cfg.Mapping.Timestamp.Field); ts != "" {
		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		markConsumed(consumed, p.cfg.Mapping.Timestamp.Field)
	}

	if sev := getNestedString(raw, p.cfg.Mapping.Severity.Field); sev != "" {
		entry.Severity = sev
		markConsumed(consumed, p.cfg.Mapping.Severity.Field)
	}

	if body := getNestedString(raw, p.cfg.Mapping.Body.Field); body != "" {
		entry.Body = body
		markConsumed(consumed, p.cfg.Mapping.Body.Field)
	}

	if spans, ok := raw["spans"]; ok {
		consumed["spans"] = true
		p.extractSpanFields(spans, entry)
	}

	for _, col := range p.cfg.Columns {
		if col.SourceType() == "field" {
			name := col.SourceField()
			if val := getNestedString(raw, name); val != "" {
				entry.Fields[name] = val
				markConsumed(consumed, name)
			}
		}
	}

	if p.cfg.Mapping.AutoMapRemaining {
		p.autoMapRemaining(raw, consumed, entry)
	}

	return entry, nil
}

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

func (p *Parser) buildBody(entry *Entry, col config.ColumnConfig) string {
	if !p.cfg.Mapping.AutoMapRemaining {
		return entry.Body
	}

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

func (p *Parser) extractSpanFields(spans interface{}, entry *Entry) {
	spanSlice, ok := spans.([]interface{})
	if !ok {
		return
	}

	needed := make(map[string]bool)
	for _, col := range p.cfg.Columns {
		if col.SourceType() == "span_field" {
			needed[col.SourceField()] = true
		}
	}

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

func (p *Parser) autoMapRemaining(raw map[string]interface{}, consumed map[string]bool, entry *Entry) {
	for key, val := range raw {
		if consumed[key] {
			continue
		}
		switch v := val.(type) {
		case map[string]interface{}:
			if consumed["__partial:"+key] {
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

func markConsumed(consumed map[string]bool, path string) {
	consumed[path] = true
	if i := strings.IndexByte(path, '.'); i >= 0 {
		consumed["__partial:"+path[:i]] = true
	}
}

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
