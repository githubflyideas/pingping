package main

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Security headers.
//
// Every response says it must not be framed (X-Frame-Options plus CSP
// frame-ancestors) and must not be MIME-sniffed. The console is never meant to
// be embedded in another page. Handlers that set a stricter policy of their own
// (the custom logo and sponsor images: "default-src 'none'; sandbox") replace
// the default one, which is what they want.
//
// The console pages get a full Content-Security-Policy. Their inline <script>
// blocks are allowed by SHA-256 hash, computed from the embedded file itself,
// so editing a page never leaves a stale hash behind. Inline styles stay
// allowed: the pages use style="" attributes and ECharts writes inline styles.
// The sponsor slot may fall back to a manifest on raw.githubusercontent.com
// whose images live on any https host, hence connect-src and img-src below.

const frameOnlyCSP = "frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", frameOnlyCSP)
		next.ServeHTTP(w, r)
	})
}

// inlineScriptRe matches attribute-less <script> blocks: the only inline form the
// pages use. TestPageScriptsAllCovered keeps it that way.
var inlineScriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

func inlineScriptHashes(page []byte) []string {
	var out []string
	for _, m := range inlineScriptRe.FindAllSubmatch(page, -1) {
		sum := sha256.Sum256(m[1])
		out = append(out, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return out
}

// pageCSP is the policy for one console page.
func pageCSP(page []byte) string {
	script := append([]string{"'self'"}, inlineScriptHashes(page)...)
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(script, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"connect-src 'self' https://raw.githubusercontent.com",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		frameOnlyCSP,
	}, "; ")
}

var pageCSPs sync.Map // embedded page path → policy

func pageCSPFor(fsys fs.FS, name string) string {
	if v, ok := pageCSPs.Load(name); ok {
		return v.(string)
	}
	b, _ := fs.ReadFile(fsys, name)
	csp := pageCSP(b)
	pageCSPs.Store(name, csp)
	return csp
}
