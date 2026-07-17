package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// pingping 2.0: a probe, text files, and a smoke graph. Nothing else.
type Config struct {
	Listen        string      `json:"listen"`
	DataDir       string      `json:"data_dir"`
	TargetsDir    string      `json:"targets_dir"`
	Targets       []TargetCfg `json:"targets"`
	Probe         ProbeCfg    `json:"probe"`
	RetentionDays int         `json:"retention_days"` // raw JSONL kept this long, then rm
}

type TargetCfg struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "icmp" (default) | "tcp"
	Host        string `json:"host"`
	Port        int    `json:"port"`         // tcp only
	Pace        string `json:"pace"`         // "fast"(15s) | "normal"(60s) | "slow"(300s)
	IntervalSec int    `json:"interval_sec"` // explicit seconds, highest priority
	dir         string
}

type ProbeCfg struct {
	IntervalSec int `json:"interval_sec"`
	Packets     int `json:"packets"`
	GapMs       int `json:"gap_ms"`
	TimeoutMs   int `json:"timeout_ms"`
}

var dirSan = regexp.MustCompile(`[^a-zA-Z0-9._\p{Han}-]`)

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Listen:        "0.0.0.0:8517",
		DataDir:       "./data",
		Probe:         ProbeCfg{IntervalSec: 60, Packets: 20, GapMs: 50, TimeoutMs: 1000},
		RetentionDays: 300,
	}
	if err := json.Unmarshal(stripJSONC(raw), cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.TargetsDir == "" {
		cfg.TargetsDir = "./targets"
	}
	listTargets, err := loadTargetLists(cfg.TargetsDir)
	if err != nil {
		return nil, err
	}
	cfg.Targets = append(cfg.Targets, listTargets...)
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("no targets: add one to %s/ping.list or tcp.list", cfg.TargetsDir)
	}
	seen := map[string]bool{}
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if t.Name == "" {
			return nil, fmt.Errorf("targets[%d] missing name", i)
		}
		switch t.Type {
		case "", "icmp":
			t.Type = "icmp"
		case "tcp":
			if t.Port <= 0 {
				return nil, fmt.Errorf("tcp target %q needs a port", t.Name)
			}
		default:
			return nil, fmt.Errorf("target %q: unknown type %q (icmp|tcp)", t.Name, t.Type)
		}
		switch t.Pace {
		case "", "fast", "normal", "slow":
		default:
			return nil, fmt.Errorf("target %q: invalid pace %q (fast|slow)", t.Name, t.Pace)
		}
		t.dir = dirSan.ReplaceAllString(t.Name, "_")
		if seen[t.dir] {
			return nil, fmt.Errorf("target name %q collides with another target", t.Name)
		}
		seen[t.dir] = true
	}
	if cfg.Probe.Packets < 3 {
		cfg.Probe.Packets = 3 // fewer than 3 samples is not a distribution
	}
	return cfg, nil
}

// loadTargetLists reads targets/ping.list and tcp.list.
// line format: host[:port]  [name...]  [pace=fast|slow] [interval=sec]
func loadTargetLists(dir string) ([]TargetCfg, error) {
	var out []TargetCfg
	for _, spec := range []struct{ file, typ string }{{"ping.list", "icmp"}, {"tcp.list", "tcp"}} {
		raw, err := os.ReadFile(filepath.Join(dir, spec.file))
		if err != nil {
			continue
		}
		for ln, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			t, err := parseListLine(line, spec.typ)
			if err != nil {
				return nil, fmt.Errorf("%s/%s line %d: %w", dir, spec.file, ln+1, err)
			}
			out = append(out, t)
		}
	}
	return out, nil
}

func parseListLine(line, typ string) (TargetCfg, error) {
	t := TargetCfg{Type: typ}
	fields := strings.Fields(line)
	addr := fields[0]
	if typ == "tcp" {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return t, fmt.Errorf("tcp target %q must be host:port", addr)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			return t, fmt.Errorf("tcp target %q: bad port", addr)
		}
		t.Host, t.Port = host, port
	} else {
		t.Host = addr
	}
	var nameParts []string
	for _, f := range fields[1:] {
		k, v, isKV := strings.Cut(f, "=")
		if !isKV {
			nameParts = append(nameParts, f)
			continue
		}
		switch k {
		case "pace":
			t.Pace = v
		case "interval":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return t, fmt.Errorf("interval=%q invalid", v)
			}
			t.IntervalSec = n
		} // unknown k=v silently ignored: forward compatibility
	}
	if len(nameParts) > 0 {
		t.Name = strings.Join(nameParts, " ")
	} else {
		t.Name = addr
	}
	return t, nil
}

func stripJSONC(b []byte) []byte {
	var out strings.Builder
	out.Grow(len(b))
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++
		default:
			out.WriteByte(c)
		}
	}
	return []byte(out.String())
}
