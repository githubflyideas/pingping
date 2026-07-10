package main

// boss-board — 給管理層看的一頁式健康面板。單二進制，零外部依賴，零CDN。
//   數據源: VictoriaMetrics(趨勢與現狀) + brain 的診斷歸檔目錄(最近結論)
//   用法: ./boss-board -vm http://127.0.0.1:8428 -hosts gw01,pve01,pve02 \
//                      -reports ../reports -listen :8080

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	vmURL      string
	hosts      []string
	reportsDir string
)

func main() {
	vm := flag.String("vm", "http://127.0.0.1:8428", "VictoriaMetrics 地址")
	hostList := flag.String("hosts", "", "主機清單，逗號分隔")
	reports := flag.String("reports", "./reports", "brain 診斷歸檔目錄")
	listen := flag.String("listen", ":8080", "監聽地址")
	flag.Parse()
	vmURL = *vm
	reportsDir = *reports
	for _, h := range strings.Split(*hostList, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/summary", handleSummary)
	log.Printf("boss-board up %s hosts=%v", *listen, hosts)
	log.Fatal(http.ListenAndServe(*listen, nil))
}

// ---- 數據 ----

type HostView struct {
	Host    string               `json:"host"`
	Status  string               `json:"status"` // ok | warn | crit | nodata
	Reasons []string             `json:"reasons"`
	Last    map[string]float64   `json:"last"`
	Series  map[string][]float64 `json:"series"` // 近30分鐘趨勢，供前端畫 sparkline
}

// 展示與判級用的指標
var boardMetrics = []string{
	"cpu_util_pct", "mem_util_pct", "psi_io_some_avg10",
	"tcp_retrans_pct", "procs_blocked",
}

func handleSummary(w http.ResponseWriter, _ *http.Request) {
	type resp struct {
		GeneratedAt string       `json:"generated_at"`
		Hosts       []HostView   `json:"hosts"`
		Reports     []map[string]string `json:"reports"`
	}
	out := resp{GeneratedAt: time.Now().Format("2006-01-02 15:04:05")}
	for _, h := range hosts {
		out.Hosts = append(out.Hosts, buildHostView(h))
	}
	out.Reports = recentReports(8)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func buildHostView(host string) HostView {
	v := HostView{Host: host, Status: "nodata",
		Last: map[string]float64{}, Series: map[string][]float64{}}
	now := time.Now().Unix()
	got := false
	for _, m := range boardMetrics {
		pts := vmRange(fmt.Sprintf(`%s{host="%s"}`, m, host), now-1800, now, 60)
		if len(pts) == 0 {
			continue
		}
		got = true
		v.Series[m] = pts
		v.Last[m] = pts[len(pts)-1]
	}
	if !got {
		v.Reasons = []string{"無數據（agent 離線?）"}
		return v
	}
	v.Status = "ok"
	warn := func(cond bool, level, reason string) {
		if !cond {
			return
		}
		if level == "crit" || v.Status != "crit" {
			if level == "crit" {
				v.Status = "crit"
			} else if v.Status == "ok" {
				v.Status = "warn"
			}
		}
		v.Reasons = append(v.Reasons, reason)
	}
	l := v.Last
	warn(l["cpu_util_pct"] > 90, "crit", fmt.Sprintf("CPU %.0f%%", l["cpu_util_pct"]))
	warn(l["cpu_util_pct"] > 70 && l["cpu_util_pct"] <= 90, "warn", fmt.Sprintf("CPU %.0f%%", l["cpu_util_pct"]))
	warn(l["mem_util_pct"] > 92, "crit", fmt.Sprintf("內存 %.0f%%", l["mem_util_pct"]))
	warn(l["mem_util_pct"] > 82 && l["mem_util_pct"] <= 92, "warn", fmt.Sprintf("內存 %.0f%%", l["mem_util_pct"]))
	warn(l["tcp_retrans_pct"] > 3, "crit", fmt.Sprintf("TCP重傳 %.1f%%", l["tcp_retrans_pct"]))
	warn(l["tcp_retrans_pct"] > 1 && l["tcp_retrans_pct"] <= 3, "warn", fmt.Sprintf("TCP重傳 %.1f%%", l["tcp_retrans_pct"]))
	warn(l["psi_io_some_avg10"] > 20, "crit", fmt.Sprintf("IO壓力 %.0f", l["psi_io_some_avg10"]))
	warn(l["psi_io_some_avg10"] > 5 && l["psi_io_some_avg10"] <= 20, "warn", fmt.Sprintf("IO壓力 %.0f", l["psi_io_some_avg10"]))
	warn(l["procs_blocked"] >= 5, "warn", fmt.Sprintf("D狀態任務 %.0f", l["procs_blocked"]))
	return v
}

func vmRange(promql string, start, end int64, step int) []float64 {
	q := url.Values{"query": {promql},
		"start": {fmt.Sprint(start)}, "end": {fmt.Sprint(end)}, "step": {fmt.Sprint(step)}}
	c := http.Client{Timeout: 5 * time.Second}
	res, err := c.Get(vmURL + "/api/v1/query_range?" + q.Encode())
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var parsed struct {
		Data struct {
			Result []struct {
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Data.Result) == 0 {
		return nil
	}
	var out []float64
	for _, kv := range parsed.Data.Result[0].Values {
		var f float64
		fmt.Sscanf(fmt.Sprint(kv[1]), "%f", &f)
		out = append(out, f)
	}
	return out
}

func recentReports(n int) []map[string]string {
	var out []map[string]string
	entries, _ := os.ReadDir(reportsDir)
	type rf struct {
		name string
		mod  time.Time
	}
	var files []rf
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			if info, err := e.Info(); err == nil {
				files = append(files, rf{e.Name(), info.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if len(files) > n {
		files = files[:n]
	}
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(reportsDir, f.name))
		content := string(b)
		if len(content) > 2500 {
			content = content[:2500] + "\n…(截斷)"
		}
		out = append(out, map[string]string{
			"time": f.mod.Format("01-02 15:04"), "name": f.name, "content": content})
	}
	return out
}

// ---- 頁面（內嵌，零CDN）----

func handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, indexHTML)
}

const indexHTML = `<!DOCTYPE html><html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>私有雲健康總覽</title><style>
:root{--bg:#0f1419;--card:#1a2129;--tx:#e6edf3;--dim:#8b98a5;--ok:#3fb950;--warn:#d29922;--crit:#f85149}
*{box-sizing:border-box;margin:0}body{background:var(--bg);color:var(--tx);
font:15px/1.6 -apple-system,"PingFang TC","Microsoft YaHei",sans-serif;padding:28px}
h1{font-size:22px;font-weight:600}#ts{color:var(--dim);font-size:13px;margin:4px 0 22px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:16px}
.card{background:var(--card);border-radius:12px;padding:18px;border:1px solid #2a3441}
.hd{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
.host{font-size:17px;font-weight:600}
.badge{padding:3px 12px;border-radius:20px;font-size:13px;font-weight:600}
.ok{background:#1c3325;color:var(--ok)}.warn{background:#332b13;color:var(--warn)}
.crit{background:#3d1a1c;color:var(--crit)}.nodata{background:#2a3441;color:var(--dim)}
.reasons{color:var(--dim);font-size:13px;min-height:20px;margin-bottom:8px}
.crit .reasons{color:var(--crit)}
.mrow{display:flex;justify-content:space-between;align-items:center;padding:3px 0;font-size:13px}
.mname{color:var(--dim)}.mval{font-variant-numeric:tabular-nums;min-width:52px;text-align:right}
svg{display:block}
h2{font-size:17px;margin:34px 0 14px}
.rep{background:var(--card);border:1px solid #2a3441;border-radius:12px;padding:16px;margin-bottom:12px}
.rep .t{color:var(--dim);font-size:12px;margin-bottom:6px}
.rep pre{white-space:pre-wrap;font:13px/1.6 inherit;color:var(--tx)}
.empty{color:var(--dim)}
</style></head><body>
<h1>私有雲健康總覽</h1><div id="ts">載入中…</div>
<div class="grid" id="grid"></div>
<h2>最近 AI 診斷</h2><div id="reports"></div>
<script>
const NAMES={cpu_util_pct:"CPU 使用率",mem_util_pct:"內存使用率",
psi_io_some_avg10:"IO 壓力(PSI)",tcp_retrans_pct:"TCP 重傳率",procs_blocked:"D狀態任務"};
const BADGE={ok:"正常",warn:"關注",crit:"異常",nodata:"無數據"};
function spark(pts,color){if(!pts||pts.length<2)return"";
const w=110,h=26,mx=Math.max(...pts,1e-9),mn=Math.min(...pts);
const p=pts.map((v,i)=>((i/(pts.length-1))*w).toFixed(1)+","+ (h-2-((v-mn)/(mx-mn||1))*(h-4)).toFixed(1)).join(" ");
return '<svg width="'+w+'" height="'+h+'"><polyline points="'+p+'" fill="none" stroke="'+color+'" stroke-width="1.6"/></svg>'}
async function load(){
const d=await (await fetch("api/summary")).json();
document.getElementById("ts").textContent="更新於 "+d.generated_at+"（每30秒自動刷新）";
document.getElementById("grid").innerHTML=(d.hosts||[]).map(h=>{
const col=h.status==="crit"?"#f85149":h.status==="warn"?"#d29922":"#3fb950";
const rows=Object.keys(NAMES).map(m=>{
 if(!(h.series&&h.series[m]))return"";
 const v=h.last[m],txt=m.includes("pct")?v.toFixed(1)+"%":v.toFixed(1);
 return '<div class="mrow"><span class="mname">'+NAMES[m]+'</span>'+spark(h.series[m],col)+
        '<span class="mval">'+txt+'</span></div>'}).join("");
return '<div class="card '+h.status+'"><div class="hd"><span class="host">'+h.host+
 '</span><span class="badge '+h.status+'">'+BADGE[h.status]+'</span></div>'+
 '<div class="reasons">'+((h.reasons||[]).join("、")||"各項指標正常")+'</div>'+rows+'</div>'}).join("");
document.getElementById("reports").innerHTML=(d.reports&&d.reports.length)?
 d.reports.map(r=>'<div class="rep"><div class="t">'+r.time+" · "+r.name+
 '</div><pre>'+r.content.replace(/</g,"&lt;")+'</pre></div>').join("")
 :'<div class="empty">暫無診斷記錄</div>'}
load();setInterval(load,30000);
</script></body></html>`
