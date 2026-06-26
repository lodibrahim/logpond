package mcpsvr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/store"
)

type Server struct {
	httpServer *http.Server
}

func New(cfg *config.Config, st *store.Store, port int, name string) *Server {
	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "logpond",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(mcpSrv, cfg, st, name)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpSrv },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	return &Server{
		httpServer: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}
}

// Listen binds the MCP server's TCP port. Port 0 (the default) binds ":0", so
// the OS assigns a free port and co-located instances never collide; the hub
// discovers the actual port from the registry file. An explicit --mcp-port binds
// exactly that port and fails fast on conflict.
func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return nil, fmt.Errorf("MCP server failed to bind to %s: %w", s.httpServer.Addr, err)
	}
	return ln, nil
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutCtx) //nolint:errcheck
	}()

	return s.httpServer.Serve(ln)
}
