package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestBrowserServer serves the real router, login and CSP included, on a
// random loopback port for the browser tests in browser/. It is skipped
// unless they start it, and stops when they signal it (or after 5 minutes).
func TestBrowserServer(t *testing.T) {
	if os.Getenv("BROWSER_TEST_SERVE") == "" {
		t.Skip("started by the browser tests in browser/")
	}
	s, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateUser("admin", "browser-test-password", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	run := NewRunner(ProbeCfg{Packets: 1, TimeoutMs: 50}, s, NewDetector(s))
	defer run.Stop()
	srv := httptest.NewServer(newMux(&Config{}, s, run))
	defer srv.Close()
	fmt.Println("BROWSER_TEST_URL=" + srv.URL)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	<-ctx.Done()
}
