package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lodibrahim/logpond/internal/registration"
)

func registerHubTools(srv *mcp.Server, h *Hub) {
	// list_instances — uses liveInstances for consistent discovery
	srv.AddTool(
		&mcp.Tool{
			Name:        "list_instances",
			Description: "List all discovered logpond instances and their status.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			live := h.liveInstances()
			if len(live) == 0 {
				return toolText("No logpond instances found."), nil
			}
			var out []instanceStatus
			for _, info := range live {
				out = append(out, instanceStatus{
					Name:      info.Name,
					Port:      info.Port,
					PID:       info.PID,
					Status:    "alive",
					StartedAt: info.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
				})
			}
			b, _ := json.MarshalIndent(map[string]any{"instances": out}, "", "  ")
			return toolText(string(b)), nil
		},
	)

	// stats — fan out to all, wrap per-instance results
	srv.AddTool(
		&mcp.Tool{
			Name:        "stats",
			Description: "Get stats from all running logpond instances: total entries, severity breakdown, field values, time range.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			instances, errResult := h.resolveInstances("")
			if errResult != nil {
				return errResult, nil
			}
			results := h.fanOut(ctx, instances, "stats", nil)
			return wrapInstances(results), nil
		},
	)

	// search_logs — fan out (optionally filtered by instance), merge entries by timestamp
	srv.AddTool(
		&mcp.Tool{
			Name:        "search_logs",
			Description: "Search log entries across all running logpond instances. Supports text regex, field filters, severity, and time range. Use 'instance' to target a specific instance.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"instance":   map[string]any{"type": "string", "description": "Target a specific instance by name (omit to search all)"},
					"text":       map[string]any{"type": "string", "description": "Regex to match against log body and field values"},
					"fields":     map[string]any{"type": "object", "description": "Field name -> value exact matches"},
					"level":      map[string]any{"type": "string", "description": "Severity filter (INFO, WARN, ERROR, DEBUG)"},
					"after":      map[string]any{"type": "string", "description": "ISO 8601 — entries after this time"},
					"before":     map[string]any{"type": "string", "description": "ISO 8601 — entries before this time"},
					"limit":      map[string]any{"type": "integer", "description": "Max results to return"},
					"count_only": map[string]any{"type": "boolean", "description": "Return only match count (saves tokens)"},
				},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				args = make(map[string]any)
			}

			instanceFilter, _ := args["instance"].(string)
			delete(args, "instance")

			limit, _ := args["limit"].(float64)
			countOnly, _ := args["count_only"].(bool)

			instances, errResult := h.resolveInstances(instanceFilter)
			if errResult != nil {
				return errResult, nil
			}

			forwardArgs, _ := json.Marshal(args)
			results := h.fanOut(ctx, instances, "search_logs", forwardArgs)

			if countOnly {
				return mergeCountOnly(results), nil
			}
			return mergeEntries(results, int(limit)), nil
		},
	)

	// get_schema — fan out to all, wrap per-instance results
	srv.AddTool(
		&mcp.Tool{
			Name:        "get_schema",
			Description: "Get column schemas and sample values from all running logpond instances.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			instances, errResult := h.resolveInstances("")
			if errResult != nil {
				return errResult, nil
			}
			results := h.fanOut(ctx, instances, "get_schema", nil)
			return wrapInstances(results), nil
		},
	)

	// tail — fan out to all, merge entries by timestamp, take last N
	srv.AddTool(
		&mcp.Tool{
			Name:        "tail",
			Description: "Return the last N log entries merged across all running logpond instances.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"n":        map[string]any{"type": "integer", "description": "Number of entries (default: 10)"},
					"instance": map[string]any{"type": "string", "description": "Target a specific instance by name (omit for all)"},
				},
			},
		},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				args = make(map[string]any)
			}

			instanceFilter, _ := args["instance"].(string)
			delete(args, "instance")

			n := 10
			if v, ok := args["n"].(float64); ok && v > 0 {
				n = int(v)
			}

			instances, errResult := h.resolveInstances(instanceFilter)
			if errResult != nil {
				return errResult, nil
			}

			forwardArgs, _ := json.Marshal(args)
			results := h.fanOut(ctx, instances, "tail", forwardArgs)
			return mergeEntries(results, n), nil
		},
	)
}

// --- helpers ---

type instanceStatus struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
}


// resolveInstances returns live instances, optionally filtered by name.
// Returns nil result on success, or an MCP error result if no instances found.
func (h *Hub) resolveInstances(filter string) ([]registration.InstanceInfo, *mcp.CallToolResult) {
	instances := h.liveInstances()
	if len(instances) == 0 {
		return nil, toolText("No live instances found.")
	}
	if filter != "" {
		var filtered []registration.InstanceInfo
		for _, info := range instances {
			if info.Name == filter {
				filtered = append(filtered, info)
			}
		}
		if len(filtered) == 0 {
			return nil, toolText(fmt.Sprintf("Instance %q not found.", filter))
		}
		return filtered, nil
	}
	return instances, nil
}

func fanOutErrorJSON(instance string, err error) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"instance": instance, "error": err.Error()})
	return b
}

func extractText(result *mcp.CallToolResult) string {
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func toolText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// wrapInstances collects raw JSON responses from each instance into {"instances": [...]}.
func wrapInstances(results []fanOutResult) *mcp.CallToolResult {
	var instances []json.RawMessage
	for _, r := range results {
		if r.Err != nil {
			instances = append(instances, fanOutErrorJSON(r.Instance, r.Err))
			continue
		}
		text := extractText(r.Result)
		if text != "" {
			instances = append(instances, json.RawMessage(text))
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"instances": instances}, "", "  ")
	return toolText(string(b))
}

// mergeCountOnly sums counts from search_logs count_only responses.
func mergeCountOnly(results []fanOutResult) *mcp.CallToolResult {
	total := 0
	var perInstance []json.RawMessage
	for _, r := range results {
		if r.Err != nil {
			perInstance = append(perInstance, fanOutErrorJSON(r.Instance, r.Err))
			continue
		}
		text := extractText(r.Result)
		var counts struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &counts); err == nil {
			total += counts.Count
		}
		if text != "" {
			perInstance = append(perInstance, json.RawMessage(text))
		}
	}
	merged := struct {
		TotalCount int               `json:"total_count"`
		Instances  []json.RawMessage `json:"instances"`
	}{TotalCount: total, Instances: perInstance}
	b, _ := json.MarshalIndent(merged, "", "  ")
	return toolText(string(b))
}

type mergedEntry struct {
	Instance  string            `json:"instance"`
	Timestamp string            `json:"timestamp"`
	Severity  string            `json:"severity"`
	Body      string            `json:"body"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// mergeEntries parses entry arrays from each instance result, interleaves by
// timestamp, and applies the limit (keeping the most recent entries).
func mergeEntries(results []fanOutResult, limit int) *mcp.CallToolResult {
	var allEntries []mergedEntry
	var errors []map[string]string

	for _, r := range results {
		if r.Err != nil {
			errors = append(errors, map[string]string{"instance": r.Instance, "error": r.Err.Error()})
			continue
		}
		text := extractText(r.Result)
		var resp struct {
			Instance string `json:"instance"`
			Entries  []struct {
				Timestamp string            `json:"timestamp"`
				Severity  string            `json:"severity"`
				Body      string            `json:"body"`
				Fields    map[string]string `json:"fields,omitempty"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(text), &resp); err != nil {
			// Non-JSON response (e.g., "No matches found.") — not an error, just zero entries
			continue
		}
		instanceName := r.Instance // use fan-out name as source of truth
		for _, e := range resp.Entries {
			allEntries = append(allEntries, mergedEntry{
				Instance:  instanceName,
				Timestamp: e.Timestamp,
				Severity:  e.Severity,
				Body:      e.Body,
				Fields:    e.Fields,
			})
		}
	}

	// Sort chronologically (ascending by timestamp string — RFC3339 is lexicographic)
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].Timestamp < allEntries[j].Timestamp
	})

	// Keep most recent entries
	if limit > 0 && len(allEntries) > limit {
		allEntries = allEntries[len(allEntries)-limit:]
	}

	result := struct {
		Count   int               `json:"count"`
		Entries []mergedEntry     `json:"entries"`
		Errors  []map[string]string `json:"errors,omitempty"`
	}{
		Count:   len(allEntries),
		Entries: allEntries,
		Errors:  errors,
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return toolText(string(b))
}
