package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionEntry struct {
	id      string
	modTime time.Time
	cwd     string
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	timestamps := fs.Bool("timestamps", false, "show timestamp alongside each session id")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := collectSessions(projectsDir())
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})

	for _, e := range entries {
		basename := ""
		if e.cwd != "" {
			basename = filepath.Base(e.cwd)
		}
		if *timestamps {
			fmt.Printf("%s %s %s\n", e.modTime.Format("2006-01-02 15:04"), e.id, basename)
		} else {
			fmt.Printf("%s %s\n", e.id, basename)
		}
	}
	return nil
}

// collectSessions walks ${CLAUDE_CONFIG_DIR}/projects/<dir>/<session_id>.jsonl
// and returns one entry per session file found across every project directory.
func collectSessions(root string) ([]sessionEntry, error) {
	projectDirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var entries []sessionEntry
	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		sub, err := collectSessionsInDir(filepath.Join(root, pd.Name()))
		if err != nil {
			continue
		}
		entries = append(entries, sub...)
	}
	return entries, nil
}

// collectSessionsInDir returns one entry per <session_id>.jsonl file found
// directly inside dir.
func collectSessionsInDir(dir string) ([]sessionEntry, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []sessionEntry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		cwd, _ := readSessionCwd(filepath.Join(dir, f.Name()))
		entries = append(entries, sessionEntry{
			id:      strings.TrimSuffix(f.Name(), ".jsonl"),
			modTime: info.ModTime(),
			cwd:     cwd,
		})
	}
	return entries, nil
}
