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
	port       int
}

func New(cfg *config.Config, st *store.Store, port int) *Server {
	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "logpond",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(mcpSrv, cfg, st)

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
		port: port,
	}
}

func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return nil, fmt.Errorf("MCP server failed to bind to port %d: %w", s.port, err)
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
