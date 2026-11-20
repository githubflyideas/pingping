package main

import (
	"os"
	"strings"
	"testing"
)

// The docs must not point at a presets/ directory that does not exist.
func TestDocsNoMissingPresets(t *testing.T) {
	if _, err := os.Stat("presets"); err == nil {
		return
	}
	for _, p := range []string{"README.md", "docs/targets-format.md"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "presets/") {
			t.Errorf("%s mentions presets/, which does not exist", p)
		}
	}
}
