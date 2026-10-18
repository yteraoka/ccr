package ccr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsHistoryHook(t *testing.T) {
	for cmd, want := range map[string]bool{
		"ccr hook":                        true,
		"/usr/local/bin/ccr hook":         true,
		`"C:\Program Files\ccr.exe" hook`: true,
		"ccr search hook":                 false,
		"other hook":                      false,
		"ccr":                             false,
		"~/bin/check.sh":                  false,
	} {
		if got := isHistoryHook(cmd); got != want {
			t.Errorf("isHistoryHook(%q) = %v, want %v", cmd, got, want)
		}
	}
}

const settingsWithOtherHooks = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "~/.claude/stop-check.sh"}]}
    ],
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "guard"}]}
    ]
  },
  "permissions": {"allow": ["Bash(ls:*)"]}
}`

func hookCommands(t *testing.T, settings []byte, event string) []string {
	t.Helper()
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
				Async   bool   `json:"async"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		t.Fatalf("settings are not valid JSON: %v\n%s", err, settings)
	}
	var out []string
	for _, g := range s.Hooks[event] {
		for _, h := range g.Hooks {
			c := h.Command
			if h.Async {
				c += " (async)"
			}
			out = append(out, c)
		}
	}
	return out
}

func TestInstallHistoryHooks(t *testing.T) {
	out, added, err := installHistoryHooks([]byte(settingsWithOtherHooks), "ccr hook")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"UserPromptSubmit", "Stop"}; !reflect.DeepEqual(added, want) {
		t.Errorf("added = %v, want %v", added, want)
	}
	if got := hookCommands(t, out, "Stop"); !reflect.DeepEqual(got, []string{"~/.claude/stop-check.sh", "ccr hook (async)"}) {
		t.Errorf("Stop hooks = %q", got)
	}
	if got := hookCommands(t, out, "UserPromptSubmit"); !reflect.DeepEqual(got, []string{"ccr hook (async)"}) {
		t.Errorf("UserPromptSubmit hooks = %q", got)
	}
	if got := hookCommands(t, out, "PreToolUse"); !reflect.DeepEqual(got, []string{"guard"}) {
		t.Errorf("PreToolUse hooks = %q", got)
	}
	// the rest of the file keeps its keys, in their order
	s := string(out)
	model, hooks, perms := strings.Index(s, `"model"`), strings.Index(s, `"hooks"`), strings.Index(s, `"permissions"`)
	if model > hooks || hooks > perms {
		t.Errorf("top-level key order changed:\n%s", s)
	}
	if !strings.Contains(s, `"matcher": "Bash"`) {
		t.Errorf("other hook groups changed:\n%s", s)
	}

	// running it again changes nothing
	again, added, err := installHistoryHooks(out, "/other/ccr hook")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 || string(again) != string(out) {
		t.Errorf("second install added %v:\n%s", added, again)
	}
}

func TestInstallIntoEmptySettings(t *testing.T) {
	for _, in := range []string{"", "  \n", "{}"} {
		out, added, err := installHistoryHooks([]byte(in), "ccr hook")
		if err != nil || len(added) != 2 {
			t.Fatalf("install into %q: added %v, err %v", in, added, err)
		}
		want := `{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "ccr hook",
            "async": true
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "ccr hook",
            "async": true
          }
        ]
      }
    ]
  }
}
`
		if string(out) != want {
			t.Errorf("install into %q =\n%s", in, out)
		}
	}
	if _, _, err := installHistoryHooks([]byte(`[1]`), "ccr hook"); err == nil {
		t.Error("install into a non-object: want error")
	}
	if _, _, err := installHistoryHooks([]byte(`{"hooks": {"Stop": {}}}`), "ccr hook"); err == nil {
		t.Error("install into a malformed hooks section: want error")
	}
}

func TestUninstallHistoryHooks(t *testing.T) {
	installed, _, err := installHistoryHooks([]byte(settingsWithOtherHooks), "/usr/local/bin/ccr hook")
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := uninstallHistoryHooks(installed)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Stop", "UserPromptSubmit"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if got := hookCommands(t, out, "Stop"); !reflect.DeepEqual(got, []string{"~/.claude/stop-check.sh"}) {
		t.Errorf("Stop hooks after uninstall = %q", got)
	}
	if strings.Contains(string(out), "UserPromptSubmit") {
		t.Errorf("the emptied event was kept:\n%s", out)
	}

	// a group shared with another hook keeps that hook
	shared := `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "ccr hook"}, {"type": "command", "command": "notify"}]}]}}`
	out, _, err = uninstallHistoryHooks([]byte(shared))
	if err != nil {
		t.Fatal(err)
	}
	if got := hookCommands(t, out, "Stop"); !reflect.DeepEqual(got, []string{"notify"}) {
		t.Errorf("shared group after uninstall = %q", got)
	}

	// nothing of ours: nothing removed; only ours: the hooks key goes
	if _, removed, _ := uninstallHistoryHooks([]byte(settingsWithOtherHooks)); len(removed) != 0 {
		t.Errorf("removed %v from settings without ccr hooks", removed)
	}
	onlyOurs, _, _ := installHistoryHooks(nil, "ccr hook")
	out, _, _ = uninstallHistoryHooks(onlyOurs)
	if !isEmptySettings(out) {
		t.Errorf("uninstall of only ccr hooks left %s", out)
	}
}

func TestHookSettingsPaths(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	sys := t.TempDir()
	orig := systemSettingsDir
	systemSettingsDir = func() string { return sys }
	t.Cleanup(func() { systemSettingsDir = orig })
	cwd, _ := os.Getwd()

	cases := []struct {
		scope string
		all   bool
		want  []string
	}{
		{"user", false, []string{"/cfg/settings.json"}},
		{"project", false, []string{filepath.Join(cwd, ".claude", "settings.json")}},
		{"local", false, []string{filepath.Join(cwd, ".claude", "settings.local.json")}},
		{"system", false, []string{filepath.Join(sys, "managed-settings.d", "ccr.json")}},
	}
	for _, c := range cases {
		got, err := hookSettingsPaths(c.scope, c.all)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("hookSettingsPaths(%q) = %q, %v; want %q", c.scope, got, err, c.want)
		}
	}
	writeCCRConfig(t, `{"config_dirs": ["/work"]}`)
	if got, _ := hookSettingsPaths("user", true); !reflect.DeepEqual(got, []string{"/cfg/settings.json", "/work/settings.json"}) {
		t.Errorf("-A user paths = %q", got)
	}
	for _, scope := range []string{"project", "local", "system"} {
		if _, err := hookSettingsPaths(scope, true); err == nil {
			t.Errorf("-A with %s scope: want error", scope)
		}
	}
	if _, err := hookSettingsPaths("managed", false); err == nil {
		t.Error("unknown scope: want error")
	}
}

func TestRunHookCommandWritesAndRemoves(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	settings := filepath.Join(cfg, "settings.json")
	if err := os.WriteFile(settings, []byte(settingsWithOtherHooks), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RunHookCommand([]string{"install", "-command", "ccr hook"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(settings)
	if got := hookCommands(t, data, "UserPromptSubmit"); len(got) != 1 {
		t.Errorf("UserPromptSubmit hooks after install = %q", got)
	}
	if info, _ := os.Stat(settings); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want the file's own 0600 kept", info.Mode().Perm())
	}

	if err := RunHookCommand([]string{"uninstall"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(settings)
	if strings.Contains(string(data), "ccr hook") {
		t.Errorf("uninstall left the hooks:\n%s", data)
	}

	// the system drop-in is ccr's alone, so uninstall removes the file
	sys := t.TempDir()
	orig := systemSettingsDir
	systemSettingsDir = func() string { return sys }
	t.Cleanup(func() { systemSettingsDir = orig })
	dropIn := filepath.Join(sys, "managed-settings.d", "ccr.json")
	if err := RunHookCommand([]string{"install", "-scope", "system", "-command", "/usr/local/bin/ccr hook"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropIn); err != nil {
		t.Fatalf("system install wrote no drop-in: %v", err)
	}
	if err := RunHookCommand([]string{"uninstall", "-scope", "system"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropIn); !os.IsNotExist(err) {
		t.Errorf("system uninstall left %s", dropIn)
	}

	if err := RunHookCommand([]string{"frobnicate"}); err == nil {
		t.Error("unknown hook command: want error")
	}
}
