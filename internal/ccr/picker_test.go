package ccr

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSessionFile(t *testing.T, dir, id, cwd string) {
	t.Helper()
	content := `{"type":"user","cwd":"` + cwd + `","timestamp":"2020-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsForPickerScopedToCwd(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	cwd := t.TempDir()
	t.Chdir(cwd)

	projectDir := filepath.Join(configDir, "projects", encodeProjectDir(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, projectDir, "session-a", cwd)

	otherProjectDir := filepath.Join(configDir, "projects", "some-other-project")
	if err := os.MkdirAll(otherProjectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, otherProjectDir, "session-b", "/somewhere/else")

	entries, err := sessionsForPicker(false)
	if err != nil {
		t.Fatalf("sessionsForPicker: %v", err)
	}
	if len(entries) != 1 || entries[0].id != "session-a" {
		t.Fatalf("got %+v, want only session-a", entries)
	}
}

func TestSessionsForPickerGlobal(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Chdir(t.TempDir())

	for _, name := range []string{"project-one", "project-two"} {
		dir := filepath.Join(configDir, "projects", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeSessionFile(t, dir, "session", "/proj")
	}

	entries, err := sessionsForPicker(true)
	if err != nil {
		t.Fatalf("sessionsForPicker: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestSessionsForPickerNoProjectDir(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Chdir(t.TempDir())

	entries, err := sessionsForPicker(false)
	if err != nil {
		t.Fatalf("sessionsForPicker: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
}

func TestPrintUsage(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	PrintUsage()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(out), "ccr [-g]") {
		t.Errorf("PrintUsage output = %q, want it to mention ccr [-g]", out)
	}
}

func TestServeAndOpenTranscriptOpenBrowserFails(t *testing.T) {
	setupFixtureSession(t, "77777777-7777-7777-7777-777777777777", "hello")
	// An opener that exists but fails is a real error, unlike having no
	// opener at all.
	t.Setenv("BROWSER", "myopener")
	withStartCommandStubErr(t, errors.New("exec: no such file"))

	url, opened, err := serveAndOpenTranscript("77777777-7777-7777-7777-777777777777", false)
	if url == "" {
		t.Error("expected a URL even though opening the browser failed")
	}
	if opened {
		t.Error("opened = true, want false when the opener failed")
	}
	if err == nil {
		t.Fatal("expected an error when the opener fails")
	}
	if !strings.Contains(err.Error(), "failed to open browser") {
		t.Errorf("err = %v, want it to mention failing to open the browser", err)
	}
}

// On a machine with no browser to open — a server, typically — v still has
// something useful to do: serve the transcript and hand back its URL.
func TestServeAndOpenTranscriptWithoutAnyOpenerIsNotAnError(t *testing.T) {
	setupFixtureSession(t, "77777777-7777-7777-7777-777777777777", "hello")
	t.Setenv("BROWSER", "")
	// Without this, running on a real Mac would actually shell out to
	// `open <url>` and succeed, falsifying this test's premise.
	withFallbackOpenerStub(t, nil)

	url, opened, err := serveAndOpenTranscript("77777777-7777-7777-7777-777777777777", false)
	if err != nil {
		t.Fatalf("serveAndOpenTranscript: %v", err)
	}
	if opened {
		t.Error("opened = true, want false when there is no opener")
	}
	if !strings.HasSuffix(url, "/77777777-7777-7777-7777-777777777777") {
		t.Errorf("url = %q, want the session's transcript URL", url)
	}
}

// -n means "do not even try", so the browser must not be started even
// where an opener is perfectly available.
func TestServeAndOpenTranscriptNoBrowserSkipsTheOpener(t *testing.T) {
	setupFixtureSession(t, "77777777-7777-7777-7777-777777777777", "hello")
	t.Setenv("BROWSER", "myopener")
	captured := withStartCommandStub(t)

	url, opened, err := serveAndOpenTranscript("77777777-7777-7777-7777-777777777777", true)
	if err != nil {
		t.Fatalf("serveAndOpenTranscript: %v", err)
	}
	if opened {
		t.Error("opened = true, want false with noBrowser set")
	}
	if len(*captured) > 0 {
		t.Errorf("ran %v, want no opener to be started", *captured)
	}
	if !strings.HasSuffix(url, "/77777777-7777-7777-7777-777777777777") {
		t.Errorf("url = %q, want the session's transcript URL", url)
	}
}

func TestEnvIsTrue(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"", false}, {"0", false}, {"false", false}, {"no", false},
		{"off", false}, {"OFF", false}, {" 0 ", false},
		{"1", true}, {"true", true}, {"yes", true}, {"anything", true},
	} {
		if got := envIsTrue(c.in); got != c.want {
			t.Errorf("envIsTrue(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
