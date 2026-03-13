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
