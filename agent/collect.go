package main

// collect.go — 只讀採集，全部走 /proc 與 /sys，禁止 exec。
// 指標按 USE 方法組織: 每類資源覆蓋 Utilization / Saturation / Errors。

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

type Sample struct {
	TS     int64              `json:"ts"`
	Values map[string]float64 `json:"v"`
}

type Collector struct {
	prevCPU  map[string]uint64
	prevDisk map[string][3]uint64 // ioTimeMs, reads, writes
	prevNet  map[string][4]uint64 // rxBytes, txBytes, rxErrDrop, txErrDrop
	prevTCP  map[string]uint64
	prevTS   time.Time
}

func NewCollector() *Collector {
	c := &Collector{}
	c.Collect() // 预热基线
	return c
}

func (c *Collector) Collect() Sample {
	now := time.Now()
	elapsed := now.Sub(c.prevTS).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	v := map[string]float64{}

	c.cpu(v, elapsed)
	c.psi(v)
	c.mem(v)
	c.disk(v, elapsed)
	c.net(v, elapsed)
	c.tcp(v, elapsed)
	c.conntrack(v)
	c.load(v)
	c.procStates(v)

	c.prevTS = now
	return Sample{TS: now.Unix(), Values: v}
}

func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func f64(s string) float64 { x, _ := strconv.ParseFloat(s, 64); return x }
func u64(s string) uint64  { x, _ := strconv.ParseUint(s, 10, 64); return x }

// ---- CPU: util(U) + iowait, PSI 提供 saturation ----
func (c *Collector) cpu(v map[string]float64, _ float64) {
	for _, ln := range readLines("/proc/stat") {
		if !strings.HasPrefix(ln, "cpu ") {
			continue
		}
		fs := strings.Fields(ln)
		if len(fs) < 8 {
			return
		}
		var vals [8]uint64
		var total uint64
		for i := 0; i < 8; i++ {
			vals[i] = u64(fs[i+1])
			total += vals[i]
		}
		if c.prevCPU == nil {
			c.prevCPU = map[string]uint64{}
		}
		dTotal := float64(total - c.prevCPU["total"])
		if dTotal > 0 && c.prevCPU["total"] > 0 {
			dIdle := float64(vals[3] - c.prevCPU["idle"])
			dIow := float64(vals[4] - c.prevCPU["iowait"])
			v["cpu_util_pct"] = (1 - (dIdle+dIow)/dTotal) * 100
			v["cpu_iowait_pct"] = dIow / dTotal * 100
		}
		c.prevCPU["total"], c.prevCPU["idle"], c.prevCPU["iowait"] = total, vals[3], vals[4]
		return
	}
}

// ---- PSI: cpu/mem/io 的 saturation（Gregg 意義上的 S）----
func (c *Collector) psi(v map[string]float64) {
	for _, res := range []string{"cpu", "memory", "io"} {
		for _, ln := range readLines("/proc/pressure/" + res) {
			fs := strings.Fields(ln)
			if len(fs) < 2 {
				continue
			}
			kind := fs[0] // some | full
			for _, f := range fs[1:] {
				if strings.HasPrefix(f, "avg10=") {
					v["psi_"+res+"_"+kind+"_avg10"] = f64(strings.TrimPrefix(f, "avg10="))
				}
			}
		}
	}
}

// ---- 內存: U ----
func (c *Collector) mem(v map[string]float64) {
	var total, avail float64
	for _, ln := range readLines("/proc/meminfo") {
		fs := strings.Fields(ln)
		if len(fs) < 2 {
			continue
		}
		switch fs[0] {
		case "MemTotal:":
			total = f64(fs[1])
		case "MemAvailable:":
			avail = f64(fs[1])
		}
	}
	if total > 0 {
		v["mem_util_pct"] = (1 - avail/total) * 100
		v["mem_avail_mb"] = avail / 1024
	}
}

// ---- 磁盤: util(U)、in-flight(S) ----
func (c *Collector) disk(v map[string]float64, elapsed float64) {
	if c.prevDisk == nil {
		c.prevDisk = map[string][3]uint64{}
	}
	for _, ln := range readLines("/proc/diskstats") {
		fs := strings.Fields(ln)
		if len(fs) < 14 {
			continue
		}
		dev := fs[2]
		if strings.HasPrefix(dev, "loop") || strings.HasPrefix(dev, "ram") ||
			strings.HasPrefix(dev, "dm-") || strings.HasPrefix(dev, "sr") {
			continue
		}
		// 只保留整盤（不含分區數字結尾的 sdX1/nvme0n1p1）
		if strings.ContainsAny(dev[len(dev)-1:], "0123456789") && !strings.HasPrefix(dev, "nvme") {
			continue
		}
		if strings.Contains(dev, "p") && strings.HasPrefix(dev, "nvme") {
			continue
		}
		reads, writes := u64(fs[3]), u64(fs[7])
		inflight := f64(fs[11])
		ioTime := u64(fs[12])
		prev := c.prevDisk[dev]
		if prev[0] > 0 {
			v["disk_util_pct{dev="+dev+"}"] = float64(ioTime-prev[0]) / (elapsed * 1000) * 100
			v["disk_iops{dev="+dev+"}"] = float64((reads-prev[1])+(writes-prev[2])) / elapsed
		}
		v["disk_inflight{dev="+dev+"}"] = inflight
		c.prevDisk[dev] = [3]uint64{ioTime, reads, writes}
	}
}

// ---- 網卡: 帶寬(U)、errs+drops(E) ----
func (c *Collector) net(v map[string]float64, elapsed float64) {
	if c.prevNet == nil {
		c.prevNet = map[string][4]uint64{}
	}
	lines := readLines("/proc/net/dev")
	for i, ln := range lines {
		if i < 2 {
			continue
		}
		parts := strings.SplitN(ln, ":", 2)
		if len(parts) != 2 {
			continue
		}
		dev := strings.TrimSpace(parts[0])
		if dev == "lo" {
			continue
		}
		fs := strings.Fields(parts[1])
		if len(fs) < 12 {
			continue
		}
		rxB, rxErrDrop := u64(fs[0]), u64(fs[2])+u64(fs[3])
		txB, txErrDrop := u64(fs[8]), u64(fs[10])+u64(fs[11])
		prev := c.prevNet[dev]
		if prev[0] > 0 || prev[1] > 0 {
			v["net_rx_mbps{dev="+dev+"}"] = float64(rxB-prev[0]) * 8 / elapsed / 1e6
			v["net_tx_mbps{dev="+dev+"}"] = float64(txB-prev[1]) * 8 / elapsed / 1e6
			v["net_rx_errdrop_ps{dev="+dev+"}"] = float64(rxErrDrop-prev[2]) / elapsed
			v["net_tx_errdrop_ps{dev="+dev+"}"] = float64(txErrDrop-prev[3]) / elapsed
		}
		c.prevNet[dev] = [4]uint64{rxB, txB, rxErrDrop, txErrDrop}
	}
}

// ---- TCP 質量: 重傳率(E)、accept 隊列溢出(S)、超時 ----
func (c *Collector) tcp(v map[string]float64, elapsed float64) {
	if c.prevTCP == nil {
		c.prevTCP = map[string]uint64{}
	}
	cur := map[string]uint64{}
	parseKV := func(path, section string, keys []string) {
		lines := readLines(path)
		for i := 0; i+1 < len(lines); i++ {
			if !strings.HasPrefix(lines[i], section) || !strings.HasPrefix(lines[i+1], section) {
				continue
			}
			hs, vs := strings.Fields(lines[i]), strings.Fields(lines[i+1])
			for j, h := range hs {
				for _, k := range keys {
					if h == k && j < len(vs) {
						cur[k] = u64(vs[j])
					}
				}
			}
		}
	}
	parseKV("/proc/net/snmp", "Tcp:", []string{"RetransSegs", "OutSegs", "CurrEstab"})
	parseKV("/proc/net/netstat", "TcpExt:",
		[]string{"TCPSynRetrans", "ListenOverflows", "ListenDrops", "TCPTimeouts"})

	v["tcp_curr_estab"] = float64(cur["CurrEstab"])
	if c.prevTCP["OutSegs"] > 0 {
		dOut := float64(cur["OutSegs"] - c.prevTCP["OutSegs"])
		dRet := float64(cur["RetransSegs"] - c.prevTCP["RetransSegs"])
		if dOut > 0 {
			v["tcp_retrans_pct"] = dRet / dOut * 100
		}
		v["tcp_syn_retrans_ps"] = float64(cur["TCPSynRetrans"]-c.prevTCP["TCPSynRetrans"]) / elapsed
		v["tcp_listen_drops_ps"] = float64(cur["ListenOverflows"]+cur["ListenDrops"]-
			c.prevTCP["ListenOverflows"]-c.prevTCP["ListenDrops"]) / elapsed
		v["tcp_timeouts_ps"] = float64(cur["TCPTimeouts"]-c.prevTCP["TCPTimeouts"]) / elapsed
	}
	for k, x := range cur {
		c.prevTCP[k] = x
	}
}

// ---- conntrack 水位(S) ----
func (c *Collector) conntrack(v map[string]float64) {
	cnt := readLines("/proc/sys/net/netfilter/nf_conntrack_count")
	max := readLines("/proc/sys/net/netfilter/nf_conntrack_max")
	if len(cnt) > 0 && len(max) > 0 {
		cf, mf := f64(cnt[0]), f64(max[0])
		v["conntrack_count"] = cf
		if mf > 0 {
			v["conntrack_util_pct"] = cf / mf * 100
		}
	}
}

// ---- TSA-lite: 全局可運行(R)與不可中斷(D)任務數。D 激增 = IO/鎖等待，
// 是「負載不高但很卡」的另一個關鍵信號 ----
func (c *Collector) procStates(v map[string]float64) {
	for _, ln := range readLines("/proc/stat") {
		fs := strings.Fields(ln)
		if len(fs) != 2 {
			continue
		}
		switch fs[0] {
		case "procs_running":
			v["procs_running"] = f64(fs[1])
		case "procs_blocked":
			v["procs_blocked"] = f64(fs[1])
		}
	}
}

func (c *Collector) load(v map[string]float64) {
	if ls := readLines("/proc/loadavg"); len(ls) > 0 {
		fs := strings.Fields(ls[0])
		if len(fs) > 0 {
			v["load1"] = f64(fs[0])
		}
	}
}
