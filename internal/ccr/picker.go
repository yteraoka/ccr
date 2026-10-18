package ccr

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

// PrintUsage prints command-line usage help to stderr.
func PrintUsage() {
	fmt.Fprintln(os.Stderr, `usage:
  ccr [-g] [-A] [-n]   interactive session picker (current project, or -g for every project)
  ccr search [-g] [-l n] [-role user|assistant] [-mode hybrid|keyword|vector] <query>
                       search the prompts and answers recorded by ccr hook
  ccr import [-A] [-no-embed]
                       add the prompts and answers of existing transcripts to the history
  ccr hook install [-scope user|project|system] [-A] [-command cmd] [-print]
  ccr hook uninstall [-scope user|project|system] [-A] [-print]
                       register (or remove) ccr hook in Claude Code's settings
  ccr hook             record a prompt or answer (run from Claude Code's
                       UserPromptSubmit and Stop hooks; reads the hook JSON on stdin)
  ccr -v               print version and exit

options:
  -g                   list sessions from every project, not just the current directory
  -A                   list sessions from every config_dirs entry of the config file too,
                       not just the current CLAUDE_CONFIG_DIR
  -n, -no-browser      on v, print the transcript URL instead of opening a browser
                       (for machines with no browser; also settable with CCR_NO_BROWSER=1)

config file (${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json, or $CCR_CONFIG):
  {"config_dirs": ["~/.claude", "~/.claude-work"]}
                       Claude Code config directories -A lists sessions from`)
}

// RunPicker implements the default ccr action: an interactive picker that
// lists sessions (scoped to the current project unless -g is given), shows
// a live cwd/message preview of the highlighted one, and resumes into the
// selected session on Enter by cd-ing into its cwd and exec-ing
// `claude --resume <session_id>`.
func RunPicker(args []string) error {
	fs := flag.NewFlagSet("ccr", flag.ExitOnError)
	global := fs.Bool("g", false, "list sessions from every project, not just the current directory")
	allDirs := fs.Bool("A", false, "list sessions from every config directory in the config file")
	noBrowser := fs.Bool("no-browser", envIsTrue(os.Getenv("CCR_NO_BROWSER")), "on v, show the transcript URL instead of opening a browser")
	noBrowserShort := fs.Bool("n", false, "shorthand for -no-browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	dirs := []string{configDir()}
	if *allDirs {
		var err error
		if dirs, err = configDirs(); err != nil {
			return err
		}
		useAllConfigDirs = true
	}
	entries, err := sessionsForPicker(dirs, *global)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no sessions found")
		return nil
	}
	pids := make(map[string]int)
	for _, dir := range dirs {
		for id, pid := range loadRunningSessionPIDs(sessionsDir(dir)) {
			pids[id] = pid
		}
	}
	entries = attachRunningPIDs(entries, pids)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].timestamp.After(entries[j].timestamp)
	})

	model := newPickerModel(entries)
	model.noBrowser = *noBrowser || *noBrowserShort
	model.showConfigDir = len(dirs) > 1
	model.global = *global
	p := tea.NewProgram(model)
	res, err := p.Run()
	if err != nil {
		return err
	}

	m := res.(pickerModel)
	if m.selected.id == "" {
		return nil
	}
	return resumeSession(m.selected)
}

// sessionsForPicker returns, from every config directory in dirs, every
// project's sessions when global is true, otherwise only those belonging to
// the current working directory's project. A config directory without the
// project (or without any projects at all) just contributes nothing.
func sessionsForPicker(dirs []string, global bool) ([]sessionEntry, error) {
	cwd := ""
	if !global {
		var err error
		if cwd, err = os.Getwd(); err != nil {
			return nil, err
		}
	}

	var entries []sessionEntry
	for _, dir := range dirs {
		var sub []sessionEntry
		var err error
		if global {
			sub, err = collectSessions(filepath.Join(dir, "projects"))
		} else {
			sub, err = collectSessionsInDir(filepath.Join(dir, "projects", encodeProjectDir(cwd)))
		}
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for i := range sub {
			sub[i].configDir = dir
		}
		entries = append(entries, sub...)
	}
	return entries, nil
}

// resumeSession moves into the session's original cwd and execs
// `claude --resume <sessionID>` in place of the current process, mirroring
// what shell `exec` does. claude is pointed at the config directory the
// session was found in (see resumeEnv), so a session listed from another
// profile resumes in that profile. If the cwd has a .envrc and direnv is on PATH,
// claude is launched through `direnv exec` so its environment is loaded;
// direnv itself execs into claude, so no wrapper process is left behind.
func resumeSession(entry sessionEntry) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return err
	}
	if entry.cwd != "" {
		if err := os.Chdir(entry.cwd); err != nil {
			return err
		}
	}

	argv0, args := claudePath, []string{"claude", "--resume", entry.id}
	if entry.cwd != "" {
		if _, err := os.Stat(filepath.Join(entry.cwd, ".envrc")); err == nil {
			if direnvPath, err := exec.LookPath("direnv"); err == nil {
				argv0 = direnvPath
				args = []string{"direnv", "exec", entry.cwd, claudePath, "--resume", entry.id}
			}
		}
	}

	return syscall.Exec(argv0, args, resumeEnv(os.Environ(), entry.configDir))
}

// envIsTrue reports whether an environment variable's value asks for a
// boolean setting to be turned on. Anything set but obviously negative
// ("0", "false", "no", "off") counts as off, so CCR_NO_BROWSER=0 can turn
// the setting back off for one shell.
func envIsTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// serveAndOpenTranscript makes sessionID's jsonl viewable as a
// self-contained HTML transcript on the shared local HTTP server, and
// opens it via $BROWSER unless noBrowser is set or this machine has no
// opener at all. It returns the URL (valid even if the browser was not
// opened), whether a browser was actually opened, and any error.
//
// A machine with no opener is not an error: on a server there is no
// browser to open and the URL itself is the useful outcome, so the caller
// shows it instead of reporting a failure.
func serveAndOpenTranscript(sessionID string, noBrowser bool) (string, bool, error) {
	url, err := serveTranscriptSession(sessionID)
	if err != nil {
		return "", false, err
	}
	if noBrowser {
		return url, false, nil
	}
	if err := openInBrowser(url); err != nil {
		if errors.Is(err, errNoBrowserOpener) {
			return url, false, nil
		}
		return url, false, fmt.Errorf("started server but failed to open browser: %w", err)
	}
	return url, true, nil
}
