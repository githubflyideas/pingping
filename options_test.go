package main

import (
	"reflect"
	"testing"
	"time"
)

func TestBuiltinsTakeNoOptionsExceptHTTP(t *testing.T) {
	for _, typ := range []string{"icmp", "tcp"} {
		tg := TargetCfg{Type: typ, Host: "h", Port: 80, Options: map[string]any{"x": 1}}
		if err := normalizeTarget(&tg); errCode(err) != codeUnknownOption {
			t.Errorf("%s: %v", typ, err)
		}
	}
}

// An http target keeps its options through the database, the targets file and a
// reload, and fogping's exported file imports here unchanged.
func TestHTTPTargetRoundTrip(t *testing.T) {
	s, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tg := TargetCfg{Name: "web", Type: typeHTTP, Host: "example.org", Port: 443,
		Options: map[string]any{"path": "/health", "expect_status": 200, "follow_redirects": true}}
	if err := normalizeTarget(&tg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTarget(tg); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ActiveTargets()
	if len(got) != 1 || got[0].OptString("path") != "/health" || got[0].OptInt("expect_status") != 200 ||
		!got[0].OptBool("follow_redirects") || got[0].OptString("scheme") != "https" {
		t.Fatalf("stored %+v", got)
	}
	f := exportTargets(got, "pingping", time.Now())
	if f.Targets[0].Type != "http" || f.Targets[0].Port != 443 || f.Targets[0].Options["path"] != "/health" {
		t.Fatalf("export %+v", f.Targets[0])
	}
	back, _, err := fromFileRow([]byte(`{"type":"http","host":"example.org","port":443,"options":{"path":"/x"}}`))
	if err != nil || back.OptString("path") != "/x" || back.Type != typeHTTP {
		t.Fatalf("import %+v %v", back, err)
	}
	if _, _, err := fromFileRow([]byte(`{"type":"http","host":"example.org","port":443,"options":{"nope":1}}`)); err == nil {
		t.Fatal("unknown option imported")
	}
	again, _ := s.ActiveTargets()
	if !sameTarget(got[0], again[0]) {
		t.Fatal("identical targets compare different, so a reload would restart the probe")
	}
}

// sameTarget lists TargetCfg's fields by hand: a new field must be added there, or
// changing it would not restart the running probe.
func TestSameTargetCoversEveryField(t *testing.T) {
	if n := reflect.TypeOf(TargetCfg{}).NumField(); n != 8 {
		t.Fatalf("TargetCfg has %d fields; update sameTarget (options.go) and this number", n)
	}
}

func TestProbeTypesListsHTTPWithItsOptions(t *testing.T) {
	types := probeTypes()
	if len(types) != 3 || types[0].Type != "icmp" || types[2].Type != typeHTTP || !types[2].UsesPort || len(types[2].Options) != 6 {
		t.Fatalf("%+v", types)
	}
}
