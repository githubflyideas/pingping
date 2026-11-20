package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// webHarness is a console with an admin account already set up.
func webHarness(t *testing.T) (http.Handler, *Runner, string) {
	t.Helper()
	old := loginFailDelay
	loginFailDelay = 0
	t.Cleanup(func() { loginFailDelay = old })
	s, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	run := NewRunner(ProbeCfg{Packets: 1, TimeoutMs: 50}, s, NewDetector(s))
	t.Cleanup(run.Stop)
	h := newMux(&Config{}, s, run)
	w := webCall(h, "POST", "/api/setup", `{"User":"admin","Pass":"correct horse battery"}`, "", "")
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	return h, run, webSession(w)
}

func webCall(h http.Handler, method, path, body, cookie, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://pp.local"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://pp.local")
	if remote != "" {
		req.RemoteAddr = remote
	}
	if cookie != "" {
		req.Header.Set("Cookie", sessionCookie+"="+cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func webSession(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	return ""
}

// Eight wrong passwords and then the right one: the right one must not get in
// while the address is locked, and the operator at another address must.
func TestLoginLockout(t *testing.T) {
	h, _, _ := webHarness(t)
	const attacker, operator = "198.51.100.9:4000", "192.0.2.1:5000"
	wrong := `{"User":"admin","Pass":"wrong password"}`
	right := `{"User":"admin","Pass":"correct horse battery"}`

	for i := 1; i <= 8; i++ {
		w := webCall(h, "POST", "/api/login", wrong, "", attacker)
		want := http.StatusUnauthorized
		if i > loginFreeFails+1 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("wrong password %d: %d, want %d", i, w.Code, want)
		}
	}
	w := webCall(h, "POST", "/api/login", right, "", attacker)
	if w.Code != http.StatusTooManyRequests || webSession(w) != "" {
		t.Fatalf("right password while locked: %d (session %q), want 429 and none", w.Code, webSession(w))
	}
	if w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "too many failed logins") {
		t.Errorf("429 should say why and for how long: %v %s", w.Header(), w.Body)
	}
	if w := webCall(h, "POST", "/api/login", right, "", operator); w.Code != 200 {
		t.Fatalf("the operator at another address was locked out too: %d", w.Code)
	}
}

// A mistyped field name must not create a target with a silently different
// setting: "interval" is not "interval_sec".
func TestTargetUnknownFieldRefused(t *testing.T) {
	h, run, tok := webHarness(t)
	w := webCall(h, "POST", "/api/targets",
		`{"type":"tcp","host":"127.0.0.1","port":1,"interval":30}`, tok, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d %s, want 400", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `unknown field \"interval\"`) || !strings.Contains(w.Body.String(), "interval_sec") {
		t.Errorf("error should name the bad field and the accepted ones: %s", w.Body)
	}
	if n := len(run.Targets()); n != 0 {
		t.Fatalf("a refused request created %d target(s)", n)
	}

	w = webCall(h, "POST", "/api/targets",
		`{"type":"tcp","host":"127.0.0.1","port":1,"interval_sec":30}`, tok, "")
	if w.Code != 200 {
		t.Fatalf("valid create: %d %s", w.Code, w.Body)
	}
	if got := run.Targets(); len(got) != 1 || got[0].IntervalSec != 30 {
		t.Fatalf("interval_sec not applied: %+v", got)
	}
	id := run.Targets()[0].ID
	w = webCall(h, "PUT", "/api/targets/"+strconv.FormatInt(id, 10),
		`{"type":"tcp","host":"127.0.0.1","port":1,"pace":"fast","intervalsec":60}`, tok, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field on update: %d %s, want 400", w.Code, w.Body)
	}
	if got := run.Targets(); got[0].IntervalSec != 30 || got[0].Pace == "fast" {
		t.Fatalf("a refused update changed the target: %+v", got[0])
	}
}
