package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A refused start names every problem at once.
func TestConfigureReportsEveryProblem(t *testing.T) {
	_, _, err := configure(options{port: 70000, zabbix: true})
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"--port must be 1-65535", "--zabbix needs a token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %q", want, err)
		}
	}
}

func TestSecuritySummaryMarksWhatIsExposed(t *testing.T) {
	cfg := defaultConfig()
	cfg.Listen = "0.0.0.0:8518" // what --bind 0.0.0.0 or a server install gives
	text := strings.Join(securitySummary(cfg, 0), "\n")
	if !strings.Contains(text, "! listen") || !strings.Contains(text, "! login     NOT SET UP") {
		t.Errorf("open, not set up:\n%s", text)
	}
	for _, l := range []string{"127.0.0.1:8518", "[::1]:8518", "localhost:8518"} {
		if !loopbackOnly(l) {
			t.Errorf("%s not seen as this machine only", l)
		}
	}
	cfg.Listen = "127.0.0.1:8518"
	if text := strings.Join(securitySummary(cfg, 2), "\n"); strings.Contains(text, "!") {
		t.Errorf("localhost with accounts flagged:\n%s", text)
	}
}

// check never starts anything and never creates or changes the database.
func TestCheckChangesNothing(t *testing.T) {
	dir := t.TempDir()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var out bytes.Buffer
	if code := runCheck(&out, options{data: dir, port: port, localOnly: true}); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "pingping.db")); err == nil {
		t.Fatal("check created a database")
	}

	// With a real database it reports the accounts, read-only.
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.CreateUser("admin", "correct horse battery", RoleAdmin)
	s.Close()
	out.Reset()
	runCheck(&out, options{data: dir, port: port, localOnly: true})
	if !strings.Contains(out.String(), "1 console account(s)") || !strings.Contains(out.String(), "login     on, 1 account(s)") {
		t.Fatalf("report:\n%s", out.String())
	}
}

func TestCheckFindsWhatWouldStopTheStart(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	var out bytes.Buffer
	code := runCheck(&out, options{data: t.TempDir(), port: ln.Addr().(*net.TCPAddr).Port, localOnly: true, zabbix: true})
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"FAIL  --zabbix needs a token", "cannot listen", "would NOT start (2 problem(s)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
}

func TestGenTokenIsStrongPrivateAndNeverPrinted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zabbix.token")
	var out bytes.Buffer
	if err := genToken(&out, path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	token := strings.TrimSpace(string(b))
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(token) {
		t.Fatalf("token %q", token)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("the token was printed")
	}
	if got, _, err := resolveZabbixToken(true, path, func(string) string { return "" }); err != nil || got != token {
		t.Fatalf("not accepted: %v", err)
	}
	if err := genToken(&out, path); err == nil {
		t.Fatal("overwrote an existing token")
	}
}
