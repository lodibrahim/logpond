package mcpsvr

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/search"
	"github.com/lodibrahim/logpond/internal/store"
)

func registerTools(srv *mcp.Server, cfg *config.Config, st *store.Store, name string) {
	excludeSet := make(map[string]bool, len(cfg.MCP.ExcludeFields))
	for _, f := range cfg.MCP.ExcludeFields {
		excludeSet[f] = true
	}
	// Build dynamic description with available fields
	fieldNames := availableFieldNames(cfg)
	searchDesc := fmt.Sprintf(
		"Search log entries by text regex, field values, severity level, and time range. All filters are AND-ed. Available fields for filtering: %s. Set count_only=true to get just the count without entries.",
		strings.Join(fieldNames, ", "),
	)

	// search_logs
	srv.AddTool(
		&mcp.Tool{
			Name:        "search_logs",
			Description: searchDesc,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":       map[string]any{"type": "string", "description": "Regex to match against log body and all field values (case-insensitive)"},
					"fields":     map[string]any{"type": "object", "description": fmt.Sprintf("Field name → value exact matches. Available: %s", strings.Join(fieldNames, ", "))},
					"level":      map[string]any{"type": "string", "description": "Severity level filter (INFO, WARN, ERROR, DEBUG)"},
					"after":      map[string]any{"type": "string", "description": "ISO 8601 timestamp — return entries after this time"},
					"before":     map[string]any{"type": "string", "description": "ISO 8601 timestamp — return entries before this time"},
					"limit":      map[string]any{"type": "integer", "description": fmt.Sprintf("Max results to return (default: %d)", cfg.MCP.DefaultLimit)},
					"count_only": map[string]any{"type": "boolean", "description": "If true, return only the match count without entry data (fast, saves tokens)"},
				},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var params struct {
				Text      string            `json:"text"`
				Fields    map[string]string `json:"fields"`
				Level     string            `json:"level"`
				After     string            `json:"after"`
				Before    string            `json:"before"`
				Limit     int               `json:"limit"`
				CountOnly bool              `json:"count_only"`
			}
			if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}

			limit := params.Limit
			if !params.CountOnly && limit <= 0 && cfg.MCP.DefaultLimit > 0 {
				limit = cfg.MCP.DefaultLimit
			}
			q := search.Query{
				Text:   params.Text,
				Fields: params.Fields,
				Level:  params.Level,
				Limit:  limit,
			}
			if params.After != "" {
				t, err := time.Parse(time.RFC3339, params.After)
				if err != nil {
					return toolError(fmt.Sprintf("invalid 'after' timestamp: %v", err)), nil
				}
				q.After = t
			}
			if params.Before != "" {
				t, err := time.Parse(time.RFC3339, params.Before)
				if err != nil {
					return toolError(fmt.Sprintf("invalid 'before' timestamp: %v", err)), nil
				}
				q.Before = t
			}

			results, err := search.Run(st, q)
			if err != nil {
				return toolError(err.Error()), nil
			}

			if params.CountOnly {
				result := struct {
					Instance string `json:"instance"`
					Count    int    `json:"count"`
				}{Instance: name, Count: len(results)}
				b, _ := json.MarshalIndent(result, "", "  ")
				return toolText(string(b)), nil
			}

			if len(results) == 0 {
				return toolText(fmt.Sprintf("[%s] No matches found.", name)), nil
			}

			return toToolResult(name, results, excludeSet)
		},
	)

	// tail
	srv.AddTool(
		&mcp.Tool{
			Name:        "tail",
			Description: "Return the last N log entries.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"n": map[string]any{"type": "integer", "description": "Number of entries to return (default: 10)"},
				},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			return toToolResult(name, entries, excludeSet)
		},
	)

	// get_schema
	srv.AddTool(
		&mcp.Tool{
			Name:        "get_schema",
			Description: "Returns available columns and sample values from recent log entries.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			schema := buildSchema(cfg, st)
			result := struct {
				Instance string         `json:"instance"`
				Context  string         `json:"context,omitempty"`
				Columns  []columnSchema `json:"columns"`
			}{
				Instance: name,
				Context:  cfg.MCP.Context,
				Columns:  schema,
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			return toolText(string(b)), nil
		},
	)

	// stats
	srv.AddTool(
		&mcp.Tool{
			Name:        "stats",
			Description: "Get a quick overview of log data: total entries, severity breakdown, active field values (e.g. symbols, components), and time range. Use this first to understand what's in the logs before searching.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			stats := buildStats(cfg, st, name)
			b, _ := json.MarshalIndent(stats, "", "  ")
			return toolText(string(b)), nil
		},
	)
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
		return entry.RawTimestamp
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

func toolText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}

func toToolResult(instance string, entries []*parser.Entry, exclude map[string]bool) (*mcp.CallToolResult, error) {
	type entryJSON struct {
		Timestamp string            `json:"timestamp"`
		Severity  string            `json:"severity"`
		Body      string            `json:"body"`
		Fields    map[string]string `json:"fields,omitempty"`
	}

	result := struct {
		Instance string      `json:"instance"`
		Count    int         `json:"count"`
		Entries  []entryJSON `json:"entries"`
	}{
		Instance: instance,
		Count:    len(entries),
		Entries:  make([]entryJSON, len(entries)),
	}
	for i, e := range entries {
		fields := e.Fields
		if len(exclude) > 0 {
			fields = filterFields(e.Fields, exclude)
		}
		result.Entries[i] = entryJSON{
			Timestamp: e.RawTimestamp,
			Severity:  e.Severity,
			Body:      e.Body,
			Fields:    fields,
		}
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return toolText(string(b)), nil
}

func filterFields(fields map[string]string, exclude map[string]bool) map[string]string {
	filtered := make(map[string]string, len(fields))
	for k, v := range fields {
		if !exclude[k] {
			filtered[k] = v
		}
	}
	return filtered
}

func availableFieldNames(cfg *config.Config) []string {
	var names []string
	for _, col := range cfg.Columns {
		st := col.SourceType()
		if st == "field" || st == "span_field" {
			names = append(names, col.SourceField())
		}
	}
	return names
}

type statsResult struct {
	Instance   string                  `json:"instance"`
	Total      int                     `json:"total_entries"`
	TimeRange  *timeRange              `json:"time_range,omitempty"`
	Severity   map[string]int          `json:"severity"`
	FieldStats map[string][]fieldCount `json:"fields"`
}

type timeRange struct {
	Oldest string `json:"oldest"`
	Newest string `json:"newest"`
}

type fieldCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

func buildStats(cfg *config.Config, st *store.Store, instance string) statsResult {
	severity := make(map[string]int)
	fieldCounts := make(map[string]map[string]int) // field_name -> value -> count

	// Collect field names from config
	fieldNames := availableFieldNames(cfg)
	for _, f := range fieldNames {
		fieldCounts[f] = make(map[string]int)
	}

	var oldest, newest time.Time
	total := 0

	st.ForEach(func(e *parser.Entry) {
		total++
		sev := e.Severity
		if sev == "" {
			sev = "UNKNOWN"
		}
		severity[sev]++

		if !e.Timestamp.IsZero() {
			if oldest.IsZero() || e.Timestamp.Before(oldest) {
				oldest = e.Timestamp
			}
			if newest.IsZero() || e.Timestamp.After(newest) {
				newest = e.Timestamp
			}
		}

		for _, f := range fieldNames {
			if v := e.Fields[f]; v != "" {
				fieldCounts[f][v]++
			}
		}
	})

	result := statsResult{
		Instance:   instance,
		Total:      total,
		Severity:   severity,
		FieldStats: make(map[string][]fieldCount),
	}

	if !oldest.IsZero() {
		result.TimeRange = &timeRange{
			Oldest: oldest.Format(time.RFC3339),
			Newest: newest.Format(time.RFC3339),
		}
	}

	// Convert field counts to sorted slices (top 20 per field)
	for f, counts := range fieldCounts {
		var fcs []fieldCount
		for v, c := range counts {
			fcs = append(fcs, fieldCount{Value: v, Count: c})
		}
		sort.Slice(fcs, func(i, j int) bool { return fcs[i].Count > fcs[j].Count })
		if len(fcs) > 20 {
			fcs = fcs[:20]
		}
		if len(fcs) > 0 {
			result.FieldStats[f] = fcs
		}
	}

	return result
}
