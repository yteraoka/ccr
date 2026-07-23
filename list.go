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
		if *timestamps {
			fmt.Printf("%s %s\n", e.modTime.Format("2006-01-02 15:04"), e.id)
		} else {
			fmt.Println(e.id)
		}
	}
	return nil
}

// collectSessions walks ${CLAUDE_CONFIG_DIR}/projects/<dir>/<session_id>.jsonl
// and returns one entry per session file found.
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
		projPath := filepath.Join(root, pd.Name())
		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			entries = append(entries, sessionEntry{
				id:      strings.TrimSuffix(f.Name(), ".jsonl"),
				modTime: info.ModTime(),
			})
		}
	}
	return entries, nil
}
