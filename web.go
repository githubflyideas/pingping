package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

// sessions: in-memory, so a restart logs everyone out — acceptable and simple
// for a single-binary tool. Auth exists only when users are passed on the CLI.
type sessions struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (s *sessions) issue() string {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.m[tok] = time.Now().Add(7 * 24 * time.Hour)
	s.mu.Unlock()
	return tok
}

func (s *sessions) valid(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[tok]
	if !ok || time.Now().After(exp) {
		delete(s.m, tok)
		return false
	}
	return true
}

func serveWeb(cfg *Config, store *Store, users map[string]string) error {
	sess := &sessions{m: map[string]time.Time{}}
	mux := http.NewServeMux()

	authed := func(r *http.Request) bool {
		if len(users) == 0 {
			return true
		}
		c, err := r.Cookie("pingping_session")
		return err == nil && sess.valid(c.Value)
	}
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authed(r) {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := staticFS.ReadFile("static/" + name)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		}
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !authed(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		page("index.html")(w, r)
	})
	mux.HandleFunc("/login", page("login.html"))
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))

	// login: constant-time compare, 1s delay on failure to make brute force boring
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct{ User, Pass string }
		json.NewDecoder(r.Body).Decode(&body)
		want, ok := users[body.User]
		if len(users) == 0 || !ok ||
			subtle.ConstantTimeCompare([]byte(body.Pass), []byte(want)) != 1 {
			time.Sleep(time.Second)
			log.Printf("web login failed from %s", r.RemoteAddr)
			http.Error(w, `{"error":"auth"}`, http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "pingping_session", Value: sess.issue(),
			Path: "/", HttpOnly: true, MaxAge: 7 * 24 * 3600, SameSite: http.SameSiteLaxMode})
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "pingping_session", Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/targets", guard(func(w http.ResponseWriter, r *http.Request) {
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
			down := len(rec) > 0 && rec[len(rec)-1].R == 0
			out = append(out, item{
				Name: name, Type: t.Type, Host: targetAddr(t),
				IntervalSec: int(iv.Seconds()), Pace: pace, Down: down,
				Last1h:  calcStats(rec),
				Last24h: calcStats(store.Recent(name, now.Add(-24*time.Hour).Unix())),
			})
		}
		writeJSON(w, out)
	}))

	// raw rounds for smoke (≤24h)
	mux.HandleFunc("/api/series", guard(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("target")
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		if minutes <= 0 || minutes > 1440 {
			minutes = 360
		}
		writeJSON(w, store.Recent(name, time.Now().Add(-time.Duration(minutes)*time.Minute).Unix()))
	}))

	// aggregated buckets for long windows (7d/1M/3M/6M/all)
	mux.HandleFunc("/api/band", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days <= 0 || days > 300 {
			days = 300
		}
		writeJSON(w, store.BandSeries(r.URL.Query().Get("target"), days))
	}))

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
