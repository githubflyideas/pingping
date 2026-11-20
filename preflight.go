package main

// Start-up made easy to get right, without loosening anything (the same three tools
// as fogping and vrf-ping):
//
//   - configure returns ALL its problems at once, so a refused start names
//     everything to fix, not the first thing.
//   - `pingping check` runs the same configuration plus the checks that need the
//     machine (data directory, database, accounts, port) and reports, without
//     starting: run it before restarting a service.
//   - `pingping gen-token FILE` writes a strong random token to a file only its
//     owner can read, so the safe way to configure Zabbix is also the easy one.
//
// And at every start a security summary says what is exposed. Every refusal stays a
// refusal (ISO/IEC 27001:2022 A.8.9, A.8.15, A.8.24; ISO/IEC 25010:2023 user error
// protection).

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// securitySummary describes what this instance exposes. Lines that deserve a second
// look start with "!". accounts is the number of console accounts.
func securitySummary(cfg *Config, accounts int) []string {
	var out []string
	line := func(risky bool, name, text string) {
		mark := " "
		if risky {
			mark = "!"
		}
		out = append(out, fmt.Sprintf("%s %-9s %s", mark, name, text))
	}
	local := loopbackOnly(cfg.Listen)
	if local {
		line(false, "listen", cfg.Listen+" (this machine only)")
	} else {
		line(true, "listen", cfg.Listen+" (every interface: reachable from the network)")
	}
	if !local {
		line(true, "transport", "plain HTTP: logins and pages cross the network unencrypted; put an HTTPS reverse proxy in front")
	}
	switch {
	case accounts > 0:
		line(false, "login", fmt.Sprintf("on, %d account(s)", accounts))
	case local:
		line(false, "login", "not set up yet: the first person to open the console creates the admin account")
	default:
		line(true, "login", "NOT SET UP: the first person on the network to open the console becomes admin; open it now")
	}
	if cfg.ZabbixToken != "" {
		line(false, "zabbix", "on: GET /api/stats with a read-only token")
	} else {
		line(false, "zabbix", "off")
	}
	if runningAsRoot() {
		line(true, "account", "running as root: a flaw anywhere is a flaw with full control of this machine; use an ordinary account")
	}
	return out
}

type machineCheck struct {
	name, detail string
	err          error
}

// runCheck is `pingping check`: it reports on the configuration and the machine and
// exits without starting anything. 0 means pingping would start; 1 means it would not.
func runCheck(w io.Writer, opt options) int {
	fmt.Fprintln(w, "pingping check: nothing is started; this only reports.")
	fmt.Fprintln(w)
	cfg, _, err := configure(opt)
	var problems []string
	if err != nil {
		problems = strings.Split(err.Error(), "\n")
	}
	fmt.Fprintln(w, "configuration")
	if len(problems) == 0 {
		fmt.Fprintln(w, "  ok    flags and tokens")
	}
	for _, p := range problems {
		fmt.Fprintf(w, "  FAIL  %s\n", p)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "this machine")
	checks, accounts := checkMachine(cfg)
	failed := len(problems)
	for _, c := range checks {
		status, detail := "ok  ", c.detail
		if c.err != nil {
			status, detail = "FAIL", c.err.Error()
			failed++
		}
		fmt.Fprintf(w, "  %s  %-9s %s\n", status, c.name, detail)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "security summary")
	for _, l := range securitySummary(cfg, accounts) {
		fmt.Fprintln(w, "  "+l)
	}
	fmt.Fprintln(w)
	if failed > 0 {
		fmt.Fprintf(w, "result: pingping would NOT start (%d problem(s) above)\n", failed)
		return 1
	}
	fmt.Fprintln(w, "result: pingping would start")
	return 0
}

// checkMachine looks at what the start-up needs from this machine without changing
// anything: the database is opened read-only. It also returns the number of console
// accounts, for the summary.
func checkMachine(cfg *Config) ([]machineCheck, int) {
	var out []machineCheck
	out = append(out, machineCheck{name: "data", detail: cfg.DataDir + " is writable", err: checkDataDir(cfg.DataDir)})

	accounts := 0
	path := filepath.Join(cfg.DataDir, "pingping.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		out = append(out, machineCheck{name: "database", detail: "none yet; a new one will be created"})
	} else if n, err := countAccounts(path); err != nil {
		out = append(out, machineCheck{name: "database", err: fmt.Errorf("cannot read %s: %v", path, err)})
	} else {
		accounts = n
		out = append(out, machineCheck{name: "database", detail: fmt.Sprintf("readable, %d console account(s)", n)})
	}

	if ln, err := net.Listen("tcp", cfg.Listen); err != nil {
		out = append(out, machineCheck{name: "port", err: fmt.Errorf("cannot listen on %s: %v", cfg.Listen, err)})
	} else {
		ln.Close()
		out = append(out, machineCheck{name: "port", detail: cfg.Listen + " is free"})
	}
	return out, accounts
}

// countAccounts opens the database read-only and counts the console accounts.
func countAccounts(path string) (int, error) {
	db, err := sql.Open(sqlDriver, "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// checkDataDir says whether the data directory exists and can be written, or can be
// created. It writes one empty file and removes it again.
func checkDataDir(dir string) error {
	fi, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		parent := filepath.Dir(filepath.Clean(dir))
		if err := writable(parent); err != nil {
			return fmt.Errorf("%s does not exist and cannot be created: %v", dir, err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return writable(dir)
}

func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".pingping-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// genToken writes a new random token to path, readable by its owner only. It never
// overwrites a file (an existing token may be in use) and never prints the token:
// it would end up in the terminal's scrollback and in logs.
func genToken(w io.Writer, path string) error {
	b := make([]byte, 32) // 256 bits from the operating system's CSPRNG
	if _, err := rand.Read(b); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; not overwriting it (it may be in use). Remove it first, or choose another path", path)
		}
		return err
	}
	if _, err := f.WriteString(hex.EncodeToString(b) + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Windows does not apply Unix file modes: the file's access comes from the
		// folder it is in. Say so, rather than claim a protection that is not there.
		fmt.Fprintf(w, "wrote a new 64-character token to %s.\n", path)
		fmt.Fprintf(w, "NOTE: Windows does not support owner-only file modes; who can read this file depends on its folder.\n"+
			"Keep it in a folder only the account that runs pingping (and administrators) can open.\n")
	} else {
		fmt.Fprintf(w, "wrote a new 64-character token to %s (readable by you only).\n", path)
	}
	fmt.Fprintf(w, "start pingping with it:\n  pingping run --zabbix --zabbix-token-file %s\n", path)
	fmt.Fprintf(w, "and give the same token to the Zabbix template ({$FOGPING.TOKEN}).\n")
	return nil
}

// loopbackOnly says whether a listen address accepts connections from this machine
// only: a loopback IP (127.x, ::1) or the name localhost.
func loopbackOnly(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// runningAsRoot is a variable so tests can pretend. On Windows Geteuid is -1, so a
// service account is never reported as root.
var runningAsRoot = func() bool { return os.Geteuid() == 0 }
