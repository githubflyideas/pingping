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
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

// sessionStore 内存会话:登录成功发随机 token 存 cookie,进程重启全员重登 —— 对内网工具是可接受的简单。
type sessionStore struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (s *sessionStore) issue() string {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.m[tok] = time.Now().Add(7 * 24 * time.Hour)
	s.mu.Unlock()
	return tok
}

func (s *sessionStore) valid(r *http.Request) bool {
	c, err := r.Cookie("pp_session")
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[c.Value]
	if !ok || time.Now().After(exp) {
		delete(s.m, c.Value)
		return false
	}
	return true
}

func (s *sessionStore) drop(r *http.Request) {
	if c, err := r.Cookie("pp_session"); err == nil {
		s.mu.Lock()
		delete(s.m, c.Value)
		s.mu.Unlock()
	}
}

func serveWeb(cfg *Config, store *Store, det *Detector, n *Notifier) {
	sess := &sessionStore{m: map[string]time.Time{}}
	authOn := cfg.WebPassword != ""
	mux := http.NewServeMux()

	serveStatic := func(w http.ResponseWriter, name, ctype string) {
		b, _ := staticFS.ReadFile("static/" + name)
		w.Header().Set("Content-Type", ctype)
		w.Write(b)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveStatic(w, "index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if !authOn {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		serveStatic(w, "login.html", "text/html; charset=utf-8")
	})
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))

	// 登录:恒定时间比较,防时序探测。密码是配置里的明文 —— 内网工具的务实取舍。
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var in struct{ User, Pass string }
		json.NewDecoder(r.Body).Decode(&in)
		uOK := subtle.ConstantTimeCompare([]byte(in.User), []byte(cfg.WebUser)) == 1
		pOK := subtle.ConstantTimeCompare([]byte(in.Pass), []byte(cfg.WebPassword)) == 1
		if !authOn || !uOK || !pOK {
			time.Sleep(300 * time.Millisecond) // 拖慢爆破
			http.Error(w, `{"error":"auth"}`, http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "pp_session", Value: sess.issue(),
			Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		sess.drop(r)
		http.SetCookie(w, &http.Cookie{Name: "pp_session", Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"auth": authOn, "version": version, "instance": cfg.Instance})
	})

	// 目标列表 + 状态 + 统计 + 主机页所需的配置字段
	mux.HandleFunc("/api/targets", func(w http.ResponseWriter, r *http.Request) {
		statuses := det.Snapshot()
		now := time.Now()
		type item struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Host        string `json:"host"`
			IntervalSec int    `json:"interval_sec"`
			Pace        string `json:"pace"`
			Sensitivity string `json:"sensitivity"`
			AlertsOff   bool   `json:"alerts_off"`
			Status      Status `json:"status"`
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
			pace, sens := t.Pace, t.Sensitivity
			if pace == "" {
				pace = "normal"
			}
			if sens == "" {
				sens = "normal"
			}
			out = append(out, item{
				Name: name, Type: t.Type, Host: targetAddr(t),
				IntervalSec: int(iv.Seconds()), Pace: pace, Sensitivity: sens,
				AlertsOff: t.Alerts != nil && !*t.Alerts,
				Status:    statuses[name],
				Last1h:    calcStats(store.Recent(name, now.Add(-time.Hour).Unix())),
				Last24h:   calcStats(store.Recent(name, now.Add(-24*time.Hour).Unix())),
			})
		}
		writeJSON(w, out)
	})

	// 通知配置展示:URL 打码、secret 只报"有没有" —— webhook URL 本身就是凭证,不上屏。
	mux.HandleFunc("/api/webhooks", func(w http.ResponseWriter, r *http.Request) {
		type item struct {
			Name      string   `json:"name"`
			URL       string   `json:"url"`
			Kinds     []string `json:"kinds"`
			HasSecret bool     `json:"has_secret"`
		}
		var out []item
		for _, wh := range cfg.Webhooks {
			out = append(out, item{Name: wh.Name, URL: maskURL(wh.URL),
				Kinds: wh.Kinds, HasSecret: wh.Secret != ""})
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("/api/series", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("target")
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		if minutes <= 0 || minutes > 1440 {
			minutes = 360
		}
		rounds := store.Recent(name, time.Now().Add(-time.Duration(minutes)*time.Minute).Unix())
		writeJSON(w, rounds)
	})

	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, store.SummaryLines(r.URL.Query().Get("target"), 30))
	})

	mux.HandleFunc("/api/push", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		n.SendReport("📊 链路实时报告(手动触发)", "manual")
		writeJSON(w, map[string]string{"ok": "pushed"})
	})

	handler := http.Handler(mux)
	if authOn {
		handler = requireLogin(mux, sess)
	}
	log.Fatal(http.ListenAndServe(cfg.Listen, handler))
}

// requireLogin:登录页、登录接口、静态资源、meta 放行,其余页面跳登录、API 返 401。
func requireLogin(next http.Handler, sess *sessionStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/login" || p == "/api/login" || p == "/api/meta" || strings.HasPrefix(p, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if sess.valid(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

// maskURL 保留可辨认的头尾,遮掉中段凭证部分。
func maskURL(u string) string {
	if len(u) <= 44 {
		if len(u) > 12 {
			return u[:len(u)-8] + "…"
		}
		return u
	}
	return u[:36] + "……" + u[len(u)-6:]
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
