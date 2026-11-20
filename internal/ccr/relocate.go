package ccr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// relocatePrompt asks where to resume a session whose directory no longer
// exists (a scratch folder that has since been deleted, typically). Once a
// folder is given, the session's files are moved to that folder's project
// directory, so claude --resume run there finds it.
type relocatePrompt struct {
	entry sessionEntry
	// path is the session's jsonl, which is what gets moved.
	path  string
	input string
	err   error
}

// newRelocatePrompt starts the prompt on ccr's own working directory, the
// folder the session is most likely meant to continue in.
func newRelocatePrompt(entry sessionEntry, path string) *relocatePrompt {
	input, _ := os.Getwd()
	return &relocatePrompt{entry: entry, path: path, input: input}
}

// key applies one keypress. done says the prompt is over: with ok the
// session has been moved and entry, now pointing at its new directory, is
// to be resumed; without ok the user gave up.
func (r *relocatePrompt) key(key, text string) (entry sessionEntry, done, ok bool) {
	switch key {
	case "esc", "ctrl+c":
		return sessionEntry{}, true, false
	case "enter":
		dir, err := resolveDir(r.input)
		if err == nil && r.entry.pid != 0 {
			err = fmt.Errorf("the session is running (pid %d)", r.entry.pid)
		}
		if err == nil {
			err = relocateSession(r.path, dir)
		}
		if err != nil {
			r.err = err
			return sessionEntry{}, false, false
		}
		entry = r.entry
		entry.cwd = dir
		return entry, true, true
	case "tab":
		r.input = completeDir(r.input)
	case "backspace":
		if in := []rune(r.input); len(in) > 0 {
			r.input = string(in[:len(in)-1])
		}
	case "ctrl+u":
		r.input = ""
	case "ctrl+w":
		r.input = dropLastPathElement(r.input)
	default:
		if text == "" {
			return sessionEntry{}, false, false
		}
		r.input += text
	}
	r.err = nil
	return sessionEntry{}, false, false
}

// prompt is the footer line while the prompt is open. When it does not
// fit, the start of the typed path gives way first: its end is what is
// being typed.
func (r *relocatePrompt) prompt(width int) string {
	label := r.entry.cwd + " is gone; resume in (tab: complete, esc: cancel): "
	if r.err != nil {
		label = "error: " + r.err.Error() + "; resume in: "
	}
	line := label + r.input + "▏"
	if width <= 0 || runewidth.StringWidth(line) <= width {
		return line
	}
	label = "resume in: "
	if r.err != nil {
		label = "error: " + r.err.Error() + ": "
	}
	if line := label + r.input + "▏"; runewidth.StringWidth(line) <= width {
		return line
	}
	room := width - runewidth.StringWidth(label) - 1 // the "…"
	if room < 8 {
		return truncate(label+r.input+"▏", width)
	}
	in := []rune(r.input + "▏")
	for runewidth.StringWidth(string(in)) > room && len(in) > 1 {
		in = in[1:]
	}
	return label + "…" + string(in)
}

// resolveDir turns what was typed into the directory to resume in: ~ and
// $VARS expanded, relative to ccr's own directory, and with symlinks
// resolved, since claude names the project after the physical path it
// finds itself in.
func resolveDir(input string) (string, error) {
	p := expandPath(strings.TrimSpace(input))
	if p == "" {
		return "", errors.New("no directory given")
	}
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		p = filepath.Join(wd, p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", p)
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p), nil
}

// relocateSession moves the session whose jsonl is path to the project
// directory claude uses for cwd, in the same config directory:
// <id>.jsonl, plus the <id>/ directory beside it holding its sub agents'
// transcripts and saved tool results. The old project directory is removed
// if that leaves it empty. Nothing is overwritten: if the destination
// already holds the session, it is an error.
func relocateSession(path, cwd string) error {
	projectDir := filepath.Dir(path)
	destDir := filepath.Join(filepath.Dir(projectDir), encodeProjectDir(cwd))
	if destDir == projectDir {
		return nil // the new folder's name encodes the same; nothing to move
	}
	name := filepath.Base(path)
	id := strings.TrimSuffix(name, ".jsonl")
	srcData, destData := filepath.Join(projectDir, id), filepath.Join(destDir, id)
	destFile := filepath.Join(destDir, name)

	for _, p := range []string{destFile, destData} {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("%s already exists", p)
		}
	}
	_, err := os.Lstat(srcData)
	hasData := err == nil

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	if err := os.Rename(path, destFile); err != nil {
		return err
	}
	if hasData {
		if err := os.Rename(srcData, destData); err != nil {
			// put the transcript back with its data rather than split them
			if back := os.Rename(destFile, path); back != nil {
				return fmt.Errorf("%w (and moving %s back failed: %v)", err, destFile, back)
			}
			return err
		}
	}
	_ = os.Remove(projectDir) // only succeeds when empty
	return nil
}

// completeDir completes the last element of input to the subdirectories it
// is the start of: all the way, with a trailing slash, when only one
// matches, otherwise as far as they agree. Hidden directories are offered
// only once a dot has been typed.
func completeDir(input string) string {
	if input == "~" {
		return "~/"
	}
	expanded := expandPath(input)
	dir, prefix := filepath.Split(expanded)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return input
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return input
	}
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	for !utf8.ValidString(common) { // a cut through a multibyte character
		common = common[:len(common)-1]
	}
	if common == "" {
		return input
	}
	if len(names) == 1 {
		common += string(filepath.Separator)
	}
	// When the last element was typed as is, input keeps the way it was
	// written (~ or $VARS before it stay). When the element itself came
	// from a $VAR, input has no part to keep the completion against, so
	// it gives way to the expanded path.
	tail := input[strings.LastIndexByte(input, filepath.Separator)+1:]
	if tail == prefix {
		return input[:len(input)-len(prefix)] + common
	}
	return expanded[:len(expanded)-len(prefix)] + common
}

// dropLastPathElement removes the last element of a typed path, keeping
// the slash before it, the way ctrl+w does in a shell.
func dropLastPathElement(input string) string {
	trimmed := strings.TrimRight(input, string(filepath.Separator))
	if i := strings.LastIndexByte(trimmed, filepath.Separator); i >= 0 {
		return trimmed[:i+1]
	}
	return ""
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
