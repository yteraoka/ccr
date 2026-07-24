package ccr

import (
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

func TestOpenInBrowserUnset(t *testing.T) {
	t.Setenv("BROWSER", "")
	if err := openInBrowser("http://localhost:8000/"); err == nil {
		t.Fatal("expected error when BROWSER is unset")
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
