package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Export follows GET /api/targets (any signed-in account); import follows the
// target CRUD (admin, same origin, JSON body).
func TestWebTargetsExportImport(t *testing.T) {
	s, _ := NewStore(t.TempDir(), nil)
	defer s.Close()
	run := NewRunner(ProbeCfg{Packets: 1, TimeoutMs: 50}, s, NewDetector(s))
	defer run.Stop()
	h := newMux(&Config{}, s, run)
	file := `{"format":"netping-targets","version":1,"targets":[
		{"name":"a","type":"tcp","host":"127.0.0.1","port":1,"interval":3600},
		{"name":"b","type":"ping","host":"127.0.0.1","interval":3600},
		{"name":"bad","type":"tcp","host":"127.0.0.1"}]}`
	do := func(method, path, ct, origin, body, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://st.local"+path, strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != "" {
			req.Header.Set("Cookie", sessionCookie+"="+cookie)
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
	plan := func(w *httptest.ResponseRecorder) importPlan {
		var p importPlan
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatalf("bad plan JSON %q: %v", w.Body, err)
		}
		return p
	}

	if w := do("GET", "/api/targets/export", "", "", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("export without a session must be 401, got %d", w.Code)
	}
	if w := do("POST", "/api/targets/import", "application/json", "", file, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("import without a session must be 401, got %d", w.Code)
	}
	adm := sessionOf(do("POST", "/api/setup", "application/json", "http://st.local", `{"User":"admin","Pass":"correct horse battery"}`, ""))
	if w := do("POST", "/api/users", "application/json", "", `{"Name":"viewer","Pass":"just looking around","Role":"viewer"}`, adm); w.Code != 200 {
		t.Fatalf("create viewer: %d %s", w.Code, w.Body)
	}
	view := sessionOf(do("POST", "/api/login", "application/json", "", `{"User":"viewer","Pass":"just looking around"}`, ""))

	if w := do("POST", "/api/targets/import?dry_run=1", "application/json", "", file, view); w.Code != http.StatusForbidden {
		t.Fatalf("viewer import must be 403, got %d", w.Code)
	}
	if w := do("POST", "/api/targets/import", "text/plain", "", file, adm); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON import must be 415, got %d", w.Code)
	}
	if w := do("POST", "/api/targets/import", "application/json", "http://evil.example", file, adm); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin import must be 403, got %d", w.Code)
	}
	big := `{"format":"netping-targets","version":1,"targets":[],"pad":"` + strings.Repeat("x", maxImportBytes) + `"}`
	if w := do("POST", "/api/targets/import", "application/json", "", big, adm); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize import must be 413, got %d", w.Code)
	}
	if w := do("POST", "/api/targets/import", "application/json", "", `{"format":"other","version":1,"targets":[]}`, adm); w.Code != 400 {
		t.Fatalf("foreign format must be 400, got %d", w.Code)
	}

	w := do("POST", "/api/targets/import?dry_run=1", "application/json", "http://st.local", file, adm)
	if p := plan(w); w.Code != 200 || !p.DryRun || len(p.Add) != 2 || len(p.Invalid) != 1 || p.Imported != 0 {
		t.Fatalf("dry run: %d %s", w.Code, w.Body)
	}
	if len(run.Targets()) != 0 {
		t.Fatal("dry run changed the target list")
	}
	w = do("POST", "/api/targets/import", "application/json", "http://st.local", file, adm)
	if p := plan(w); w.Code != 200 || p.Imported != 2 {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	if got := run.Targets(); len(got) != 2 {
		t.Fatalf("runner not reloaded: %+v", got)
	}
	w = do("POST", "/api/targets/import", "application/json", "", file, adm)
	if p := plan(w); w.Code != 200 || p.Imported != 0 || len(p.Skip) != 2 {
		t.Fatalf("second import should skip both: %d %s", w.Code, w.Body)
	}

	w = do("GET", "/api/targets/export", "", "", "", view)
	if w.Code != 200 {
		t.Fatalf("viewer export: %d", w.Code)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="pingping-targets-`) {
		t.Fatalf("Content-Disposition %q", cd)
	}
	var f targetsFile
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil || f.Format != targetsFormat || len(f.Targets) != 2 ||
		f.Targets[0].Name != "a" || f.Targets[1].Type != "ping" {
		t.Fatalf("export body: %v %s", err, w.Body)
	}
	if strings.Contains(w.Body.String(), "admin") || strings.Contains(w.Body.String(), "pbkdf2") {
		t.Fatal("export mentions accounts")
	}
}
