package main

// snapshot.go — 狀態快照環。每 10s 記錄一次「當前系統的配置與進程形態」，
// 供 /diff 回答「故障點前後什麼變了」。對應 Google SRE 的變更監控原則:
// 變更要持續記錄，而不是故障後才去看（那時看到的已經是改過之後的了）。
// 全部只讀，零 exec。

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProcInfo struct {
	PID     int    `json:"pid"`
	Comm    string `json:"comm"`
	State   string `json:"state"` // R/S/D/Z/T...
	Threads int    `json:"threads"`
	RSSMB   int    `json:"rss_mb"`
}

type StateSnapshot struct {
	TS           int64             `json:"ts"`
	Procs        []ProcInfo        `json:"procs"`         // top-N by RSS
	NProcs       int               `json:"nprocs"`        // 進程總數
	NThreads     int               `json:"nthreads"`      // 線程總數
	StateDist    map[string]int    `json:"state_dist"`    // 進程狀態分佈 R/S/D/Z
	Sysctl       map[string]string `json:"sysctl"`        // 監視清單
	ResolvConf   string            `json:"resolv_conf"`
	Routes       []string          `json:"routes"`        // /proc/net/route + ipv6 行
	ListenPorts  []string          `json:"listen_ports"`  // "tcp:80" ...
	RulesetHash  string            `json:"fw_files_hash"` // /etc 防火牆相關文件內容 hash
}

// 默認 sysctl 監視清單（可用 -sysctl-watch 文件覆蓋）
var defaultSysctlWatch = []string{
	"net/ipv4/tcp_congestion_control",
	"net/ipv4/tcp_max_syn_backlog",
	"net/ipv4/tcp_tw_reuse",
	"net/ipv4/tcp_fin_timeout",
	"net/ipv4/ip_local_port_range",
	"net/ipv4/tcp_rmem",
	"net/ipv4/tcp_wmem",
	"net/core/somaxconn",
	"net/core/rmem_max",
	"net/core/wmem_max",
	"net/netfilter/nf_conntrack_max",
	"net/netfilter/nf_conntrack_tcp_timeout_established",
	"vm/swappiness",
	"vm/overcommit_memory",
	"fs/file-max",
}

type StateRing struct {
	mu   sync.RWMutex
	buf  []StateSnapshot
	next int
	full bool
}

func NewStateRing(n int) *StateRing { return &StateRing{buf: make([]StateSnapshot, n)} }

func (r *StateRing) Add(s StateSnapshot) {
	r.mu.Lock()
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// 找 ts 之前(dir=-1)或之後(dir=+1)最近的快照
func (r *StateRing) Nearest(ts int64, dir int) *StateSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best *StateSnapshot
	for i := range r.buf {
		s := &r.buf[i]
		if s.TS == 0 {
			continue
		}
		if dir < 0 && s.TS <= ts && (best == nil || s.TS > best.TS) {
			best = s
		}
		if dir > 0 && s.TS >= ts && (best == nil || s.TS < best.TS) {
			best = s
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

func TakeStateSnapshot(sysctlWatch []string, topN int) StateSnapshot {
	s := StateSnapshot{TS: time.Now().Unix(), Sysctl: map[string]string{}, StateDist: map[string]int{}}

	// 進程掃描: /proc/[pid]/stat + status
	entries, _ := os.ReadDir("/proc")
	var procs []ProcInfo
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// comm 可能含空格，定位右括號
		str := string(stat)
		rp := strings.LastIndexByte(str, ')')
		if rp < 0 {
			continue
		}
		comm := str[strings.IndexByte(str, '(')+1 : rp]
		fs := strings.Fields(str[rp+2:])
		if len(fs) < 22 {
			continue
		}
		state := fs[0]
		threads, _ := strconv.Atoi(fs[17])
		rssPages, _ := strconv.ParseInt(fs[21], 10, 64)
		s.NProcs++
		s.NThreads += threads
		s.StateDist[state]++
		// 內核線程（cmdline 為空）計入總數與狀態分佈，但不進 top-N 清單，
		// 避免 kworker 等在 diff 中造成「進程出現/消失」噪音
		if cmd, err := os.ReadFile("/proc/" + e.Name() + "/cmdline"); err != nil || len(cmd) == 0 {
			continue
		}
		procs = append(procs, ProcInfo{PID: pid, Comm: comm, State: state,
			Threads: threads, RSSMB: int(rssPages * int64(os.Getpagesize()) / 1024 / 1024)})
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSMB > procs[j].RSSMB })
	if len(procs) > topN {
		procs = procs[:topN]
	}
	s.Procs = procs

	// sysctl 監視清單
	for _, k := range sysctlWatch {
		if b, err := os.ReadFile("/proc/sys/" + k); err == nil {
			s.Sysctl[strings.ReplaceAll(k, "/", ".")] = strings.TrimSpace(string(b))
		}
	}

	// resolv.conf（去註釋）
	if b, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		var keep []string
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if ln != "" && !strings.HasPrefix(ln, "#") {
				keep = append(keep, ln)
			}
		}
		s.ResolvConf = strings.Join(keep, "\n")
	}

	// 路由表（v4 跳過表頭 + v6）
	if lines := readLines("/proc/net/route"); len(lines) > 1 {
		s.Routes = append(s.Routes, lines[1:]...)
	}
	if lines := readLines("/proc/net/ipv6_route"); len(lines) > 0 {
		n := len(lines)
		if n > 50 {
			n = 50
		}
		s.Routes = append(s.Routes, lines[:n]...)
	}

	// 監聽端口: /proc/net/tcp{,6} state 0A
	seen := map[string]bool{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for i, ln := range readLines(f) {
			if i == 0 {
				continue
			}
			fs := strings.Fields(ln)
			if len(fs) < 4 || fs[3] != "0A" {
				continue
			}
			hp := strings.Split(fs[1], ":")
			if len(hp) != 2 {
				continue
			}
			port, err := strconv.ParseInt(hp[1], 16, 32)
			if err != nil {
				continue
			}
			seen["tcp:"+strconv.FormatInt(port, 10)] = true
		}
	}
	for k := range seen {
		s.ListenPorts = append(s.ListenPorts, k)
	}
	sort.Strings(s.ListenPorts)

	// 防火牆配置文件內容 hash（運行時 nft 規則需 exec，此處只跟蹤配置文件變更）
	h := sha256.New()
	for _, p := range []string{"/etc/nftables.conf", "/etc/sysconfig/nftables.conf"} {
		if b, err := os.ReadFile(p); err == nil {
			h.Write(b)
		}
	}
	for _, p := range []string{"/etc/nftables.d", "/etc/iptables"} {
		filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, e := os.ReadFile(path); e == nil {
					h.Write(b)
				}
			}
			return nil
		})
	}
	s.RulesetHash = hex.EncodeToString(h.Sum(nil))[:16]
	return s
}
