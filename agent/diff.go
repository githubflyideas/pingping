package main

// diff.go — 故障點前後對比。指標部分照抄 PCP pmdiff 的語義:
//   窗口均值比、默認閾值 2（翻倍/腰斬才報）、基線0而對比非0記 "|+|"、反之 "|-|"、
//   按比率排序、報告只在單側出現的指標。
//   （本 agent 的採集器在採集時已完成計數器速率化，故直接比均值。）
// 狀態部分對比最近的兩份快照: 進程出現/消失、線程與狀態分佈變化、
//   sysctl/resolv.conf/路由/監聽端口/防火牆文件變更。

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
)

type MetricDiff struct {
	Metric string  `json:"metric"`
	Before float64 `json:"before_avg"`
	After  float64 `json:"after_avg"`
	Ratio  string  `json:"ratio"` // 數值、"|+|" 或 "|-|"
	rank   float64
}

func handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var t int64
	fmt.Sscanf(q.Get("t"), "%d", &t)
	before, after, thresh := 120, 120, 2.0
	fmt.Sscanf(q.Get("before"), "%d", &before)
	fmt.Sscanf(q.Get("after"), "%d", &after)
	fmt.Sscanf(q.Get("q"), "%f", &thresh)
	if thresh < 1.1 {
		thresh = 2.0
	}

	resp := map[string]any{
		"host": hostname, "t": t,
		"before_window_s": before, "after_window_s": after, "threshold": thresh,
	}
	resp["metric_diff"] = diffMetrics(t, before, after, thresh)
	resp["state_diff"] = diffState(t)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func windowAvg(from, to int64) map[string]float64 {
	sum, cnt := map[string]float64{}, map[string]int{}
	for _, s := range ring.Last(len(ring.buf)) {
		if s.TS < from || s.TS >= to {
			continue
		}
		for k, v := range s.Values {
			sum[k] += v
			cnt[k]++
		}
	}
	out := map[string]float64{}
	for k := range sum {
		out[k] = sum[k] / float64(cnt[k])
	}
	return out
}

func diffMetrics(t int64, before, after int, thresh float64) map[string]any {
	b := windowAvg(t-int64(before), t)
	a := windowAvg(t, t+int64(after))

	var diffs []MetricDiff
	var appeared, disappeared []string
	const eps = 1e-9

	for k, bv := range b {
		av, ok := a[k]
		if !ok {
			disappeared = append(disappeared, k)
			continue
		}
		switch {
		case math.Abs(bv) < eps && math.Abs(av) < eps:
			// 兩側皆零，無事發生
		case math.Abs(bv) < eps:
			diffs = append(diffs, MetricDiff{k, round4(bv), round4(av), "|+|", math.Inf(1)})
		case math.Abs(av) < eps:
			diffs = append(diffs, MetricDiff{k, round4(bv), round4(av), "|-|", math.Inf(1)})
		default:
			ratio := av / bv
			if ratio >= thresh || ratio <= 1/thresh {
				diffs = append(diffs, MetricDiff{k, round4(bv), round4(av),
					fmt.Sprintf("%.2f", ratio), math.Abs(math.Log(ratio))})
			}
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			appeared = append(appeared, k)
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].rank > diffs[j].rank })
	sort.Strings(appeared)
	sort.Strings(disappeared)
	return map[string]any{
		"changed": diffs, "appeared": appeared, "disappeared": disappeared,
	}
}

func diffState(t int64) map[string]any {
	b := stateRing.Nearest(t, -1)
	a := stateRing.Nearest(t, +1)
	if b == nil || a == nil || b.TS == a.TS {
		return map[string]any{"note": "狀態快照不足（需要 t 前後各至少一份，快照間隔10s）"}
	}
	out := map[string]any{
		"before_ts": b.TS, "after_ts": a.TS,
		"nprocs":  map[string]int{"before": b.NProcs, "after": a.NProcs},
		"nthreads": map[string]int{"before": b.NThreads, "after": a.NThreads},
		"proc_state_dist": map[string]any{"before": b.StateDist, "after": a.StateDist},
	}

	// 進程出現/消失（按 pid+comm）
	key := func(p ProcInfo) string { return fmt.Sprintf("%d/%s", p.PID, p.Comm) }
	bp, ap := map[string]ProcInfo{}, map[string]ProcInfo{}
	for _, p := range b.Procs {
		bp[key(p)] = p
	}
	for _, p := range a.Procs {
		ap[key(p)] = p
	}
	var appeared, disappeared []ProcInfo
	var threadDelta []map[string]any
	for k, p := range ap {
		if old, ok := bp[k]; !ok {
			appeared = append(appeared, p)
		} else if p.Threads != old.Threads || p.State != old.State {
			threadDelta = append(threadDelta, map[string]any{
				"proc": k, "threads_before": old.Threads, "threads_after": p.Threads,
				"state_before": old.State, "state_after": p.State})
		}
	}
	for k, p := range bp {
		if _, ok := ap[k]; !ok {
			disappeared = append(disappeared, p)
		}
	}
	out["procs_appeared"] = appeared
	out["procs_disappeared"] = disappeared
	out["proc_changes"] = threadDelta

	// sysctl
	var sysctlChanged []map[string]string
	for k, bv := range b.Sysctl {
		if av, ok := a.Sysctl[k]; ok && av != bv {
			sysctlChanged = append(sysctlChanged, map[string]string{"key": k, "before": bv, "after": av})
		}
	}
	out["sysctl_changed"] = sysctlChanged

	// resolv.conf / 防火牆文件 / 路由 / 監聽端口
	if b.ResolvConf != a.ResolvConf {
		out["resolv_conf"] = map[string]string{"before": b.ResolvConf, "after": a.ResolvConf}
	}
	if b.RulesetHash != a.RulesetHash {
		out["fw_files_changed"] = true
	}
	out["routes_added"], out["routes_removed"] = setDiff(b.Routes, a.Routes)
	out["listen_ports_added"], out["listen_ports_removed"] = setDiff(b.ListenPorts, a.ListenPorts)
	return out
}

func setDiff(before, after []string) (added, removed []string) {
	bs := map[string]bool{}
	as := map[string]bool{}
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for _, x := range before {
		bs[norm(x)] = true
	}
	for _, x := range after {
		as[norm(x)] = true
	}
	for x := range as {
		if !bs[x] {
			added = append(added, x)
		}
	}
	for x := range bs {
		if !as[x] {
			removed = append(removed, x)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }
