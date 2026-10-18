package ccr

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// writeCCRConfig points CCR_CONFIG at a fresh config file with content.
func writeCCRConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCR_CONFIG", path)
}

func TestCCRConfigPath(t *testing.T) {
	t.Setenv("CCR_CONFIG", "/explicit/config.json")
	if got := ccrConfigPath(); got != "/explicit/config.json" {
		t.Errorf("ccrConfigPath() = %q, want CCR_CONFIG", got)
	}

	t.Setenv("CCR_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, want := ccrConfigPath(), filepath.Join("/xdg", "ccr", "config.json"); got != want {
		t.Errorf("ccrConfigPath() = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}
	if got, want := ccrConfigPath(), filepath.Join(home, ".config", "ccr", "config.json"); got != want {
		t.Errorf("ccrConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigDirsWithoutConfigFile(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/env/claude")
	dirs, err := configDirs()
	if err != nil {
		t.Fatalf("configDirs: %v", err)
	}
	if want := []string{"/env/claude"}; !reflect.DeepEqual(dirs, want) {
		t.Errorf("configDirs() = %q, want %q", dirs, want)
	}
}

func TestConfigDirsMergesConfigFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/env/claude")
	t.Setenv("WORK_DIR", "/work")
	writeCCRConfig(t, `{"config_dirs": ["~/.claude-personal", "$WORK_DIR/claude", "/env/claude/", ""]}`)

	dirs, err := configDirs()
	if err != nil {
		t.Fatalf("configDirs: %v", err)
	}
	// the shell's own dir comes first, and its repeat in the file is dropped
	want := []string{"/env/claude", filepath.Join(home, ".claude-personal"), "/work/claude"}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("configDirs() = %q, want %q", dirs, want)
	}
}

func TestConfigDirsDedupesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", real)
	writeCCRConfig(t, `{"config_dirs": [`+quoteJSON(link)+`]}`)

	dirs, err := configDirs()
	if err != nil {
		t.Fatalf("configDirs: %v", err)
	}
	if want := []string{real}; !reflect.DeepEqual(dirs, want) {
		t.Errorf("configDirs() = %q, want %q", dirs, want)
	}
}

func TestConfigDirsRejectsBadConfig(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/env/claude")
	for _, content := range []string{`{"config_dirs": `, `{"config_dirs": ["relative/dir"]}`} {
		writeCCRConfig(t, content)
		if _, err := configDirs(); err == nil {
			t.Errorf("configDirs() with %q: want error", content)
		}
	}
}

// makeSession lays down <configDir>/projects/<encoded cwd>/<id>.jsonl.
func makeSession(t *testing.T, configDir, cwd, id string) {
	t.Helper()
	dir := filepath.Join(configDir, "projects", encodeProjectDir(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, dir, id, cwd)
}

func quoteJSON(s string) string {
	return `"` + s + `"`
}

func TestSessionsForPickerAcrossConfigDirs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	a, b, empty := t.TempDir(), t.TempDir(), t.TempDir()
	makeSession(t, a, cwd, "session-a")
	makeSession(t, a, "/elsewhere", "session-a2")
	makeSession(t, b, cwd, "session-b")
	dirs := []string{a, b, empty, filepath.Join(t.TempDir(), "missing")}

	ids := func(entries []sessionEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.id+"@"+e.configDir)
		}
		slices.Sort(out)
		return out
	}

	local, err := sessionsForPicker(dirs, false)
	if err != nil {
		t.Fatalf("sessionsForPicker(local): %v", err)
	}
	want := []string{"session-a@" + a, "session-b@" + b}
	slices.Sort(want)
	if got := ids(local); !reflect.DeepEqual(got, want) {
		t.Errorf("local = %q, want %q", got, want)
	}

	global, err := sessionsForPicker(dirs, true)
	if err != nil {
		t.Fatalf("sessionsForPicker(global): %v", err)
	}
	want = []string{"session-a@" + a, "session-a2@" + a, "session-b@" + b}
	slices.Sort(want)
	if got := ids(global); !reflect.DeepEqual(got, want) {
		t.Errorf("global = %q, want %q", got, want)
	}
}

func TestLookupSessionFileSearchesEveryConfigDir(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	makeSession(t, b, "/tmp/proj", "session-b")
	t.Setenv("CLAUDE_CONFIG_DIR", a)
	writeCCRConfig(t, `{"config_dirs": [`+quoteJSON(b)+`]}`)

	if _, err := lookupSessionFile("session-b"); err == nil {
		t.Error("lookupSessionFile without -A: want only the current config dir searched")
	}
	useAllConfigDirs = true
	t.Cleanup(func() { useAllConfigDirs = false })

	got, err := lookupSessionFile("session-b")
	if err != nil {
		t.Fatalf("lookupSessionFile: %v", err)
	}
	if want := filepath.Join(b, "projects", encodeProjectDir("/tmp/proj"), "session-b.jsonl"); got != want {
		t.Errorf("lookupSessionFile = %q, want %q", got, want)
	}
	if _, err := lookupSessionFile("nope"); err == nil {
		t.Error("lookupSessionFile(nope): want error")
	}
}

func TestResumeEnv(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}
	defaultDir := filepath.Join(home, ".claude")

	cases := []struct {
		name    string
		current string // CLAUDE_CONFIG_DIR of the shell, "" for unset
		dir     string
		want    []string
	}{
		{"same as shell", "/work", "/work", []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/work"}},
		{"other dir", "/work", "/personal", []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/personal"}},
		{"from unset", "", "/work", []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/work"}},
		{"back to default", "/work", defaultDir, []string{"PATH=/bin"}},
		{"default stays unset", "", defaultDir, []string{"PATH=/bin"}},
		{"unknown dir", "/work", "", []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/work"}},
	}
	for _, c := range cases {
		t.Setenv("CLAUDE_CONFIG_DIR", c.current)
		environ := []string{"PATH=/bin"}
		if c.current != "" {
			environ = append(environ, "CLAUDE_CONFIG_DIR="+c.current)
		}
		if got := resumeEnv(environ, c.dir); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: resumeEnv = %q, want %q", c.name, got, c.want)
		}
	}
}
