package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

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
	port := fs.Int("port", 9800, "Hub MCP server port")
	install := fs.Bool("install", false, "Install hub as launchd service (starts on login)")
	uninstall := fs.Bool("uninstall", false, "Uninstall hub launchd service")
	fs.Parse(os.Args[2:])

	if *install {
		if err := installLaunchd(*port); err != nil {
			fmt.Fprintf(os.Stderr, "logpond hub: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *uninstall {
		if err := uninstallLaunchd(); err != nil {
			fmt.Fprintf(os.Stderr, "logpond hub: %v\n", err)
			os.Exit(1)
		}
		return
	}

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

const launchdLabel = "com.logpond.hub"

func launchdPlistPath() string {
	home, _ := os.UserHomeDir()
	return home + "/Library/LaunchAgents/" + launchdLabel + ".plist"
}

func installLaunchd(port int) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find logpond binary: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>hub</string>
        <string>--port</string>
        <string>%d</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>/tmp/logpond-hub.log</string>
    <key>StandardOutPath</key>
    <string>/tmp/logpond-hub.log</string>
</dict>
</plist>`, launchdLabel, binPath, port)

	path := launchdPlistPath()
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	cmd := exec.Command("launchctl", "load", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %s (%w)", string(out), err)
	}

	fmt.Fprintf(os.Stderr, "logpond hub: installed and started (port %d)\n", port)
	fmt.Fprintf(os.Stderr, "  plist: %s\n", path)
	fmt.Fprintf(os.Stderr, "  logs:  /tmp/logpond-hub.log\n")
	fmt.Fprintf(os.Stderr, "  MCP:   http://localhost:%d/mcp\n", port)
	return nil
}

func uninstallLaunchd() error {
	path := launchdPlistPath()

	cmd := exec.Command("launchctl", "unload", path)
	cmd.Run() //nolint:errcheck

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	fmt.Fprintln(os.Stderr, "logpond hub: uninstalled")
	return nil
}
