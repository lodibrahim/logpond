package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lodibrahim/logpond/internal/registration"
)

// Hub discovers running logpond instances and serves a single MCP endpoint
// that fans out queries to all live instances.
type Hub struct {
	port     int
	mu       sync.Mutex
	sessions map[string]*cachedSession
}

type cachedSession struct {
	info    registration.InstanceInfo
	client  *mcp.Client
	session *mcp.ClientSession
}

// New creates a hub that will serve on the given port.
func New(port int) *Hub {
	return &Hub{
		port:     port,
		sessions: make(map[string]*cachedSession),
	}
}

// liveInstances returns all registered instances whose PIDs are still alive.
// Stale registration files are cleaned up.
func (h *Hub) liveInstances() []registration.InstanceInfo {
	all, err := registration.Discover()
	if err != nil {
		return nil
	}
	var live []registration.InstanceInfo
	for _, info := range all {
		if registration.IsAlive(info.PID) {
			live = append(live, info)
		} else {
			registration.Deregister(info.Name)
			fmt.Fprintf(os.Stderr, "logpond hub: cleaned stale instance %q (pid %d)\n", info.Name, info.PID)
			// Remove cached session if any
			h.mu.Lock()
			key := sessionKey(info)
			if cs, ok := h.sessions[key]; ok {
				cs.session.Close()
				delete(h.sessions, key)
			}
			h.mu.Unlock()
		}
	}
	return live
}

func sessionKey(info registration.InstanceInfo) string {
	return fmt.Sprintf("%s:%d", info.Name, info.PID)
}

// getSession returns a cached MCP client session for the instance,
// creating one if needed.
func (h *Hub) getSession(ctx context.Context, info registration.InstanceInfo) (*mcp.ClientSession, error) {
	key := sessionKey(info)

	h.mu.Lock()
	if cs, ok := h.sessions[key]; ok {
		h.mu.Unlock()
		return cs.session, nil
	}
	h.mu.Unlock()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "logpond-hub",
		Version: "0.1.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: fmt.Sprintf("http://localhost:%d/mcp", info.Port),
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	h.sessions[key] = &cachedSession{
		info:    info,
		client:  client,
		session: session,
	}
	h.mu.Unlock()

	return session, nil
}

// fanOutResult holds the result from a single instance call.
type fanOutResult struct {
	Instance string
	Result   *mcp.CallToolResult
	Err      error
}

// fanOut calls the named tool on each instance in parallel and collects results.
func (h *Hub) fanOut(ctx context.Context, instances []registration.InstanceInfo, toolName string, rawArgs json.RawMessage) []fanOutResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ch := make(chan fanOutResult, len(instances))
	for _, info := range instances {
		go func(info registration.InstanceInfo) {
			session, err := h.getSession(ctx, info)
			if err != nil {
				ch <- fanOutResult{Instance: info.Name, Err: err}
				return
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      toolName,
				Arguments: rawArgs,
			})
			if err != nil {
				// Evict broken session
				h.mu.Lock()
				key := sessionKey(info)
				if cs, ok := h.sessions[key]; ok {
					cs.session.Close()
					delete(h.sessions, key)
				}
				h.mu.Unlock()
			}
			ch <- fanOutResult{Instance: info.Name, Result: result, Err: err}
		}(info)
	}

	results := make([]fanOutResult, 0, len(instances))
	for range instances {
		results = append(results, <-ch)
	}
	return results
}

// Run starts the hub MCP server and blocks until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) error {
	srv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "logpond-hub",
			Version: "0.1.0",
		},
		nil,
	)

	registerHubTools(srv, h)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", h.port),
		Handler: mux,
	}

	ln, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("hub failed to bind to port %d: %w", h.port, err)
	}
	fmt.Fprintf(os.Stderr, "logpond hub: MCP server on http://localhost:%d/mcp\n", h.port)

	go func() {
		<-ctx.Done()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer shutCancel()
		httpServer.Shutdown(shutCtx) //nolint:errcheck
	}()

	if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
