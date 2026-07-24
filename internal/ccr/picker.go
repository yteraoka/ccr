package ccr

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// PrintUsage prints command-line usage help to stderr.
func PrintUsage() {
	fmt.Fprintln(os.Stderr, `usage:
  ccr [-g]            interactive session picker (current project, or -g for every project)
  ccr -v               print version and exit`)
}

// RunPicker implements the default ccr action: an interactive picker that
// lists sessions (scoped to the current project unless -g is given), shows
// a live cwd/message preview of the highlighted one, and resumes into the
// selected session on Enter by cd-ing into its cwd and exec-ing
// `claude --resume <session_id>`.
func RunPicker(args []string) error {
	fs := flag.NewFlagSet("ccr", flag.ExitOnError)
	global := fs.Bool("g", false, "list sessions from every project, not just the current directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	entries, err := sessionsForPicker(*global)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no sessions found")
		return nil
	}
	entries = attachRunningPIDs(entries, loadRunningSessionPIDs(sessionsDir()))

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].timestamp.After(entries[j].timestamp)
	})

	p := tea.NewProgram(newPickerModel(entries), tea.WithAltScreen())
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

// sessionsForPicker returns every project's sessions when global is true,
// otherwise only those belonging to the current working directory's project.
func sessionsForPicker(global bool) ([]sessionEntry, error) {
	if global {
		return collectSessions(projectsDir())
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(projectsDir(), encodeProjectDir(cwd))
	entries, err := collectSessionsInDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return entries, err
}

// resumeSession moves into the session's original cwd and execs
// `claude --resume <sessionID>` in place of the current process, mirroring
// what shell `exec` does. If the cwd has a .envrc and direnv is on PATH,
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

	return syscall.Exec(argv0, args, os.Environ())
}

// serveAndOpenTranscript makes sessionID's jsonl viewable as a
// self-contained HTML transcript on the shared local HTTP server and
// opens it via $BROWSER. It returns the URL (valid even if opening the
// browser failed) and any error encountered.
func serveAndOpenTranscript(sessionID string) (string, error) {
	url, err := serveTranscriptSession(sessionID)
	if err != nil {
		return "", err
	}
	if err := openInBrowser(url); err != nil {
		return url, fmt.Errorf("started server but failed to open browser: %w", err)
	}
	return url, nil
}
