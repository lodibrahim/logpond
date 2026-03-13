package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Name    string         `yaml:"name"`
	Type    string         `yaml:"type"`
	Mapping MappingConfig  `yaml:"mapping"`
	Columns []ColumnConfig `yaml:"columns"`
	MCP     MCPConfig      `yaml:"mcp"`
}

type MCPConfig struct {
	ExcludeFields []string `yaml:"exclude_fields"`
	DefaultLimit  int      `yaml:"default_limit"`
	Context       string   `yaml:"context"`
}

type MappingConfig struct {
	Timestamp        FieldRef `yaml:"timestamp"`
	Severity         FieldRef `yaml:"severity"`
	Body             FieldRef `yaml:"body"`
	AutoMapRemaining bool     `yaml:"auto_map_remaining"`
}

type FieldRef struct {
	Field      string `yaml:"field"`
	TimeFormat string `yaml:"time_format,omitempty"`
}

type ColumnConfig struct {
	Name    string   `yaml:"name"`
	Source  string   `yaml:"source"`
	Format  string   `yaml:"format,omitempty"`
	Width   int      `yaml:"width,omitempty"`
	Flex    bool     `yaml:"flex,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

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

func (c *ColumnConfig) SourceField() string {
	if i := strings.IndexByte(c.Source, ':'); i >= 0 {
		return c.Source[i+1:]
	}
	return ""
}

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

	warnUnsupported(data)

	return &cfg, nil
}

func (c *Config) Validate() error {
	if len(c.Columns) == 0 {
		return fmt.Errorf("config: no columns defined")
	}

	flexCount := 0
	for _, col := range c.Columns {
		if col.Flex {
			flexCount++
		}
	}
	if flexCount != 1 {
		return fmt.Errorf("config: exactly one column must have flex: true (found %d)", flexCount)
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
