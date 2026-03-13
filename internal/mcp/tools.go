package mcpsvr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/search"
	"github.com/lodibrahim/logpond/internal/store"
)

func registerTools(srv *mcp.Server, cfg *config.Config, st *store.Store, name string) {
	// search_logs
	srv.AddTool(
		&mcp.Tool{
			Name:        "search_logs",
			Description: "Search log entries by text regex, field values, severity level, and time range. All filters are AND-ed.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":   map[string]any{"type": "string", "description": "Regex to match against log body"},
					"fields": map[string]any{"type": "object", "description": "Field name → value exact matches"},
					"level":  map[string]any{"type": "string", "description": "Severity level filter (INFO, WARN, etc.)"},
					"after":  map[string]any{"type": "string", "description": "ISO 8601 timestamp — return entries after this time"},
					"before": map[string]any{"type": "string", "description": "ISO 8601 timestamp — return entries before this time"},
					"limit":  map[string]any{"type": "integer", "description": "Max results to return (default: all)"},
				},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			if len(results) == 0 {
				return toolText(fmt.Sprintf("[%s] No matches found.", name)), nil
			}

			return toToolResult(name, results)
		},
	)

	// tail
	srv.AddTool(
		&mcp.Tool{
			Name:        "tail",
			Description: "Return the last N log entries.",
			InputSchema: map[string]any{
				"type":       "object",
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
			return toToolResult(name, entries)
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
				Columns  []columnSchema `json:"columns"`
			}{
				Instance: name,
				Columns:  schema,
			}
			b, _ := json.MarshalIndent(result, "", "  ")
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

func toToolResult(instance string, entries []*parser.Entry) (*mcp.CallToolResult, error) {
	type entryJSON struct {
		Timestamp string            `json:"timestamp"`
		Severity  string            `json:"severity"`
		Body      string            `json:"body"`
		Fields    map[string]string `json:"fields"`
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
		result.Entries[i] = entryJSON{
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
	return toolText(string(b)), nil
}
