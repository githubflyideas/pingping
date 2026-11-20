package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

// loginFailDelay is paid by every wrong password before the answer goes out.
// Tests shorten it.
var loginFailDelay = time.Second

// sessions are in memory, so a restart logs everyone out. For a single-binary tool
// that is the right trade: no session table to migrate, no token to leak from disk,
// and a service restart is rare enough that re-login is not a burden.
type sessions struct {
	mu sync.Mutex
	m  map[string]session
}

type session struct {
	user string
	role string
	exp  time.Time
}

func newSessions() *sessions { return &sessions{m: map[string]session{}} }

func (s *sessions) issue(user, role string) string {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.m[tok] = session{user: user, role: role, exp: time.Now().Add(7 * 24 * time.Hour)}
	s.mu.Unlock()
	return tok
}

func (s *sessions) get(tok string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn, ok := s.m[tok]
	if !ok || time.Now().After(sn.exp) {
		delete(s.m, tok)
		return session{}, false
	}
	return sn, true
}

// dropUser invalidates the sessions of one account — used when its password
// changes or it is deleted, so a cookie never outlives the credential behind it.
func (s *sessions) dropUser(name string) {
	s.mu.Lock()
	for tok, sn := range s.m {
		if sn.user == name {
			delete(s.m, tok)
		}
	}
	s.mu.Unlock()
}

func (s *sessions) drop(tok string) {
	s.mu.Lock()
	delete(s.m, tok)
	s.mu.Unlock()
}

const sessionCookie = "pingping_session"

// webApp holds what the HTTP handlers share. newMux only wires routes; each
// area of the console registers its own, so no single function grows past a
// screenful.
type webApp struct {
	cfg      *Config
	store    *Store
	run      *Runner
	sess     *sessions
	throttle *loginThrottle
	as       *assets
}

func newMux(cfg *Config, store *Store, run *Runner) http.Handler {
	return securityHeaders(apiRules(buildMux(cfg, store, run)))
}

// buildMux is newMux without the outer wrappers, so tests can ask the router
// which pattern serves a request.
func buildMux(cfg *Config, store *Store, run *Runner) *http.ServeMux {
	a := &webApp{cfg: cfg, store: store, run: run, sess: newSessions(),
		throttle: newLoginThrottle(), as: embeddedAssets()}
	mux := http.NewServeMux()
	a.registerPages(mux)
	a.registerAuth(mux)
	a.registerUsers(mux)
	a.registerInfo(mux)
	a.registerBrand(mux)
	a.registerTargets(mux)
	registerStats(mux, cfg.ZabbixToken, store, run)
	return mux
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func (a *webApp) current(r *http.Request) (session, bool) { return a.sess.get(sessionToken(r)) }

func (a *webApp) authed(r *http.Request) bool { _, ok := a.current(r); return ok }

func setSessionCookie(w http.ResponseWriter, tok string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/",
		HttpOnly: true, MaxAge: maxAge, SameSite: http.SameSiteLaxMode,
	})
}

// guard is the only gate. There is no anonymous read path: an open console
// would expose which hosts this machine watches, which is itself a map of the
// network.
func (a *webApp) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authed(r) {
			jsonErr(w, http.StatusUnauthorized, "sign in first")
			return
		}
		t0 := time.Now()
		h(w, r)
		if d := time.Since(t0); d > time.Second {
			log.Printf("slow request: %s %s took %v", r.URL.Path, r.URL.RawQuery, d)
		}
	}
}

// write additionally refuses cross-origin form posts. A JSON content type
// forces a CORS preflight that we never answer, so a page on another site
// cannot drive these endpoints with the operator's cookie.
func (a *webApp) write(h http.HandlerFunc) http.HandlerFunc {
	return a.guard(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			jsonErr(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		h(w, r)
	})
}

// admin wraps a write that changes what this host probes, or who may sign in.
// A viewer reaching one gets 403 rather than 404: they are authenticated, and
// pretending the endpoint does not exist would only make the UI confusing.
func (a *webApp) admin(h http.HandlerFunc) http.HandlerFunc {
	return a.write(func(w http.ResponseWriter, r *http.Request) {
		sn, _ := a.current(r)
		if sn.role != RoleAdmin {
			jsonErr(w, http.StatusForbidden, "this account is read-only")
			return
		}
		h(w, r)
	})
}

// page serves one HTML page. Pages and assets both go through assets.serve,
// which gives them an ETag and `no-cache`. See assets.go for why: without a
// validator a browser is entitled to keep serving the markup from the version
// you just upgraded away from.
func (a *webApp) page(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", pageCSPFor(a.as.fs, "static/"+name))
	a.as.serve(w, r, "static/"+name, "text/html; charset=utf-8")
}

// pageGate serves a page, or redirects first: to /setup while no account
// exists (unless this is /setup), and to /login when signed out and the page
// needs a session.
func (a *webApp) pageGate(name string, needsSession bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.store.NeedsSetup() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		if needsSession && !a.authed(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		a.page(w, r, name)
	}
}

func (a *webApp) registerPages(mux *http.ServeMux) {
	// Pages are bound to GET like every endpoint, so the router itself can tell a
	// wrong method (405) from a wrong path (404); a catch-all "/" would answer
	// every request it does not recognise with 404, API calls included.
	mux.HandleFunc("GET /{$}", a.pageGate("index.html", true))
	mux.HandleFunc("GET /login", a.pageGate("login.html", false))
	mux.HandleFunc("GET /setup", func(w http.ResponseWriter, r *http.Request) {
		if !a.store.NeedsSetup() {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		a.page(w, r, "setup.html")
	})
	mux.HandleFunc("GET /settings", a.pageGate("settings.html", true))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		name := assetPath(r.URL.Path)
		if name == "" || name == "static" {
			http.NotFound(w, r)
			return
		}
		a.as.serve(w, r, name, "")
	})
}

func (a *webApp) registerAuth(mux *http.ServeMux) {
	// ---- first run ----
	// Open until a password exists, closed forever after. The window is the same
	// one Technitium leaves open, and it is why the installer tells the operator to
	// open the console straight away.
	mux.HandleFunc("POST /api/setup", a.handleSetup)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		a.sess.drop(sessionToken(r))
		setSessionCookie(w, "", -1)
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Changing your own password needs the old one. Viewers may do this too: it is
	// their account.
	mux.HandleFunc("POST /api/password", a.write(a.handlePassword))
}

func (a *webApp) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !a.store.NeedsSetup() {
		jsonErr(w, http.StatusConflict, "already set up")
		return
	}
	if !sameOrigin(r) {
		jsonErr(w, http.StatusForbidden, "cross-origin request refused")
		return
	}
	var body struct{ User, Pass string }
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.User == "" {
		body.User = "admin"
	}
	if err := validateCredentials(body.User, body.Pass); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.CreateUser(body.User, body.Pass, RoleAdmin); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("console: admin account %q created", body.User)
	setSessionCookie(w, a.sess.issue(body.User, RoleAdmin), 7*24*3600)
	writeJSON(w, map[string]bool{"ok": true})
}

func validateCredentials(user, pass string) error {
	if err := validateUsername(user); err != nil {
		return err
	}
	return validatePassword(pass)
}

func (a *webApp) handleLogin(w http.ResponseWriter, r *http.Request) {
	// The lockout is per client address, not per account (see
	// loginthrottle.go), so an attacker can lock out only themselves and never
	// the operator.
	key := loginClientKey(r)
	if wait, ok := a.throttle.begin(key); !ok {
		w.Header().Set("Retry-After", retryAfter(wait))
		jsonErr(w, http.StatusTooManyRequests,
			fmt.Sprintf("too many failed logins; try again in %s seconds", retryAfter(wait)))
		return
	}
	var body struct{ User, Pass string }
	if !decodeJSON(w, r, &body) {
		a.throttle.release(key)
		return
	}
	role, ok := a.store.Authenticate(body.User, body.Pass)
	if !ok {
		a.loginFailed(w, r, key, body.User)
		return
	}
	a.throttle.success(key)
	setSessionCookie(w, a.sess.issue(body.User, role), 7*24*3600)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *webApp) loginFailed(w http.ResponseWriter, r *http.Request, key, user string) {
	time.Sleep(loginFailDelay)
	if lock := a.throttle.fail(key); lock > 0 {
		log.Printf("console: failed login for %q from %s; address locked for %v", user, r.RemoteAddr, lock)
	} else {
		log.Printf("console: failed login for %q from %s", user, r.RemoteAddr)
	}
	jsonErr(w, http.StatusUnauthorized, "wrong username or password")
}

func (a *webApp) handlePassword(w http.ResponseWriter, r *http.Request) {
	sn, _ := a.current(r)
	var body struct{ Old, New string }
	if !decodeJSON(w, r, &body) {
		return
	}
	if _, ok := a.store.Authenticate(sn.user, body.Old); !ok {
		time.Sleep(time.Second)
		jsonErr(w, http.StatusUnauthorized, "current password is wrong")
		return
	}
	if err := a.store.SetPassword(sn.user, body.New); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a.sess.dropUser(sn.user)
	log.Printf("console: %q changed their password; their sessions were invalidated", sn.user)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- accounts and settings ----

func (a *webApp) registerUsers(mux *http.ServeMux) {
	// Who am I, and what may I do. The console asks this first and hides what the
	// answer says is unavailable — the server still enforces it, the UI just stops
	// offering buttons that would 403.
	mux.HandleFunc("GET /api/me", a.guard(a.handleMe))
	mux.HandleFunc("GET /api/users", a.guard(a.handleListUsers))
	mux.HandleFunc("POST /api/users", a.admin(a.handleCreateUser))
	mux.HandleFunc("POST /api/users/{name}/password", a.admin(a.handleResetPassword))
	mux.HandleFunc("POST /api/users/{name}/role", a.admin(a.handleSetRole))
	mux.HandleFunc("DELETE /api/users/{name}", a.admin(a.handleDeleteUser))
	// The console port is a stored setting, not a service argument, so it can be
	// changed here. It cannot take effect until the listener is rebuilt, and
	// rebuilding it underneath the request that asked would drop that request — so
	// this records the intent and the answer says what to do next.
	mux.HandleFunc("POST /api/settings/port", a.admin(a.handleSettingsPort))
}

func (a *webApp) hasLogo() bool { _, _, ok := a.store.Logo(); return ok }

func (a *webApp) handleMe(w http.ResponseWriter, r *http.Request) {
	sn, _ := a.current(r)
	writeJSON(w, map[string]any{
		"user":     sn.user,
		"role":     sn.role,
		"version":  version,
		"data_dir": a.cfg.DataDir,
		"listen":   a.cfg.Listen,
		"port":     portNum(a.cfg.Listen),
		// What is running now, and what is stored for next time. They differ
		// between a change and the restart that applies it, and the page has
		// to be able to say which is which.
		"bind":           hostOf(a.cfg.Listen),
		"bind_pending":   a.store.ConsoleBind(),
		"retention_days": a.cfg.RetentionDays,
		"has_logo":       a.hasLogo(),
	})
}

func (a *webApp) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if sn, _ := a.current(r); sn.role != RoleAdmin {
		jsonErr(w, http.StatusForbidden, "this account is read-only")
		return
	}
	us, err := a.store.Users()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if us == nil {
		us = []User{}
	}
	writeJSON(w, us)
}

// userErr answers an account error and reports whether there was one.
func userErr(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errUserExists), errors.Is(err, errLastAdmin):
		jsonErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, errUserNotFound):
		jsonErr(w, http.StatusNotFound, err.Error())
	default:
		jsonErr(w, http.StatusBadRequest, err.Error())
	}
	return true
}

func (a *webApp) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var b struct{ Name, Pass, Role string }
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.Role == "" {
		b.Role = RoleViewer // the safe default: looking, not changing
	}
	if userErr(w, a.store.CreateUser(b.Name, b.Pass, b.Role)) {
		return
	}
	log.Printf("console: account %q created with role %s", b.Name, b.Role)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *webApp) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var b struct{ Pass string }
	if !decodeJSON(w, r, &b) {
		return
	}
	if userErr(w, a.store.SetPassword(name, b.Pass)) {
		return
	}
	a.sess.dropUser(name)
	log.Printf("console: password reset for %q; their sessions were invalidated", name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *webApp) handleSetRole(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var b struct{ Role string }
	if !decodeJSON(w, r, &b) {
		return
	}
	if userErr(w, a.store.SetRole(name, b.Role)) {
		return
	}
	// The role is carried in the session, so it has to be re-issued.
	a.sess.dropUser(name)
	log.Printf("console: %q is now %s", name, b.Role)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *webApp) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if sn, _ := a.current(r); name == sn.user {
		jsonErr(w, http.StatusConflict, "you cannot delete the account you are signed in with")
		return
	}
	if userErr(w, a.store.DeleteUser(name)) {
		return
	}
	a.sess.dropUser(name)
	log.Printf("console: account %q deleted", name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *webApp) handleSettingsPort(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Port int
		Bind string
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	// Validate both before storing either. Half-applying this pair is how a
	// service ends up bound to an address it cannot reach on a port nothing
	// admits, with no console left to correct it from.
	if err := validateConsoleAddr(b.Port, b.Bind); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.storeConsoleAddr(b.Port, b.Bind); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("console: port will be %d after a restart", b.Port)
	writeJSON(w, map[string]any{
		"ok":      true,
		"port":    b.Port,
		"bind":    b.Bind,
		"applied": false,
		"note":    "Restart the pingping service to apply it — the tray icon can do that, and it re-points the firewall rule at the same time.",
	})
}

// validateConsoleAddr checks the port and bind address that were given; zero
// and empty mean "unchanged".
func validateConsoleAddr(port int, bind string) error {
	if port != 0 {
		if err := validatePortValue(port); err != nil {
			return err
		}
	}
	if bind != "" {
		return validateBind(bind)
	}
	return nil
}

func (a *webApp) storeConsoleAddr(port int, bind string) error {
	if port != 0 {
		if err := a.store.SetConsolePort(port); err != nil {
			return err
		}
	}
	if bind != "" {
		if err := a.store.SetConsoleBind(bind); err != nil {
			return err
		}
		log.Printf("console: bind address will be %s after a restart", bind)
	}
	return nil
}

// ---- read ----

func (a *webApp) registerInfo(mux *http.ServeMux) {
	// Health is what the notification-area icon polls. It is unauthenticated,
	// because the tray has no session and cannot get one — so it is restricted to
	// loopback and carries counts only. No hostnames: the list of what a machine
	// watches is a map of the network, and that stays behind the login.
	//
	// A request a reverse proxy forwarded also arrives from loopback, and would
	// hand these counts to anyone the proxy serves. The tray talks to the port
	// directly and never sends forwarding headers, so their presence means the
	// request came from somewhere else.
	mux.HandleFunc("GET /api/health", a.handleHealth)
	// The catalogue, one language at a time. Unauthenticated on purpose: the sign-in
	// and first-run screens need it before anyone has a session, and it contains
	// nothing but interface text.
	mux.HandleFunc("GET /api/i18n", func(w http.ResponseWriter, r *http.Request) {
		lang := normalizeLang(r.URL.Query().Get("lang"))
		if lang == "" {
			lang = acceptLanguage(r.Header.Get("Accept-Language"))
		}
		w.Header().Set("Cache-Control", "no-cache")
		writeJSON(w, bundle(lang))
	})
	mux.HandleFunc("GET /api/version", a.handleVersion)
	// The probe types this build can run and each type's own options, so the editor
	// builds its type list and form from the server (the same endpoint fogping has).
	mux.HandleFunc("GET /api/probe-types", a.guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, probeTypes())
	}))
	mux.HandleFunc("GET /api/targets", a.guard(a.handleTargets))
	mux.HandleFunc("GET /api/series", a.guard(a.handleSeries))
}

func (a *webApp) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) || forwarded(r) {
		http.NotFound(w, r)
		return
	}
	targets := a.run.Targets()
	down := 0
	since := time.Now().Add(-time.Hour).Unix()
	for _, t := range targets {
		if rec := a.store.Recent(t.Name, since); len(rec) > 0 && rec[len(rec)-1].R == 0 {
			down++
		}
	}
	// Both ports, so the tray can follow a change it did not make. It polls
	// the effective one; if configured differs, a restart is pending and the
	// tray knows where to look afterwards.
	configured := a.store.ConsolePort()
	if configured == 0 {
		configured = portNum(a.cfg.Listen)
	}
	writeJSON(w, map[string]any{
		"version":         version,
		"needs_setup":     a.store.NeedsSetup(),
		"targets":         len(targets),
		"down":            down,
		"port":            portNum(a.cfg.Listen),
		"port_configured": configured,
	})
}

func (a *webApp) handleVersion(w http.ResponseWriter, r *http.Request) {
	// Anonymous callers get the version and nothing else: retention, whether a
	// logo is set and whether setup is still pending describe the instance, and
	// nothing before sign-in reads them (the console only loads once signed in,
	// and the first-run redirect is decided server-side).
	if !a.authed(r) {
		writeJSON(w, map[string]any{"version": version})
		return
	}
	writeJSON(w, map[string]any{
		"version":        version,
		"retention_days": a.cfg.RetentionDays,
		"hot_days":       a.cfg.HotDays, // 原始样本保留天数,控制台据此判断对比段读原始层还是小时汇总
		"has_logo":       a.hasLogo(),
		"needs_setup":    a.store.NeedsSetup(),
		"authed":         a.authed(r),
	})
}

// targetItem is one row of GET /api/targets.
type targetItem struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Host        string         `json:"host"`
	Port        int            `json:"port"`
	Addr        string         `json:"addr"`
	IntervalSec int            `json:"interval_sec"` // effective
	IntervalSet int            `json:"interval_set"` // explicit override, 0 = from pace
	Pace        string         `json:"pace"`
	Options     map[string]any `json:"options,omitempty"`
	Down        bool           `json:"down"`
	Last1h      Stats          `json:"last_1h"`
	Last24h     Stats          `json:"last_24h"`
}

func (a *webApp) handleTargets(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	out := []targetItem{}
	for _, t := range a.run.Targets() {
		out = append(out, a.targetItem(t, now))
	}
	writeJSON(w, out)
}

func (a *webApp) targetItem(t TargetCfg, now time.Time) targetItem {
	iv, _ := probeParams(t, a.cfg.Probe)
	pace := t.Pace
	if pace == "" {
		pace = "normal"
	}
	rec := a.store.Recent(t.Name, now.Add(-time.Hour).Unix())
	down := len(rec) > 0 && rec[len(rec)-1].R == 0
	return targetItem{
		ID: t.ID, Name: t.Name, Type: t.Type, Host: t.Host, Port: t.Port, Addr: targetAddr(t),
		IntervalSec: int(iv.Seconds()), IntervalSet: t.IntervalSec, Pace: pace, Options: t.Options, Down: down,
		Last1h:  calcStats(rec),
		Last24h: calcStats(a.store.Recent(t.Name, now.Add(-24*time.Hour).Unix())),
	}
}

// handleSeries returns raw rounds for smoke. Supports either minutes=N (recent
// window) or from/to unix (arbitrary range). No thinning: every sample is
// returned as stored.
func (a *webApp) handleSeries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("target")
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	if from == 0 || to == 0 {
		minutes, _ := strconv.Atoi(q.Get("minutes"))
		if minutes <= 0 || minutes > 432000 { // up to 300 days
			minutes = 360
		}
		to = time.Now().Unix()
		from = to - int64(minutes)*60
	}
	writeJSON(w, a.store.ReadRange(r.Context(), name, from, to))
}

// ---- brand ----

func (a *webApp) registerBrand(mux *http.ServeMux) {
	// The logo is served from the database rather than as a static file so a
	// portable copy stays one directory. nosniff and an explicit Content-Type
	// matter here more than elsewhere: these bytes came from a user, and the
	// browser must not be free to decide they are something executable.
	mux.HandleFunc("GET /api/brand/logo", a.guard(func(w http.ResponseWriter, r *http.Request) {
		raw, ct, ok := a.store.Logo()
		if !ok {
			jsonErr(w, http.StatusNotFound, "no custom logo")
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(raw)
	}))
	mux.HandleFunc("POST /api/brand/logo", a.admin(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxLogoBytes+1))
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "could not read the upload")
			return
		}
		if err := a.store.SetLogo(raw); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("console: custom logo set (%d bytes)", len(raw))
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("DELETE /api/brand/logo", a.admin(func(w http.ResponseWriter, r *http.Request) {
		if err := a.store.ClearLogo(); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("console: custom logo cleared")
		writeJSON(w, map[string]bool{"ok": true})
	}))
}

// ---- target CRUD ----
// Every write goes to the database first, then Runner.Reload picks it up, so a
// half-applied change cannot exist: either the row is there and probing, or it
// is not there at all.

func (a *webApp) registerTargets(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/targets", a.admin(a.handleCreateTarget))
	// Export and import of the target list as a "netping-targets" file (see
	// targetsio.go). Export is gated like GET /api/targets, so a viewer can take a
	// copy of what they can already read; import changes what this host probes and
	// is admin-only, like every other target write.
	mux.HandleFunc("GET /api/targets/export", a.guard(func(w http.ResponseWriter, r *http.Request) {
		serveTargetsExport(w, a.store, "pingping")
	}))
	mux.HandleFunc("POST /api/targets/import", a.admin(func(w http.ResponseWriter, r *http.Request) {
		serveTargetsImport(w, r, a.store, a.run)
	}))
	mux.HandleFunc("PUT /api/targets/{id}", a.admin(a.handleUpdateTarget))
	mux.HandleFunc("DELETE /api/targets/{id}", a.admin(a.handleDeleteTarget))
}

func targetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || n <= 0 {
		jsonErr(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return n, true
}

func decodeTarget(w http.ResponseWriter, r *http.Request) (TargetCfg, bool) {
	// Strict: an unknown field is a typo ("interval" for "interval_sec"), and
	// accepting it would create the target with a setting the caller never
	// asked for, with a 200 that says everything went fine.
	var t TargetCfg
	if !decodeJSONStrict(w, r, &t, targetFields) {
		return t, false
	}
	if err := normalizeTarget(&t); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return t, false
	}
	return t, true
}

func (a *webApp) done(w http.ResponseWriter, t TargetCfg, err error, verb string) {
	switch {
	case errors.Is(err, errNameTaken), errors.Is(err, errNameDeleted):
		jsonErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, errNotFound):
		jsonErr(w, http.StatusNotFound, err.Error())
	case err != nil:
		log.Printf("target %s: %v", verb, err)
		jsonErr(w, http.StatusInternalServerError, err.Error())
	default:
		if err := a.run.Reload(); err != nil {
			log.Printf("reload after %s: %v", verb, err)
		}
		log.Printf("console: target %s %q (%s)", verb, t.Name, targetAddr(t))
		writeJSON(w, t)
	}
}

func (a *webApp) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	if t, ok := decodeTarget(w, r); ok {
		t, err := a.store.CreateTarget(t)
		a.done(w, t, err, "created")
	}
}

func (a *webApp) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := targetID(w, r)
	if !ok {
		return
	}
	if t, ok := decodeTarget(w, r); ok {
		t.ID = id
		t, err := a.store.UpdateTarget(t)
		a.done(w, t, err, "updated")
	}
}

func (a *webApp) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := targetID(w, r)
	if !ok {
		return
	}
	var t TargetCfg
	for _, x := range a.run.Targets() {
		if x.ID == id {
			t = x
		}
	}
	a.done(w, t, a.store.DeactivateTarget(id), "deleted")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONStrict(w, r, v, "")
}

// targetFields is what POST and PUT /api/targets accept, for the error that
// names a field they do not.
const targetFields = "name, type, host, port, pace, interval_sec, options"

// decodeJSONStrict is decodeJSON that also refuses fields v does not have, when
// accepted (the list to quote back in the error) is not empty.
func decodeJSONStrict(w http.ResponseWriter, r *http.Request, v any, accepted string) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		jsonErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if accepted != "" {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			jsonErr(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return false
		}
		msg := "bad JSON: " + err.Error()
		if f, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok && accepted != "" {
			msg = "unknown field " + f + "; accepted fields: " + accepted
		}
		jsonErr(w, http.StatusBadRequest, msg)
		return false
	}
	return true
}

// acceptLanguage picks the best catalogue match from a browser's header. Quality
// values are honoured because a browser set to "ja, en;q=0.8" means it, and
// ignoring q would hand that user English.
func acceptLanguage(h string) string {
	type cand struct {
		tag string
		q   float64
	}
	var best cand
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			if _, err := fmt.Sscanf(strings.TrimSpace(part[i+1:]), "q=%f", &q); err != nil {
				q = 1.0
			}
		}
		if l := normalizeLang(tag); l != "" && q > best.q-0.0001 && (best.tag == "" || q > best.q) {
			best = cand{l, q}
		}
	}
	if best.tag == "" {
		return langEN
	}
	return best.tag
}

// portNum extracts the numeric port from a listen address.
func portNum(listen string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(portOf(listen), ":"))
	return n
}

// isLoopback reports whether the request came from this machine. Used only to
// scope the unauthenticated health endpoint; everything else goes through auth.
// forwarded reports whether a proxy relayed the request on someone's behalf.
func forwarded(r *http.Request) bool {
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameOrigin rejects browser requests whose Origin doesn't match the Host they were
// sent to. Non-browser clients (curl) send no Origin and are let through to auth.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}
