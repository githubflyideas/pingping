package main

// sre-agent — 單一靜態二進制。
//   1s 採樣 → 內存環形緩衝（飛行記錄器，默認保留 3600s 原始粒度）
//   10s 聚合 → push VictoriaMetrics /api/v1/import/prometheus（純文本協議，零依賴）
//   只讀 HTTP:
//     GET /triage            Brendan Gregg 60 秒排查快照（USE 摘要，供 AI 第一步調用）
//     GET /window?sec=120    拉取最近 N 秒的 1s 原始數據（診斷時按需取證）
//     GET /healthz

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Ring struct {
	mu   sync.RWMutex
	buf  []Sample
	next int
	full bool
}

func NewRing(n int) *Ring { return &Ring{buf: make([]Sample, n)} }

func (r *Ring) Add(s Sample) {
	r.mu.Lock()
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

func (r *Ring) Last(sec int) []Sample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := len(r.buf)
	cnt := n
	if !r.full {
		cnt = r.next
	}
	if sec < cnt {
		cnt = sec
	}
	out := make([]Sample, 0, cnt)
	for i := 0; i < cnt; i++ {
		idx := (r.next - cnt + i + n) % n
		if r.buf[idx].TS > 0 {
			out = append(out, r.buf[idx])
		}
	}
	return out
}

var (
	hostname, _ = os.Hostname()
	ring        *Ring
	stateRing   *StateRing
)

func main() {
	vmURL := flag.String("vm", "", "VictoriaMetrics 地址，如 http://10.0.0.5:8428（留空則只本地記錄不上報）")
	listen := flag.String("listen", ":9911", "只讀 API 監聽地址")
	retain := flag.Int("retain", 3600, "飛行記錄器保留秒數(1s 粒度)")
	pushEvery := flag.Int("push", 10, "聚合上報間隔秒")
	dnsTargets := flag.String("dns", "", "DNS 探測域名，逗號分隔（如 www.baidu.com,api.internal）")
	snapEvery := flag.Int("snapshot", 10, "狀態快照間隔秒")
	snapTopN := flag.Int("snapshot-topn", 30, "快照保留的 top-N 進程數(按RSS)")
	flag.Parse()

	ring = NewRing(*retain)
	stateRing = NewStateRing(*retain / *snapEvery) // 同樣保留 1 小時
	col := NewCollector()
	prober := NewDNSProber(*dnsTargets, time.Duration(*snapEvery)*time.Second)

	// 1s 採樣循環
	go func() {
		tick := time.NewTicker(time.Second)
		for range tick.C {
			s := col.Collect()
			prober.Merge(s.Values)
			ring.Add(s)
		}
	}()

	// 狀態快照循環
	go func() {
		stateRing.Add(TakeStateSnapshot(defaultSysctlWatch, *snapTopN))
		tick := time.NewTicker(time.Duration(*snapEvery) * time.Second)
		for range tick.C {
			stateRing.Add(TakeStateSnapshot(defaultSysctlWatch, *snapTopN))
		}
	}()

	// 10s 聚合上報循環（失敗進內存重試隊列，帶上限防打爆）
	if *vmURL != "" {
		go pushLoop(*vmURL, *pushEvery)
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	http.HandleFunc("/window", handleWindow)
	http.HandleFunc("/triage", handleTriage)
	http.HandleFunc("/diff", handleDiff)
	log.Printf("sre-agent up host=%s listen=%s vm=%s retain=%ds", hostname, *listen, *vmURL, *retain)
	log.Fatal(http.ListenAndServe(*listen, nil))
}

// ---- 上報 ----

var backlog [][]byte // 斷網重試隊列

func pushLoop(vmURL string, every int) {
	tick := time.NewTicker(time.Duration(every) * time.Second)
	client := &http.Client{Timeout: 5 * time.Second}
	for range tick.C {
		samples := ring.Last(every)
		if len(samples) == 0 {
			continue
		}
		body := aggregate(samples)
		backlog = append(backlog, body)
		if len(backlog) > 360 { // 最多囤 1 小時
			backlog = backlog[1:]
		}
		var kept [][]byte
		for _, b := range backlog {
			resp, err := client.Post(vmURL+"/api/v1/import/prometheus", "text/plain", bytes.NewReader(b))
			if err != nil {
				kept = append(kept, b)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				kept = append(kept, b)
			}
		}
		backlog = kept
	}
}

// 聚合為 Prometheus 文本行: name{host=..,dev=..} avg ts_ms（附帶 _max 系列保尖峰）
func aggregate(samples []Sample) []byte {
	sum := map[string]float64{}
	max := map[string]float64{}
	cnt := map[string]int{}
	for _, s := range samples {
		for k, v := range s.Values {
			sum[k] += v
			cnt[k]++
			if v > max[k] || cnt[k] == 1 {
				max[k] = v
			}
		}
	}
	ts := samples[len(samples)-1].TS * 1000
	var b strings.Builder
	keys := make([]string, 0, len(sum))
	for k := range sum {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name, labels := splitMetric(k)
		avg := sum[k] / float64(cnt[k])
		fmt.Fprintf(&b, "%s{host=%q%s} %.4f %d\n", name, hostname, labels, avg, ts)
		fmt.Fprintf(&b, "%s_max{host=%q%s} %.4f %d\n", name, hostname, labels, max[k], ts)
	}
	return []byte(b.String())
}

// "disk_util_pct{dev=sda}" -> ("disk_util_pct", `,dev="sda"`)
func splitMetric(k string) (string, string) {
	i := strings.IndexByte(k, '{')
	if i < 0 {
		return k, ""
	}
	name := k[:i]
	inner := strings.TrimSuffix(k[i+1:], "}")
	var parts []string
	for _, kv := range strings.Split(inner, ",") {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			parts = append(parts, fmt.Sprintf(",%s=%q", kv[:eq], kv[eq+1:]))
		}
	}
	return name, strings.Join(parts, "")
}

// ---- 只讀 API ----

func handleWindow(w http.ResponseWriter, r *http.Request) {
	sec := 120
	fmt.Sscanf(r.URL.Query().Get("sec"), "%d", &sec)
	if sec <= 0 || sec > len(ring.buf) {
		sec = 120
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"host": hostname, "granularity_sec": 1, "samples": ring.Last(sec),
	})
}

// Gregg 60 秒排查: 最近 60s 每個關鍵指標的 avg/max/last，按 USE 分組
func handleTriage(w http.ResponseWriter, r *http.Request) {
	samples := ring.Last(60)
	agg := map[string][3]float64{} // avg, max, last
	cnt := map[string]int{}
	for _, s := range samples {
		for k, v := range s.Values {
			a := agg[k]
			a[0] += v
			if v > a[1] || cnt[k] == 0 {
				a[1] = v
			}
			a[2] = v
			agg[k] = a
			cnt[k]++
		}
	}
	type stat struct{ Avg, Max, Last float64 }
	group := map[string]map[string]stat{
		"utilization": {}, "saturation": {}, "errors": {}, "other": {},
	}
	classify := func(k string) string {
		switch {
		case strings.Contains(k, "util") || strings.Contains(k, "mbps") || strings.Contains(k, "iops"):
			return "utilization"
		case strings.HasPrefix(k, "psi_") || strings.Contains(k, "inflight") ||
			strings.Contains(k, "conntrack") || strings.HasPrefix(k, "load") ||
			strings.Contains(k, "iowait"):
			return "saturation"
		case strings.Contains(k, "err") || strings.Contains(k, "drop") ||
			strings.Contains(k, "retrans") || strings.Contains(k, "timeout"):
			return "errors"
		}
		return "other"
	}
	for k, a := range agg {
		n := float64(cnt[k])
		group[classify(k)][k] = stat{round(a[0] / n), round(a[1]), round(a[2])}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"host": hostname, "window_sec": len(samples), "use": group,
	})
}

func round(f float64) float64 { return math.Round(f*100) / 100 }
