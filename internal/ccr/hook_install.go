package ccr

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// historyHookEvents are the Claude Code hook events `ccr hook` records
// from.
var historyHookEvents = []string{"UserPromptSubmit", "Stop"}

// The scopes `ccr hook install` can write to, as Claude Code names its
// settings levels.
const (
	scopeUser    = "user"
	scopeProject = "project"
	scopeSystem  = "system"
)

// systemSettingsDir is where Claude Code reads managed (system-wide)
// settings on this OS. It is a variable so tests can point it elsewhere.
var systemSettingsDir = func() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	}
	return "/etc/claude-code"
}

// systemDropInName is ccr's own file in managed-settings.d. Claude Code
// merges every *.json there with managed-settings.json, so the hooks can
// be added and removed without touching a file an administrator owns.
const systemDropInName = "ccr.json"

// RunHookCommand implements `ccr hook install|uninstall`.
func RunHookCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ccr hook install|uninstall [-scope user|project|system]")
	}
	action := args[0]
	if action != "install" && action != "uninstall" {
		return fmt.Errorf("unknown hook command %q: want install or uninstall", action)
	}

	fs := flag.NewFlagSet("ccr hook "+action, flag.ExitOnError)
	scope := fs.String("scope", scopeUser, "where to write: user ($CLAUDE_CONFIG_DIR/settings.json), project (.claude/settings.json here) or system (managed settings, every user)")
	all := fs.Bool("A", false, "with -scope user, every config directory in the config file")
	command := fs.String("command", "", `the command to register (default: "ccr hook", or this binary's path if ccr is not on PATH)`)
	printOnly := fs.Bool("print", false, "print the resulting settings instead of writing them")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ccr hook %s [-scope user|project|system] [-A] [-print]", action)
		if action == "install" {
			fmt.Fprint(os.Stderr, " [-command cmd]")
		}
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	paths, err := hookSettingsPaths(*scope, *all)
	if err != nil {
		return err
	}
	cmd := *command
	if cmd == "" && action == "install" {
		cmd = defaultHookCommand()
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		exists := err == nil

		var out []byte
		var changed []string
		if action == "install" {
			out, changed, err = installHistoryHooks(data, cmd)
		} else {
			out, changed, err = uninstallHistoryHooks(data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		if *printOnly {
			fmt.Fprintf(os.Stderr, "# %s\n", path)
			if _, err := os.Stdout.Write(out); err != nil {
				return err
			}
			continue
		}
		switch {
		case len(changed) == 0 && action == "install":
			fmt.Fprintf(os.Stderr, "%s: already has the hooks\n", path)
			continue
		case len(changed) == 0:
			fmt.Fprintf(os.Stderr, "%s: no ccr hooks to remove\n", path)
			continue
		}
		if action == "uninstall" && *scope == scopeSystem && isEmptySettings(out) {
			// the drop-in is ccr's alone; leave nothing behind
			if err := os.Remove(path); err != nil {
				return permissionHint(err, *scope)
			}
		} else if err := writeFileAtomic(path, out, exists); err != nil {
			return permissionHint(err, *scope)
		}
		if action == "install" {
			fmt.Fprintf(os.Stderr, "%s: added %q for %s\n", path, cmd, strings.Join(changed, " and "))
		} else {
			fmt.Fprintf(os.Stderr, "%s: removed ccr hook from %s\n", path, strings.Join(changed, " and "))
		}
	}
	return nil
}

// hookSettingsPaths returns the settings files a scope means.
func hookSettingsPaths(scope string, all bool) ([]string, error) {
	if all && scope != scopeUser {
		return nil, errors.New("-A goes with -scope user only")
	}
	switch scope {
	case scopeUser:
		dirs := []string{configDir()}
		if all {
			var err error
			if dirs, err = configDirs(); err != nil {
				return nil, err
			}
		}
		paths := make([]string, len(dirs))
		for i, d := range dirs {
			paths[i] = filepath.Join(d, "settings.json")
		}
		return paths, nil
	case scopeProject:
		// Claude Code reads project settings from the directory it is
		// started in, which is where this is meant to be run
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(cwd, ".claude", "settings.json")}, nil
	case scopeSystem:
		return []string{filepath.Join(systemSettingsDir(), "managed-settings.d", systemDropInName)}, nil
	}
	return nil, fmt.Errorf("-scope must be user, project or system, not %q", scope)
}

// defaultHookCommand is "ccr hook" when the ccr on PATH is this very
// binary, and otherwise this binary by its full path, so the hook still
// runs from a shell whose PATH does not have it.
func defaultHookCommand() string {
	self, err := os.Executable()
	if err != nil {
		return "ccr hook"
	}
	if onPath, err := exec.LookPath("ccr"); err == nil && sameFile(onPath, self) {
		return "ccr hook"
	}
	if strings.ContainsAny(self, " \t'\"") {
		return `"` + self + `" hook`
	}
	return self + " hook"
}

func sameFile(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// isHistoryHook reports whether a hook command runs `ccr hook`, however
// ccr is named: "ccr hook", "/usr/local/bin/ccr hook", `"C:\x\ccr.exe" hook`.
func isHistoryHook(command string) bool {
	fields := strings.Fields(command)
	if len(fields) < 2 || fields[len(fields)-1] != "hook" {
		return false
	}
	bin := strings.Trim(strings.Join(fields[:len(fields)-1], " "), `"'`)
	base := strings.ToLower(filepath.Base(filepath.ToSlash(strings.ReplaceAll(bin, `\`, "/"))))
	return base == "ccr" || base == "ccr.exe"
}

// hookEntry is the part of a hook definition used to recognize ours.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookGroup is one element of a hooks event array: a matcher and its hooks.
type hookGroup struct {
	Hooks []hookEntry `json:"hooks"`
}

// newHookGroup and newHook are what install adds, in the field order
// Claude Code's own examples use. async keeps the hook (and the embedding
// request it may make) from holding up the prompt.
type newHookGroup struct {
	Hooks []newHook `json:"hooks"`
}

type newHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async"`
}

// installHistoryHooks adds `command` (async) to settings for every event
// in historyHookEvents that does not run ccr hook already, returning the
// new settings and the events it added to. Everything else in settings,
// key order included, is kept.
func installHistoryHooks(settings []byte, command string) ([]byte, []string, error) {
	top, err := parseSettings(settings)
	if err != nil {
		return nil, nil, err
	}
	hooks, err := top.object("hooks")
	if err != nil {
		return nil, nil, err
	}

	var added []string
	for _, event := range historyHookEvents {
		groups, err := hooks.array(event)
		if err != nil {
			return nil, nil, err
		}
		if groupsRun(groups, isHistoryHook) {
			continue
		}
		group, err := json.Marshal(newHookGroup{Hooks: []newHook{{Type: "command", Command: command, Async: true}}})
		if err != nil {
			return nil, nil, err
		}
		if err := hooks.set(event, append(groups, group)); err != nil {
			return nil, nil, err
		}
		added = append(added, event)
	}
	if len(added) == 0 {
		out, err := formatSettings(top)
		return out, nil, err
	}
	if err := top.set("hooks", hooks); err != nil {
		return nil, nil, err
	}
	out, err := formatSettings(top)
	return out, added, err
}

// uninstallHistoryHooks removes every ccr hook command from settings,
// dropping the matcher groups, events and hooks object it leaves empty,
// and returns the events it removed from.
func uninstallHistoryHooks(settings []byte) ([]byte, []string, error) {
	top, err := parseSettings(settings)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := top.vals["hooks"]; !ok {
		out, err := formatSettings(top)
		return out, nil, err
	}
	hooks, err := top.object("hooks")
	if err != nil {
		return nil, nil, err
	}

	var removed []string
	for _, event := range append([]string(nil), hooks.keys...) {
		groups, err := hooks.array(event)
		if err != nil {
			return nil, nil, err
		}
		var kept []json.RawMessage
		touched := false
		for _, raw := range groups {
			out, changed, err := withoutHistoryHooks(raw)
			if err != nil {
				return nil, nil, err
			}
			touched = touched || changed
			if out != nil {
				kept = append(kept, out)
			}
		}
		if !touched {
			continue
		}
		removed = append(removed, event)
		if len(kept) == 0 {
			hooks.remove(event)
		} else if err := hooks.set(event, kept); err != nil {
			return nil, nil, err
		}
	}
	if len(hooks.keys) == 0 {
		top.remove("hooks")
	} else if err := top.set("hooks", hooks); err != nil {
		return nil, nil, err
	}
	out, err := formatSettings(top)
	return out, removed, err
}

// withoutHistoryHooks drops the ccr hook commands from one matcher group,
// returning nil when no hook is left in it, and whether it changed.
func withoutHistoryHooks(group json.RawMessage) (json.RawMessage, bool, error) {
	g, err := parseObject(group)
	if err != nil {
		return group, false, nil // not ours to judge
	}
	entries, err := g.array("hooks")
	if err != nil {
		return group, false, nil
	}
	var kept []json.RawMessage
	for _, raw := range entries {
		var h hookEntry
		if json.Unmarshal(raw, &h) == nil && isHistoryHook(h.Command) {
			continue
		}
		kept = append(kept, raw)
	}
	if len(kept) == len(entries) {
		return group, false, nil
	}
	if len(kept) == 0 {
		return nil, true, nil
	}
	if err := g.set("hooks", kept); err != nil {
		return nil, false, err
	}
	out, err := json.Marshal(g)
	return out, true, err
}

// groupsRun reports whether any hook in groups has a command match.
func groupsRun(groups []json.RawMessage, match func(string) bool) bool {
	for _, raw := range groups {
		var g hookGroup
		if json.Unmarshal(raw, &g) != nil {
			continue
		}
		for _, h := range g.Hooks {
			if match(h.Command) {
				return true
			}
		}
	}
	return false
}

func parseSettings(data []byte) (*jsonObject, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return newJSONObject(), nil
	}
	return parseObject(data)
}

func formatSettings(o *jsonObject) ([]byte, error) {
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

func isEmptySettings(data []byte) bool {
	return string(bytes.TrimSpace(data)) == "{}"
}

// writeFileAtomic replaces path with data through a temporary file in the
// same directory, so Claude Code never reads a half-written settings file.
// A new file gets mode 0644; an existing one keeps its mode.
func writeFileAtomic(path string, data []byte, exists bool) error {
	mode := os.FileMode(0o644)
	if exists {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func permissionHint(err error, scope string) error {
	if scope == scopeSystem && errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w (system settings need administrator rights: run it with sudo)", err)
	}
	return err
}

// jsonObject is a JSON object that remembers its key order, so a settings
// file can be edited without shuffling everything else in it.
type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func newJSONObject() *jsonObject {
	return &jsonObject{vals: make(map[string]json.RawMessage)}
}

func parseObject(data []byte) (*jsonObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	o := newJSONObject()
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		if _, dup := o.vals[key]; !dup {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = val
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON object")
	}
	return o, nil
}

// object returns the object under key, a new empty one if there is none.
func (o *jsonObject) object(key string) (*jsonObject, error) {
	raw, ok := o.vals[key]
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return newJSONObject(), nil
	}
	v, err := parseObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", key, err)
	}
	return v, nil
}

// array returns the elements of the array under key, none if there is none.
func (o *jsonObject) array(key string) ([]json.RawMessage, error) {
	raw, ok := o.vals[key]
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil, fmt.Errorf("%q is not an array: %w", key, err)
	}
	return elems, nil
}

// set stores v under key, keeping the key where it was or adding it last.
func (o *jsonObject) set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
	return nil
}

func (o *jsonObject) remove(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
