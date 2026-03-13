package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/lodibrahim/logpond/internal/parser"
)

func makeEntry(i int, severity, body string) *parser.Entry {
	return &parser.Entry{
		Timestamp: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
		Severity:  severity,
		Body:      body,
		Fields:    map[string]string{"symbol": fmt.Sprintf("SYM%d", i%3)},
		Raw:       fmt.Sprintf(`{"i":%d}`, i),
	}
}

func TestAppendAndLen(t *testing.T) {
	s := New(10)
	s.Append(makeEntry(1, "INFO", "hello"))
	s.Append(makeEntry(2, "WARN", "world"))
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
}

func TestEviction(t *testing.T) {
	s := New(3)
	for i := 0; i < 5; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	if s.Len() != 3 {
		t.Errorf("Len = %d, want 3", s.Len())
	}
	entries := s.Tail(3)
	if entries[0].Body != "msg2" {
		t.Errorf("First entry = %q, want %q", entries[0].Body, "msg2")
	}
}

func TestTail(t *testing.T) {
	s := New(100)
	for i := 0; i < 10; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	entries := s.Tail(3)
	if len(entries) != 3 {
		t.Fatalf("Tail(3) len = %d, want 3", len(entries))
	}
	if entries[0].Body != "msg7" {
		t.Errorf("Tail[0] = %q, want %q", entries[0].Body, "msg7")
	}
	if entries[2].Body != "msg9" {
		t.Errorf("Tail[2] = %q, want %q", entries[2].Body, "msg9")
	}
}

func TestTailMoreThanBuffer(t *testing.T) {
	s := New(100)
	for i := 0; i < 5; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	entries := s.Tail(100)
	if len(entries) != 5 {
		t.Errorf("Tail(100) len = %d, want 5", len(entries))
	}
}

func TestAll(t *testing.T) {
	s := New(100)
	for i := 0; i < 5; i++ {
		s.Append(makeEntry(i, "INFO", fmt.Sprintf("msg%d", i)))
	}
	entries := s.All()
	if len(entries) != 5 {
		t.Fatalf("All len = %d, want 5", len(entries))
	}
	if entries[0].Body != "msg0" {
		t.Errorf("All[0] = %q, want %q", entries[0].Body, "msg0")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New(1000)
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 500; i++ {
			s.Append(makeEntry(i, "INFO", "concurrent"))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = s.Tail(10)
			_ = s.Len()
		}
		done <- true
	}()

	<-done
	<-done
}
