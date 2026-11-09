package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func checkSecurityHeaders(t *testing.T, label string, w *httptest.ResponseRecorder) {
	t.Helper()
	h := w.Header()
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("%s: X-Frame-Options = %q, want DENY", label, got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", label, got)
	}
	if got := h.Get("Referrer-Policy"); got != "same-origin" {
		t.Errorf("%s: Referrer-Policy = %q, want same-origin", label, got)
	}
	csp := h.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") && csp != "default-src 'none'; sandbox" {
		t.Errorf("%s: CSP %q neither forbids framing nor is the sandbox policy", label, csp)
	}
	if n := len(h.Values("Content-Security-Policy")); n > 1 {
		t.Errorf("%s: %d CSP headers, want one", label, n)
	}
}

func checkPageCSP(t *testing.T, label string, w *httptest.ResponseRecorder, page string) {
	t.Helper()
	csp := w.Header().Get("Content-Security-Policy")
	b, _ := staticFS.ReadFile("static/" + page)
	hashes := inlineScriptHashes(b)
	if len(hashes) == 0 {
		t.Fatalf("%s: no inline scripts found in %s — the matcher has stopped working", label, page)
	}
	for _, hs := range hashes {
		if !strings.Contains(csp, hs) {
			t.Errorf("%s: CSP lacks hash %s", label, hs)
		}
	}
	for _, bad := range []string{"'unsafe-eval'", "script-src 'self' 'unsafe-inline'", " *"} {
		if strings.Contains(csp, bad) {
			t.Errorf("%s: CSP contains %s: %q", label, bad, csp)
		}
	}
	if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "object-src 'none'") ||
		!strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("%s: page CSP too weak: %q", label, csp)
	}
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	s, _ := NewStore(t.TempDir(), nil)
	defer s.Close()
	run := NewRunner(ProbeCfg{Packets: 1, TimeoutMs: 50}, s, NewDetector(s))
	defer run.Stop()
	h := newMux(&Config{}, s, run)
	do := func(method, path, origin, body, cookie string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://st.local"+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != "" {
			req.Header.Set("Cookie", sessionCookie+"="+cookie)
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	sessionOf := func(w *httptest.ResponseRecorder) string {
		for _, c := range w.Result().Cookies() {
			if c.Name == sessionCookie {
				return c.Value
			}
		}
		t.Fatalf("no session cookie: %d %s", w.Code, w.Body)
		return ""
	}
	want := func(label string, w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Errorf("%s: status %d, want %d", label, w.Code, code)
		}
		checkSecurityHeaders(t, label, w)
	}

	// Before the first account exists.
	want("GET / (no setup)", do("GET", "/", "", "", ""), http.StatusFound)
	w := do("GET", "/setup", "", "", "")
	want("GET /setup", w, 200)
	checkPageCSP(t, "/setup", w, "setup.html")

	adm := sessionOf(do("POST", "/api/setup", "http://st.local", `{"User":"admin","Pass":"correct horse battery"}`, ""))
	want("POST /api/users", do("POST", "/api/users", "", `{"Name":"viewer","Pass":"just looking around","Role":"viewer"}`, adm), 200)
	w = do("POST", "/api/login", "", `{"User":"viewer","Pass":"just looking around"}`, "")
	want("POST /api/login", w, 200)
	view := sessionOf(w)

	for _, c := range []struct{ path, page, cookie string }{
		{"/login", "login.html", ""},
		{"/", "index.html", adm},
		{"/", "index.html", view},
		{"/settings", "settings.html", adm},
	} {
		w := do("GET", c.path, "", "", c.cookie)
		want("GET "+c.path, w, 200)
		checkPageCSP(t, c.path, w, c.page)
		if got := w.Header().Get("ETag"); got == "" {
			t.Errorf("GET %s: lost its ETag", c.path)
		}
		// Revalidation still answers 304 with the same headers.
		w = do("GET", c.path, "", "", c.cookie, "If-None-Match", w.Header().Get("ETag"))
		want("GET "+c.path+" (304)", w, http.StatusNotModified)
	}
	want("GET / (no session)", do("GET", "/", "", "", ""), http.StatusFound)

	for _, c := range []struct {
		method, path, origin, body, cookie string
		code                               int
	}{
		{"GET", "/api/version", "", "", "", 200},
		{"GET", "/api/health", "", "", "", http.StatusNotFound}, // loopback only; httptest is 192.0.2.1
		{"GET", "/api/i18n", "", "", "", 200},
		{"GET", "/api/targets", "", "", "", http.StatusUnauthorized},
		{"GET", "/api/targets", "", "", view, 200},
		{"GET", "/api/me", "", "", view, 200},
		{"GET", "/api/targets/export", "", "", view, 200},
		{"POST", "/api/targets", "", `{"type":"tcp","host":"127.0.0.1","port":1}`, view, http.StatusForbidden},
		{"POST", "/api/targets", "http://evil.example", `{"type":"tcp","host":"127.0.0.1","port":1}`, adm, http.StatusForbidden},
		{"GET", "/api/brand/logo", "", "", view, http.StatusNotFound},
		{"GET", "/static/i18n.js", "", "", "", 200},
		{"GET", "/static/nope.js", "", "", "", http.StatusNotFound},
		{"GET", "/nope", "", "", "", http.StatusNotFound},
	} {
		want(c.method+" "+c.path, do(c.method, c.path, c.origin, c.body, c.cookie), c.code)
	}

	// The custom logo keeps its own, stricter policy.
	if err := s.SetLogo(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)); err != nil {
		t.Fatal(err)
	}
	w = do("GET", "/api/brand/logo", "", "", view)
	want("GET /api/brand/logo", w, 200)
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Errorf("logo CSP = %q, want the sandbox policy", got)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("logo Content-Type = %q", got)
	}
}

// Every <script> in a page is either external (same origin) or an attribute-less
// inline block the hash matcher sees, and no markup uses inline event handlers
// or javascript: URLs, which a hash cannot allow.
func TestPageScriptsAllCovered(t *testing.T) {
	anyScript := regexp.MustCompile(`<script\b[^>]*>`)
	external := regexp.MustCompile(`^<script src="/static/[^"]+"></script>`)
	handler := regexp.MustCompile(`(?i)<[a-z][^>]*\son[a-z]+\s*=`)
	for _, page := range []string{"index.html", "login.html", "setup.html", "settings.html"} {
		b, err := staticFS.ReadFile("static/" + page)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		inline := len(inlineScriptRe.FindAllString(src, -1))
		ext := 0
		for _, loc := range anyScript.FindAllStringIndex(src, -1) {
			if external.MatchString(src[loc[0]:]) {
				ext++
			}
		}
		if total := len(anyScript.FindAllString(src, -1)); total != inline+ext {
			t.Errorf("%s: %d <script> tags but only %d inline + %d external are covered", page, total, inline, ext)
		}
		if m := handler.FindString(src); m != "" {
			t.Errorf("%s: inline event handler %q is blocked by the CSP", page, m)
		}
		if strings.Contains(src, "javascript:") {
			t.Errorf("%s: javascript: URL is blocked by the CSP", page)
		}
	}
}
