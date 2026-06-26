package mcpsvr

import (
	"fmt"
	"net"
	"net/http"
	"testing"
)

// Port 0 (the default) auto-selects a free OS-assigned port.
func TestServerListen_AutoSelectsFreePort(t *testing.T) {
	s := &Server{httpServer: &http.Server{Addr: ":0"}}
	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("auto listen: %v", err)
	}
	defer ln.Close()
	if got := ln.Addr().(*net.TCPAddr).Port; got == 0 {
		t.Fatal("expected a real OS-assigned port, got 0")
	}
}

// An explicit (non-zero) port binds exactly that port and fails fast on conflict.
func TestServerListen_ExplicitPortFailsFastOnConflict(t *testing.T) {
	busy, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("occupy listen: %v", err)
	}
	defer busy.Close()
	taken := busy.Addr().(*net.TCPAddr).Port

	s := &Server{httpServer: &http.Server{Addr: fmt.Sprintf(":%d", taken)}}
	if _, err := s.Listen(); err == nil {
		t.Fatal("expected fail-fast error binding an explicit busy port")
	}
}
