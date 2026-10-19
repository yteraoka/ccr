package ccr

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// captureStdout runs fn and returns what it wrote to stdout.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	errc := make(chan error, 1)
	go func() { errc <- fn(); w.Close() }() //nolint:errcheck
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	os.Stdout = orig
	if err := <-errc; err != nil {
		t.Fatalf("%v", err)
	}
	return b.String()
}

func TestSearchJSONExcludesSession(t *testing.T) {
	t.Chdir(t.TempDir())
	historyFixture(t,
		historyMessage{sessionID: "old", role: "user", cwd: "/p", text: "hook の設定を直したい"},
		historyMessage{sessionID: "old", role: "assistant", cwd: "/p", text: "hook の設定を直しました <b>"},
		historyMessage{sessionID: "now", role: "user", cwd: "/p", text: "前に hook の設定をどうしたっけ"},
	)
	out := captureStdout(t, func() error {
		return RunSearch([]string{"-g", "-json", "-exclude-session", "now", "hook", "設定"})
	})
	var got []searchResultJSON
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var r searchResultJSON
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want the 2 from the other session: %s", len(got), out)
	}
	for _, r := range got {
		if r.SessionID != "old" || r.ID == 0 || r.Cwd != "/p" || r.Length == 0 || r.Time == "" {
			t.Errorf("result %+v", r)
		}
	}
	if !strings.Contains(out, "<b>") {
		t.Error("JSON escapes HTML in the snippet")
	}
}

func TestRunShow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	var msgs []historyMessage
	for i := 0; i < 7; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, historyMessage{sessionID: "s", role: role, cwd: "/p", text: "message " + string(rune('a'+i)), createdAt: t0.Add(time.Duration(i) * time.Minute)})
	}
	historyFixture(t, msgs...)

	whole := captureStdout(t, func() error { return RunShow([]string{"s"}) })
	if !strings.HasPrefix(whole, "session s  /p\n") || strings.Count(whole, "\n--- #") != 7 {
		t.Errorf("ccr show s =\n%s", whole)
	}
	if strings.Index(whole, "message a") > strings.Index(whole, "message g") {
		t.Error("messages out of order")
	}

	around := captureStdout(t, func() error { return RunShow([]string{"-around", "4", "s", "-context", "1"}) })
	if strings.Count(around, "\n--- #") != 3 || !strings.Contains(around, "#4 claude") || !strings.Contains(around, "<<<") ||
		strings.Contains(around, "message b") || !strings.Contains(around, "message e") {
		t.Errorf("ccr show -around 4 -context 1 =\n%s", around)
	}

	js := captureStdout(t, func() error { return RunShow([]string{"-json", "-around", "1", "s"}) })
	var first showMessageJSON
	if err := json.Unmarshal([]byte(strings.SplitN(js, "\n", 2)[0]), &first); err != nil || first.Text != "message a" || first.Role != "user" {
		t.Errorf("first JSON line = %+v (%v)", first, err)
	}

	if err := RunShow([]string{"-around", "99", "s"}); err == nil {
		t.Error("-around a message of another session: want an error")
	}
	if err := RunShow([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "nothing recorded") {
		t.Errorf("unknown session: err = %v", err)
	}
}

func TestHistorySkill(t *testing.T) {
	skill := historySkill("/opt/bin/ccr")
	if !isHistorySkill([]byte(skill)) {
		t.Error("the skill is not recognized as ccr's own")
	}
	for _, want := range []string{
		"allowed-tools: Bash(/opt/bin/ccr search *) Bash(/opt/bin/ccr show *)",
		"/opt/bin/ccr search -json -exclude-session ${CLAUDE_SESSION_ID} <words>",
		"/opt/bin/ccr show -around <id> <session_id>",
		"when_to_use:",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("skill lacks %q", want)
		}
	}
	if strings.Contains(skill, "{{ccr}}") {
		t.Error("skill keeps a placeholder")
	}
}

func TestHistorySkillPaths(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	sys := t.TempDir()
	orig := systemSettingsDir
	systemSettingsDir = func() string { return sys }
	t.Cleanup(func() { systemSettingsDir = orig })
	cwd, _ := os.Getwd()

	for scope, want := range map[string]string{
		"user":    "/cfg/skills/ccr-history/SKILL.md",
		"project": filepath.Join(cwd, ".claude", "skills", "ccr-history", "SKILL.md"),
		"system":  filepath.Join(sys, ".claude", "skills", "ccr-history", "SKILL.md"),
	} {
		got, err := historySkillPaths(scope, false)
		if err != nil || !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("historySkillPaths(%s) = %q, %v; want %q", scope, got, err, want)
		}
	}
	if _, err := historySkillPaths("local", false); err == nil {
		t.Error("local scope: want an error (skills have none)")
	}
	if _, err := historySkillPaths("project", true); err == nil {
		t.Error("-A with project scope: want an error")
	}
}

func TestRunSkillCommand(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	path := filepath.Join(cfg, "skills", "ccr-history", "SKILL.md")

	if err := RunSkillCommand([]string{"install", "-command", "ccr"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != historySkill("ccr") {
		t.Fatalf("installed skill = %q, %v", data, err)
	}
	// again: nothing to do; with another command: updated in place
	if err := RunSkillCommand([]string{"install", "-command", "ccr"}); err != nil {
		t.Fatal(err)
	}
	if err := RunSkillCommand([]string{"install", "-command", "/x/ccr"}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "Bash(/x/ccr search *)") {
		t.Errorf("skill not updated:\n%s", data)
	}

	if err := RunSkillCommand([]string{"uninstall"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Error("uninstall left the skill's directory")
	}

	// someone else's skill of the same name is left alone
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: mine\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunSkillCommand([]string{"install"}); err == nil {
		t.Error("install over a foreign SKILL.md: want an error")
	}
	if err := RunSkillCommand([]string{"uninstall"}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "mine") {
		t.Error("uninstall removed a foreign SKILL.md")
	}
}
