package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

//go:embed static
var staticFS embed.FS

func serveWeb(cfg *Config, store *Store, det *Detector, n *Notifier) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := staticFS.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))

	// 目标列表 + 当前状态 + 1h/24h 统计
	mux.HandleFunc("/api/targets", func(w http.ResponseWriter, r *http.Request) {
		statuses := det.Snapshot()
		now := time.Now()
		type item struct {
			Name    string `json:"name"`
			Status  Status `json:"status"`
			Last1h  Stats  `json:"last_1h"`
			Last24h Stats  `json:"last_24h"`
		}
		var out []item
		for _, name := range store.Names() {
			out = append(out, item{
				Name:    name,
				Status:  statuses[name],
				Last1h:  calcStats(store.Recent(name, now.Add(-time.Hour).Unix())),
				Last24h: calcStats(store.Recent(name, now.Add(-24*time.Hour).Unix())),
			})
		}
		writeJSON(w, out)
	})

	// 原始轮序列(烟雾图数据源)
	mux.HandleFunc("/api/series", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("target")
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		if minutes <= 0 || minutes > 1440 {
			minutes = 360
		}
		rounds := store.Recent(name, time.Now().Add(-time.Duration(minutes)*time.Minute).Unix())
		writeJSON(w, rounds)
	})

	// 日汇总(长期趋势)
	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, store.SummaryLines(r.URL.Query().Get("target"), 30))
	})

	// 手动拉取:立即向所有 webhook 推送实时报告(消息模型第四件)
	mux.HandleFunc("/api/push", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		n.SendReport("📊 链路实时报告(手动触发)", "manual")
		writeJSON(w, map[string]string{"ok": "报告已推送至全部 webhook"})
	})

	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
