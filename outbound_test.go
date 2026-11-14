package main

import (
	"encoding/json"
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

func TestNoSponsorFlag(t *testing.T) {
	opt, err := parseOptions([]string{"--no-sponsor"})
	if err != nil || !opt.noSponsor {
		t.Fatalf("parse: %+v %v", opt, err)
	}
	cfg, _, err := configure(opt)
	if err != nil || !cfg.NoSponsor {
		t.Fatalf("configure: %+v %v", cfg, err)
	}
	var help strings.Builder
	printHelp(&help)
	if !strings.Contains(help.String(), "--no-sponsor") {
		t.Error("--help does not mention --no-sponsor")
	}

	old := sponsorOff
	sponsorOff = true
	t.Cleanup(func() { sponsorOff = old })
	w := httptest.NewRecorder()
	(*sponsorCache)(nil).serveManifest(w, httptest.NewRequest("GET", "/sponsor/sponsors.json", nil))
	var m struct {
		Disabled bool  `json:"disabled"`
		Slides   []any `json:"slides"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || !m.Disabled || len(m.Slides) != 0 {
		t.Fatalf("manifest with --no-sponsor: %s", w.Body)
	}
	js, _ := os.ReadFile("static/sponsor.js")
	if !strings.Contains(string(js), "m.disabled === true") {
		t.Error("sponsor.js does not stop at a disabled manifest")
	}
}
