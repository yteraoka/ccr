package ccr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withStartCommandStub(t *testing.T) *[]string {
	t.Helper()
	orig := startCommand
	t.Cleanup(func() { startCommand = orig })

	var captured []string
	startCommand = func(argv []string) error {
		captured = argv
		return nil
	}
	return &captured
}

// withFallbackOpenerStub replaces fallbackOpenerArgv for the duration of
// the test and restores the original afterwards, so tests don't depend on
// the real runtime.GOOS of the machine running them.
func withFallbackOpenerStub(t *testing.T, argv []string) {
	t.Helper()
	orig := fallbackOpenerArgv
	t.Cleanup(func() { fallbackOpenerArgv = orig })

	fallbackOpenerArgv = func() []string {
		return append([]string(nil), argv...)
	}
}

func TestOpenInBrowserUnset(t *testing.T) {
	t.Setenv("BROWSER", "")
	// Pin "no platform fallback" so this test is deterministic regardless
	// of the OS actually running `go test` (e.g. a contributor's Mac,
	// where a real fallback is now available).
	withFallbackOpenerStub(t, nil)

	if err := openInBrowser("http://localhost:8000/"); err == nil {
		t.Fatal("expected error when BROWSER is unset and there is no fallback opener")
	}
}

func TestOpenInBrowserFallsBackOnMacOSWhenUnset(t *testing.T) {
	t.Setenv("BROWSER", "")
	withFallbackOpenerStub(t, []string{"open"})
	captured := withStartCommandStub(t)

	if err := openInBrowser("http://localhost:8000/"); err != nil {
		t.Fatalf("openInBrowser: %v", err)
	}
	want := []string{"open", "http://localhost:8000/"}
	if !equalSlices(*captured, want) {
		t.Errorf("argv = %v, want %v", *captured, want)
	}
}

func TestFallbackOpenerArgvFor(t *testing.T) {
	cases := []struct {
		goos string
		want []string
	}{
		{"darwin", []string{"open"}},
		{"linux", nil},
		{"windows", nil},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			got := fallbackOpenerArgvFor(c.goos)
			if !equalSlices(got, c.want) {
				t.Errorf("fallbackOpenerArgvFor(%q) = %v, want %v", c.goos, got, c.want)
			}
		})
	}
}

func TestOpenInBrowserAppendsURLByDefault(t *testing.T) {
	t.Setenv("BROWSER", "google-chrome")
	captured := withStartCommandStub(t)

	if err := openInBrowser("http://localhost:8000/"); err != nil {
		t.Fatalf("openInBrowser: %v", err)
	}
	want := []string{"google-chrome", "http://localhost:8000/"}
	if !equalSlices(*captured, want) {
		t.Errorf("argv = %v, want %v", *captured, want)
	}
}

func TestOpenInBrowserSubstitutesPercentS(t *testing.T) {
	t.Setenv("BROWSER", "myopener --url=%s --flag")
	captured := withStartCommandStub(t)

	if err := openInBrowser("http://localhost:8000/"); err != nil {
		t.Fatalf("openInBrowser: %v", err)
	}
	want := []string{"myopener", "--url=http://localhost:8000/", "--flag"}
	if !equalSlices(*captured, want) {
		t.Errorf("argv = %v, want %v", *captured, want)
	}
}

func TestOpenInBrowserWindowsSideOpenerGetsRawURL(t *testing.T) {
	// A URL needs no WSL path translation, unlike a filesystem path: it
	// should be passed straight through even to a Windows-side opener.
	t.Setenv("BROWSER", "/mnt/c/Windows/System32/rundll32.exe url.dll,FileProtocolHandler")
	captured := withStartCommandStub(t)

	if err := openInBrowser("http://localhost:8000/"); err != nil {
		t.Fatalf("openInBrowser: %v", err)
	}
	want := []string{
		"/mnt/c/Windows/System32/rundll32.exe",
		"url.dll,FileProtocolHandler",
		"http://localhost:8000/",
	}
	if !equalSlices(*captured, want) {
		t.Errorf("argv = %v, want %v", *captured, want)
	}
}

// setupFixtureSession creates a fake ${CLAUDE_CONFIG_DIR}/projects/.../<id>.jsonl
// so the shared transcript server can resolve and render it.
func setupFixtureSession(t *testing.T, sessionID, userText string) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	projDir := filepath.Join(configDir, "projects", encodeProjectDir("/tmp/some/project"))
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"user","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
		`"` + userText + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServeTranscriptSessionServesByPath(t *testing.T) {
	setupFixtureSession(t, "11111111-1111-1111-1111-111111111111", "hello world")

	url, err := serveTranscriptSession("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}
	if !strings.HasPrefix(url, "http://localhost:") {
		t.Fatalf("url = %q, want http://localhost:... prefix", url)
	}
	if !strings.HasSuffix(url, "/11111111-1111-1111-1111-111111111111") {
		t.Fatalf("url = %q, want it to end with the session id", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "hello world") {
		t.Errorf("body = %q, want it to contain %q", body, "hello world")
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestServeTranscriptSessionSharesOnePortAcrossSessions(t *testing.T) {
	setupFixtureSession(t, "22222222-2222-2222-2222-222222222222", "first session")
	url1, err := serveTranscriptSession("22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}

	setupFixtureSession(t, "33333333-3333-3333-3333-333333333333", "second session")
	url2, err := serveTranscriptSession("33333333-3333-3333-3333-333333333333")
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}

	base := func(u string) string { return u[:strings.LastIndex(u, "/")] }
	if base(url1) != base(url2) {
		t.Errorf("expected both sessions to share one server, got %q and %q", url1, url2)
	}
}

func TestServeTranscriptSessionUnknownSessionIs404(t *testing.T) {
	setupFixtureSession(t, "44444444-4444-4444-4444-444444444444", "known session")

	baseURL, err := serveTranscriptSession("44444444-4444-4444-4444-444444444444")
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}
	base := baseURL[:strings.LastIndex(baseURL, "/")]

	resp, err := http.Get(base + "/no-such-session")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestRunningTranscriptServerURLAfterServed(t *testing.T) {
	setupFixtureSession(t, "55555555-5555-5555-5555-555555555555", "hello")
	baseURL, err := serveTranscriptSession("55555555-5555-5555-5555-555555555555")
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}

	url, ok := runningTranscriptServerURL("66666666-6666-6666-6666-666666666666")
	if !ok {
		t.Fatal("expected the shared server to already be running")
	}
	base := baseURL[:strings.LastIndex(baseURL, "/")]
	want := base + "/66666666-6666-6666-6666-666666666666"
	if url != want {
		t.Errorf("runningTranscriptServerURL = %q, want %q", url, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseTranscriptPath(t *testing.T) {
	cases := []struct {
		path       string
		wantID     string
		wantAgent  string
		wantRaw    bool
		wantReject bool
	}{
		{path: "/session-1", wantID: "session-1"},
		{path: "/session-1/", wantID: "session-1"},
		{path: "/session-1/subagents/a111", wantID: "session-1", wantAgent: "a111"},
		// a page's own path plus /raw asks for one line of its jsonl
		{path: "/session-1/raw", wantID: "session-1", wantRaw: true},
		{path: "/session-1/subagents/a111/raw", wantID: "session-1", wantAgent: "a111", wantRaw: true},
		// anything else is not a page this server serves
		{path: "/", wantReject: true},
		{path: "", wantReject: true},
		// "raw" only means the endpoint when it follows a page path; alone it
		// is just a session id shape, and the lookup that follows will 404
		{path: "/raw", wantID: "raw"},
		{path: "/session-1/subagents", wantReject: true},
		{path: "/session-1/other/a111", wantReject: true},
		{path: "/session-1/subagents/a111/extra", wantReject: true},
		{path: "/../../etc/passwd", wantReject: true},
		{path: "/session-1/subagents/../../../etc/passwd", wantReject: true},
	}
	for _, c := range cases {
		id, agent, raw, err := parseTranscriptPath(c.path)
		if c.wantReject {
			if err == nil {
				t.Errorf("parseTranscriptPath(%q) = (%q, %q, %v), want an error", c.path, id, agent, raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTranscriptPath(%q): %v", c.path, err)
			continue
		}
		if id != c.wantID || agent != c.wantAgent || raw != c.wantRaw {
			t.Errorf("parseTranscriptPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.path, id, agent, raw, c.wantID, c.wantAgent, c.wantRaw)
		}
	}
}

func TestServeTranscriptSessionServesSubagentPage(t *testing.T) {
	const sessionID = "55555555-5555-5555-5555-555555555555"
	setupFixtureSession(t, sessionID, "parent session")

	// lay a sub agent transcript down beside the session fixture
	sessionPath, err := findSessionFile(projectsDir(), sessionID)
	if err != nil {
		t.Fatalf("findSessionFile: %v", err)
	}
	subDir := subagentsDir(sessionPath)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"assistant","message":{"content":[{"type":"text","text":"agent said this"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(subDir, "agent-a111.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{"agentType":"Explore","description":"Look around"}`
	if err := os.WriteFile(filepath.Join(subDir, "agent-a111.meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionURLStr, err := serveTranscriptSession(sessionID)
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}
	base := sessionURLStr[:strings.LastIndex(sessionURLStr, "/")]

	// the session page links to the agent
	sessionBody := httpGetBody(t, sessionURLStr)
	if !strings.Contains(sessionBody, subagentURL(sessionID, "a111")) {
		t.Errorf("session page does not link to the sub agent: %q", sessionBody)
	}

	// and that link renders the agent's own transcript
	agentBody := httpGetBody(t, base+subagentURL(sessionID, "a111"))
	if !strings.Contains(agentBody, "agent said this") {
		t.Errorf("sub agent page = %q, want the agent's messages", agentBody)
	}
	if !strings.Contains(agentBody, `href="/`+sessionID+`"`) {
		t.Errorf("sub agent page = %q, want a link back to the session", agentBody)
	}

	// an agent id that isn't there is a 404, not a path escape
	resp, err := http.Get(base + subagentURL(sessionID, "nope"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status for unknown agent = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func httpGetBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func TestServeRawLineReturnsThatLineOnly(t *testing.T) {
	const sessionID = "66666666-6666-6666-6666-666666666666"
	setupFixtureSession(t, sessionID, "first prompt")

	path, err := findSessionFile(projectsDir(), sessionID)
	if err != nil {
		t.Fatalf("findSessionFile: %v", err)
	}
	lines, err := readJSONLLines(path)
	if err != nil || len(lines) == 0 {
		t.Fatalf("readJSONLLines: %v (%d lines)", err, len(lines))
	}
	span := lines[0].span

	pageURL, err := serveTranscriptSession(sessionID)
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}
	rawURL := fmt.Sprintf("%s/raw?offset=%d&len=%d", pageURL, span.offset, span.length)

	body := httpGetBody(t, rawURL)
	if !json.Valid([]byte(body)) {
		t.Errorf("raw line is not valid JSON: %q", body)
	}
	if !strings.Contains(body, "first prompt") {
		t.Errorf("raw line = %q, want the fixture's line", body)
	}

	// the page links to this endpoint rather than embedding the JSON
	page := httpGetBody(t, pageURL)
	if !strings.Contains(page, `class="raw-json"`) {
		t.Error("the page offers no way to ask for the original JSON")
	}
}

func TestServeRawLineRejectsRangesOutsideTheTranscript(t *testing.T) {
	const sessionID = "77777777-7777-7777-7777-777777777777"
	setupFixtureSession(t, sessionID, "a prompt")

	pageURL, err := serveTranscriptSession(sessionID)
	if err != nil {
		t.Fatalf("serveTranscriptSession: %v", err)
	}

	for _, q := range []string{
		"?offset=0&len=999999999",   // past the end of the file
		"?offset=-5&len=10",         // before the start of it
		"?offset=0&len=0",           // empty
		"?offset=abc&len=10",        // not a number
		"?offset=0",                 // no length
		"?offset=0&len=99999999999", // over the per-request cap
	} {
		resp, err := http.Get(pageURL + "/raw" + q)
		if err != nil {
			t.Fatalf("GET %s: %v", q, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET /raw%s = %d, want %d", q, resp.StatusCode, http.StatusBadRequest)
		}
	}
}
