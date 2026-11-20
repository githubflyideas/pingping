package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Behind a reverse proxy on the same host every request comes from loopback;
// the tray-only health endpoint must not answer those.
func TestHealthRefusesProxiedRequests(t *testing.T) {
	h, _, _ := webHarness(t)
	get := func(hdr string) int {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8518/api/health", nil)
		req.RemoteAddr = "127.0.0.1:50000"
		if hdr != "" {
			k, v, _ := strings.Cut(hdr, ": ")
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if c := get(""); c != 200 {
		t.Fatalf("tray request: %d, want 200", c)
	}
	for _, hdr := range []string{"X-Forwarded-For: 203.0.113.5", "Forwarded: for=203.0.113.5", "X-Real-IP: 203.0.113.5", "X-Forwarded-Host: pp.example"} {
		if c := get(hdr); c != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", hdr, c)
		}
	}
}

// There is no advertising to switch off, so there is no switch: --no-sponsor is
// not a flag, and --help says nothing about sponsors.
func TestNoSponsorFlagIsGone(t *testing.T) {
	if _, err := parseOptions([]string{"--no-sponsor"}); err == nil {
		t.Error("--no-sponsor is still accepted")
	}
	var help strings.Builder
	printHelp(&help)
	if strings.Contains(help.String(), "sponsor") {
		t.Error("--help still talks about sponsors")
	}
}

// No advertising anywhere, and nothing that reaches outside: the pages and
// scripts carry no promo slot, no fetch to a foreign host, no mention of the
// old ad repository.
func TestNoAdsAndNoOutbound(t *testing.T) {
	entries, err := os.ReadDir("static")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("static/%s: unexpected directory (ad images used to live in static/ads)", e.Name())
			continue
		}
		b, _ := os.ReadFile("static/" + e.Name())
		if e.Name() == "echarts.min.js" {
			continue // a vendored library; it names many hosts in comments
		}
		for _, bad := range []string{"sponsorTop", "loadSponsor", "sponsor.js", "ads-sponsor", "raw.githubusercontent.com", "/static/ads/"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("static/%s contains %q", e.Name(), bad)
			}
		}
	}
	for _, f := range []string{"sponsor.go", "ads.go"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s is back", f)
		}
	}
	csp := pageCSP([]byte("<script>1</script>"))
	for _, want := range []string{"img-src 'self' data:", "connect-src 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("page CSP lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "https:") || strings.Contains(csp, "githubusercontent") {
		t.Errorf("page CSP allows outside hosts: %s", csp)
	}
}
