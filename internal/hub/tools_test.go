package hub

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func makeResult(instance, jsonText string) fanOutResult {
	return fanOutResult{
		Instance: instance,
		Result: &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: jsonText},
			},
		},
	}
}

func makeError(instance, msg string) fanOutResult {
	return fanOutResult{
		Instance: instance,
		Err:      &testError{msg},
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestMergeEntriesSortsByTimestamp(t *testing.T) {
	results := []fanOutResult{
		makeResult("app-a", `{"instance":"app-a","count":2,"entries":[
			{"timestamp":"2026-03-13T10:00:02Z","severity":"INFO","body":"second"},
			{"timestamp":"2026-03-13T10:00:04Z","severity":"INFO","body":"fourth"}
		]}`),
		makeResult("app-b", `{"instance":"app-b","count":2,"entries":[
			{"timestamp":"2026-03-13T10:00:01Z","severity":"WARN","body":"first"},
			{"timestamp":"2026-03-13T10:00:03Z","severity":"ERROR","body":"third"}
		]}`),
	}

	result := mergeEntries(results, 0)
	text := extractText(result)

	var resp struct {
		Count   int           `json:"count"`
		Entries []mergedEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Count != 4 {
		t.Errorf("count = %d, want 4", resp.Count)
	}

	expected := []string{"first", "second", "third", "fourth"}
	for i, e := range resp.Entries {
		if e.Body != expected[i] {
			t.Errorf("entry[%d].Body = %q, want %q", i, e.Body, expected[i])
		}
	}
}

func TestMergeEntriesAppliesLimit(t *testing.T) {
	results := []fanOutResult{
		makeResult("app", `{"instance":"app","count":3,"entries":[
			{"timestamp":"2026-03-13T10:00:01Z","severity":"INFO","body":"old"},
			{"timestamp":"2026-03-13T10:00:02Z","severity":"INFO","body":"mid"},
			{"timestamp":"2026-03-13T10:00:03Z","severity":"INFO","body":"new"}
		]}`),
	}

	result := mergeEntries(results, 2)
	text := extractText(result)

	var resp struct {
		Count   int           `json:"count"`
		Entries []mergedEntry `json:"entries"`
	}
	json.Unmarshal([]byte(text), &resp)

	if resp.Count != 2 {
		t.Errorf("count = %d, want 2", resp.Count)
	}
	if resp.Entries[0].Body != "mid" {
		t.Errorf("first entry = %q, want %q (most recent 2)", resp.Entries[0].Body, "mid")
	}
}

func TestMergeEntriesUseFanOutInstanceName(t *testing.T) {
	results := []fanOutResult{
		makeResult("correct-name", `{"instance":"wrong-name","count":1,"entries":[
			{"timestamp":"2026-03-13T10:00:01Z","severity":"INFO","body":"test"}
		]}`),
	}

	result := mergeEntries(results, 0)
	text := extractText(result)

	var resp struct {
		Entries []mergedEntry `json:"entries"`
	}
	json.Unmarshal([]byte(text), &resp)

	if resp.Entries[0].Instance != "correct-name" {
		t.Errorf("instance = %q, want %q", resp.Entries[0].Instance, "correct-name")
	}
}

func TestMergeEntriesHandlesNonJSON(t *testing.T) {
	results := []fanOutResult{
		makeResult("app-a", `[app-a] No matches found.`),
		makeResult("app-b", `{"instance":"app-b","count":1,"entries":[
			{"timestamp":"2026-03-13T10:00:01Z","severity":"INFO","body":"found"}
		]}`),
	}

	result := mergeEntries(results, 0)
	text := extractText(result)

	var resp struct {
		Count   int           `json:"count"`
		Entries []mergedEntry `json:"entries"`
	}
	json.Unmarshal([]byte(text), &resp)

	if resp.Count != 1 {
		t.Errorf("count = %d, want 1 (non-JSON skipped)", resp.Count)
	}
}

func TestMergeEntriesIncludesErrors(t *testing.T) {
	results := []fanOutResult{
		makeError("dead-app", "connection refused"),
		makeResult("live-app", `{"instance":"live-app","count":1,"entries":[
			{"timestamp":"2026-03-13T10:00:01Z","severity":"INFO","body":"ok"}
		]}`),
	}

	result := mergeEntries(results, 0)
	text := extractText(result)

	var resp struct {
		Count  int               `json:"count"`
		Errors []json.RawMessage `json:"errors"`
	}
	json.Unmarshal([]byte(text), &resp)

	if resp.Count != 1 {
		t.Errorf("count = %d, want 1", resp.Count)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(resp.Errors))
	}
}

func TestMergeCountOnly(t *testing.T) {
	results := []fanOutResult{
		makeResult("app-a", `{"instance":"app-a","count":10}`),
		makeResult("app-b", `{"instance":"app-b","count":25}`),
	}

	result := mergeCountOnly(results)
	text := extractText(result)

	var resp struct {
		TotalCount int `json:"total_count"`
	}
	json.Unmarshal([]byte(text), &resp)

	if resp.TotalCount != 35 {
		t.Errorf("total_count = %d, want 35", resp.TotalCount)
	}
}

func TestMergeCountOnlyWithErrors(t *testing.T) {
	results := []fanOutResult{
		makeResult("app-a", `{"instance":"app-a","count":10}`),
		makeError("app-b", "timeout"),
	}

	result := mergeCountOnly(results)
	text := extractText(result)

	var resp struct {
		TotalCount int               `json:"total_count"`
		Instances  []json.RawMessage `json:"instances"`
	}
	json.Unmarshal([]byte(text), &resp)

	if resp.TotalCount != 10 {
		t.Errorf("total_count = %d, want 10", resp.TotalCount)
	}
	if len(resp.Instances) != 2 {
		t.Errorf("instances = %d, want 2 (one result + one error)", len(resp.Instances))
	}
}

func TestWrapInstances(t *testing.T) {
	results := []fanOutResult{
		makeResult("app-a", `{"instance":"app-a","total":100}`),
		makeError("app-b", "down"),
	}

	result := wrapInstances(results)
	text := extractText(result)

	var resp struct {
		Instances []json.RawMessage `json:"instances"`
	}
	json.Unmarshal([]byte(text), &resp)

	if len(resp.Instances) != 2 {
		t.Errorf("instances = %d, want 2", len(resp.Instances))
	}
}

func TestFanOutErrorJSON(t *testing.T) {
	b := fanOutErrorJSON("myapp", &testError{"broke"})
	var m map[string]string
	json.Unmarshal(b, &m)

	if m["instance"] != "myapp" {
		t.Errorf("instance = %q, want %q", m["instance"], "myapp")
	}
	if m["error"] != "broke" {
		t.Errorf("error = %q, want %q", m["error"], "broke")
	}
}
