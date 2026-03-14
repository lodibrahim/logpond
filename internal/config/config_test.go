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

func TestLoadMissing(t *testing.T) {
	_, err := Load("nonexistent.yaml")
	if err == nil {
		t.Error("Load should fail for missing file")
	}
}

func TestLoadInvalid(t *testing.T) {
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
