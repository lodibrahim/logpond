package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/hub"
	mcpsvr "github.com/lodibrahim/logpond/internal/mcp"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/registration"
	"github.com/lodibrahim/logpond/internal/store"
	"github.com/lodibrahim/logpond/internal/tui"
)

func main() {
	// Detect hub subcommand before flag.Parse()
	if len(os.Args) > 1 && os.Args[1] == "hub" {
		runHub()
		return
	}

	configPath := flag.String("config", "", "Path to YAML config file (required)")
	bufferSize := flag.Int("buffer", 50000, "Ring buffer capacity")
	mcpPort := flag.Int("mcp-port", 9876, "MCP server port")
	name := flag.String("name", "", "Instance name — overrides config name (shown in MCP responses)")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config is required")
		os.Exit(1)
	}

	// Check stdin is a pipe
	stat, err := os.Stdin.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot stat stdin: %v\n", err)
		os.Exit(1)
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		fmt.Fprintln(os.Stderr, "usage: app | logpond --config ./config.yaml")
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Resolve instance name: CLI flag > config name > "logpond"
	instanceName := *name
	if instanceName == "" {
		instanceName = cfg.Name
	}
	if instanceName == "" {
		instanceName = "logpond"
	}

	// Create components
	p := parser.New(cfg)
	st := store.New(*bufferSize)

	// Create TUI — WithInputTTY opens /dev/tty for keyboard input (stdin is the pipe)
	model := tui.New(cfg, p, st)
	program := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithInputTTY(),
		tea.WithMouseCellMotion(),
	)

	// Context for shutdown coordination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Bind MCP server port synchronously (fail fast on port conflict)
	mcp := mcpsvr.New(cfg, st, *mcpPort, instanceName)
	ln, err := mcp.Listen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "logpond: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "logpond [%s]: MCP server on http://localhost:%d/mcp\n", instanceName, *mcpPort)

	// Register instance for hub discovery
	if err := registration.Register(instanceName, *mcpPort); err != nil {
		fmt.Fprintf(os.Stderr, "logpond: warning: failed to register instance: %v\n", err)
	}
	defer registration.Deregister(instanceName)

	// Auto-spawn hub if not already running
	ensureHub()

	// Start MCP server in background
	go func() {
		if err := mcp.Serve(ctx, ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "logpond: MCP server error: %v\n", err)
		}
	}()

	// Start stdin reader
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			entry, err := p.Parse(line)
			if err != nil {
				continue
			}
			st.Append(entry)
			program.Send(tui.NewEntryMsg{})
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "logpond: stdin read error: %v\n", err)
		}
		program.Send(tui.InputClosedMsg{})
	}()

	// Handle SIGINT/SIGTERM for clean exit
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		cancel()
		program.Kill()
	}()

	// Run TUI (blocks until quit)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

func runHub() {
	fs := flag.NewFlagSet("hub", flag.ExitOnError)
	port := fs.Int("port", defaultHubPort, "Hub MCP server port")
	fs.Parse(os.Args[2:])

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	h := hub.New(*port)
	if err := h.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "logpond hub: %v\n", err)
		os.Exit(1)
	}
}

const defaultHubPort = 9800

// ensureHub checks if the hub is running on the default port.
// If not, it spawns one as a detached background process.
func ensureHub() {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", defaultHubPort), 200*time.Millisecond)
	if err == nil {
		conn.Close()
		return // hub already running
	}

	binPath, err := os.Executable()
	if err != nil {
		return // silently skip
	}

	cmd := exec.Command(binPath, "hub", "--port", fmt.Sprintf("%d", defaultHubPort))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from parent
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "logpond: warning: failed to start hub: %v\n", err)
		return
	}
	// Release the child so it survives after this process exits
	cmd.Process.Release()
	fmt.Fprintf(os.Stderr, "logpond: hub started on http://localhost:%d/mcp\n", defaultHubPort)
}
