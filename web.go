package main

import (
	"embed"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

//go:embed static
var staticFS embed.FS

// serveWeb: a read-only local oscilloscope. No sessions, no login, no push —
// probe results in, smoke graphs out. Bind with --localhost and put a reverse
// proxy in front if you need auth or TLS.
func serveWeb(cfg *Config, store *Store) error {
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

	// Target list with rolling stats. "red" means the latest round lost every packet.
	mux.HandleFunc("/api/targets", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		type item struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Host        string `json:"host"`
			IntervalSec int    `json:"interval_sec"`
			Pace        string `json:"pace"`
			Down        bool   `json:"down"`
			Last1h      Stats  `json:"last_1h"`
			Last24h     Stats  `json:"last_24h"`
		}
		byName := map[string]TargetCfg{}
		for _, t := range cfg.Targets {
			byName[t.Name] = t
		}
		var out []item
		for _, name := range store.Names() {
			t := byName[name]
			iv, _ := probeParams(t, cfg.Probe)
			pace := t.Pace
			if pace == "" {
				pace = "normal"
			}
			rec := store.Recent(name, now.Add(-time.Hour).Unix())
			down := false
			if len(rec) > 0 {
				last := rec[len(rec)-1]
				down = last.R == 0
			}
			out = append(out, item{
				Name: name, Type: t.Type, Host: targetAddr(t),
				IntervalSec: int(iv.Seconds()), Pace: pace, Down: down,
				Last1h:  calcStats(rec),
				Last24h: calcStats(store.Recent(name, now.Add(-24*time.Hour).Unix())),
			})
		}
		writeJSON(w, out)
	})

	// Raw rounds for the smoke graph.
	mux.HandleFunc("/api/series", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("target")
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		if minutes <= 0 || minutes > 1440 {
			minutes = 360
		}
		rounds := store.Recent(name, time.Now().Add(-time.Duration(minutes)*time.Minute).Unix())
		writeJSON(w, rounds)
	})

	return http.ListenAndServe(cfg.Listen, mux)
}

func targetAddr(t TargetCfg) string {
	if t.Type == "tcp" {
		return t.Host + ":" + strconv.Itoa(t.Port)
	}
	return t.Host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
