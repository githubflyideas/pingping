package main

import (
	"strings"
	"testing"
)

// Findings from an outside review of the sibling products, applied here too: an
// oversize body is 413 (the docs promise it), and a mistyped address such as
// 999.999.999.999 is refused instead of being stored as a host name and drawn as
// 100% loss.

func TestNumericButNotIPHostsAreRefused(t *testing.T) {
	for _, bad := range []string{"999.999.999.999", "1.2.3", "300.1.1.1", "123", "1.2.3.4.5"} {
		tg := TargetCfg{Type: "icmp", Host: bad}
		if err := normalizeTarget(&tg); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"10.0.0.1", "example.org", "1e100.net", "::1", "2001:db8::1", "a1.example"} {
		tg := TargetCfg{Type: "icmp", Host: ok}
		if err := normalizeTarget(&tg); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
}

func TestOversizeBodyIs413(t *testing.T) {
	h, session, _ := apiHarness(t)
	big := `{"type":"icmp","host":"` + strings.Repeat("a", 20<<10) + `"}`
	w := apiCall(h, "POST", "/api/targets", big, session, "")
	if w.Code != 413 || !strings.Contains(w.Body.String(), `"too_large"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
