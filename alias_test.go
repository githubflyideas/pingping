package main

import (
	"reflect"
	"testing"
)

// The family speaks both spellings: pingping's subcommands (check,
// gen-token FILE) and fogping's flags (--check, --gen-token=F).
// aliasArgs translates only the first argument.
func TestAliasArgs(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{nil, nil},
		{[]string{}, []string{}},
		{[]string{"--check"}, []string{"check"}},
		{[]string{"--check", "--port", "9000"}, []string{"check", "--port", "9000"}},
		{[]string{"--gen-token=tok.txt"}, []string{"gen-token", "tok.txt"}},
		{[]string{"--gen-token", "tok.txt"}, []string{"gen-token", "tok.txt"}},
		{[]string{"--gen-token"}, []string{"gen-token"}},
		{[]string{"check"}, []string{"check"}},
		{[]string{"gen-token", "tok.txt"}, []string{"gen-token", "tok.txt"}},
		{[]string{"--port", "9000"}, []string{"--port", "9000"}},
		{[]string{"--checkout"}, []string{"--checkout"}},
		{[]string{"run", "--check"}, []string{"run", "--check"}},
	}
	for _, c := range cases {
		if got := aliasArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("aliasArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
