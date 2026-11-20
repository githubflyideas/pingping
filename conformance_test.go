package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// docs/conformance/options.json is the shared standard for judging a target's
// options. It is the same file fogping runs (fogping's docs/PROBE-PROTOCOL.md says
// why), so a value is accepted or refused identically in both products.
func TestConformanceOptions(t *testing.T) {
	b, err := os.ReadFile("docs/conformance/options.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Vectors []struct {
			Name    string         `json:"name"`
			Options []optionSpec   `json:"options"`
			Input   map[string]any `json:"input"`
			Want    map[string]any `json:"want"`
			Error   string         `json:"error"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Vectors) == 0 {
		t.Fatalf("vectors: %v (%d)", err, len(f.Vectors))
	}
	for _, v := range f.Vectors {
		tg := TargetCfg{Options: v.Input}
		err := normalizeOptions("vec", v.Options, &tg)
		if v.Error != "" {
			if got := errCode(err); got != v.Error {
				t.Errorf("%s: code %q (%v), want %q", v.Name, got, err, v.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		got, _ := json.Marshal(tg.Options)
		var a, b any
		json.Unmarshal(got, &a)
		want, _ := json.Marshal(v.Want)
		json.Unmarshal(want, &b)
		if len(v.Want) == 0 {
			a, b = len(tg.Options), 0
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: got %s, want %s", v.Name, got, want)
		}
	}
}
