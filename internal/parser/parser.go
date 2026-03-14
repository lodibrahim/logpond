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
	Timestamp    time.Time
	RawTimestamp string
	Severity     string
	Body         string
	Fields       map[string]string
	Raw          string
}

type Parser struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Parser {
	return &Parser{cfg: cfg}
}

func (p *Parser) Parse(line string) (*Entry, error) {
	if p.cfg.Type == "logfmt" {
		return p.parseLogfmt(line)
	}
	return p.parseJSON(line)
}

func (p *Parser) parseJSON(line string) (*Entry, error) {
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
		entry.RawTimestamp = ts
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

func (p *Parser) parseLogfmt(line string) (*Entry, error) {
	pairs := parseLogfmtPairs(line)
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no logfmt pairs found")
	}

	entry := &Entry{
		Fields: make(map[string]string),
		Raw:    line,
	}

	consumed := make(map[string]bool)

	tsField := p.cfg.Mapping.Timestamp.Field
	if v, ok := pairs[tsField]; ok {
		entry.RawTimestamp = v
		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, v)
		consumed[tsField] = true
	}

	sevField := p.cfg.Mapping.Severity.Field
	if v, ok := pairs[sevField]; ok {
		entry.Severity = v
		consumed[sevField] = true
	}

	bodyField := p.cfg.Mapping.Body.Field
	if v, ok := pairs[bodyField]; ok {
		entry.Body = v
		consumed[bodyField] = true
	}

	for _, col := range p.cfg.Columns {
		if col.SourceType() == "field" {
			name := col.SourceField()
			if v, ok := pairs[name]; ok {
				entry.Fields[name] = v
				consumed[name] = true
			}
		}
	}

	if p.cfg.Mapping.AutoMapRemaining {
		for k, v := range pairs {
			if !consumed[k] {
				entry.Fields[k] = v
			}
		}
	}

	return entry, nil
}

// parseLogfmtPairs parses a logfmt line into key=value pairs.
// Supports unquoted values and double-quoted values with backslash escapes.
func parseLogfmtPairs(line string) map[string]string {
	pairs := make(map[string]string)
	i := 0
	n := len(line)

	for i < n {
		// Skip whitespace
		for i < n && line[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}

		// Read key
		keyStart := i
		for i < n && line[i] != '=' && line[i] != ' ' {
			i++
		}
		if i >= n || line[i] != '=' {
			// Bare key with no value — skip
			continue
		}
		key := line[keyStart:i]
		i++ // skip '='

		// Read value
		if i < n && line[i] == '"' {
			// Quoted value
			i++ // skip opening quote
			var val strings.Builder
			for i < n && line[i] != '"' {
				if line[i] == '\\' && i+1 < n {
					i++
				}
				val.WriteByte(line[i])
				i++
			}
			if i < n {
				i++ // skip closing quote
			}
			pairs[key] = val.String()
		} else {
			// Unquoted value
			valStart := i
			for i < n && line[i] != ' ' {
				i++
			}
			pairs[key] = line[valStart:i]
		}
	}
	return pairs
}

func (p *Parser) ResolveColumns(entry *Entry) []string {
	cells := make([]string, len(p.cfg.Columns))
	for i, col := range p.cfg.Columns {
		switch col.SourceType() {
		case "timestamp":
			if col.Format == "time_short" {
				cells[i] = entry.Timestamp.Format("15:04:05")
			} else {
				cells[i] = entry.RawTimestamp
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
	autoMap := p.cfg.Mapping.AutoMapRemaining

	// Iterate in reverse — last span (innermost) wins on conflicts.
	for i := len(spanSlice) - 1; i >= 0; i-- {
		spanMap, ok := spanSlice[i].(map[string]interface{})
		if !ok {
			continue
		}
		for key, val := range spanMap {
			if key == "name" {
				continue // skip tracing span name — internal metadata, not user data
			}
			if needed[key] || autoMap {
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
