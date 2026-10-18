package ccr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fileConfig is the content of ccr's own config file.
type fileConfig struct {
	// ConfigDirs are extra CLAUDE_CONFIG_DIR values to list sessions from,
	// for people who keep separate Claude Code profiles (work / personal).
	ConfigDirs []string `json:"config_dirs"`
	// History configures the prompt/answer history that `ccr hook` records
	// and `ccr search` searches.
	History historyConfig `json:"history"`
}

// historyConfig is the "history" section of the config file.
type historyConfig struct {
	// DB is the sqlite database the history lives in; see historyDBPath.
	DB string `json:"db"`
	// Embedding turns on vector search. Without it the history is still
	// recorded and searchable by keyword.
	Embedding *embeddingConfig `json:"embedding"`
}

// embeddingConfig names an OpenAI-compatible embeddings endpoint
// (POST {"model", "input"} -> {"data": [{"embedding"}]}), which Ollama,
// LM Studio, llama.cpp, OpenAI and Voyage AI all serve.
type embeddingConfig struct {
	URL   string `json:"url"`
	Model string `json:"model"`
	// APIKeyEnv names the environment variable holding the API key, sent
	// as a bearer token. Empty for a local server that needs none.
	APIKeyEnv string `json:"api_key_env"`
	// Dimensions is passed on as "dimensions" for models that can shorten
	// their vectors; 0 leaves the model's own size.
	Dimensions int `json:"dimensions"`
	// QueryPrefix and DocumentPrefix are prepended to search queries and
	// stored texts, for models trained with them (e5's "query: " and
	// "passage: ", for instance).
	QueryPrefix    string `json:"query_prefix"`
	DocumentPrefix string `json:"document_prefix"`
}

// ccrConfigPath returns where ccr reads its config file from: $CCR_CONFIG
// if set, otherwise ${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json.
// os.UserConfigDir is not used because on macOS it points at
// ~/Library/Application Support, an odd place for a CLI's dotfile.
func ccrConfigPath() string {
	if v := os.Getenv("CCR_CONFIG"); v != "" {
		return v
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ccr", "config.json")
}

// loadFileConfig reads the config file at path. A missing file is not an
// error: the config file is optional.
func loadFileConfig(path string) (fileConfig, error) {
	var cfg fileConfig
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// expandPath expands $VARS and a leading ~ in a config file path.
func expandPath(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return p
}

// configDirs returns every Claude Code config directory ccr lists sessions
// from: the one in effect for this shell (configDir) first, then each
// config_dirs entry of the config file, without duplicates (two spellings
// of the same directory, e.g. through a symlink, count once).
func configDirs() ([]string, error) {
	cfg, err := loadFileConfig(ccrConfigPath())
	if err != nil {
		return []string{configDir()}, err
	}

	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = filepath.Clean(dir)
		key := dir
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			key = real
		}
		if seen[key] {
			return
		}
		seen[key] = true
		dirs = append(dirs, dir)
	}
	add(configDir())
	for _, d := range cfg.ConfigDirs {
		d = expandPath(d)
		if d == "" {
			continue
		}
		if !filepath.IsAbs(d) {
			return []string{configDir()}, fmt.Errorf("%s: config_dirs entry must be an absolute path: %q", ccrConfigPath(), d)
		}
		add(d)
	}
	return dirs, nil
}

// useAllConfigDirs is set by -A: session lookups then search every config
// directory of the config file, not just the current one.
var useAllConfigDirs bool

// lookupSessionFile finds sessionID's jsonl in the current config
// directory, or with -A in any of them, the first one holding it winning.
func lookupSessionFile(sessionID string) (string, error) {
	dirs := []string{configDir()}
	if useAllConfigDirs {
		dirs, _ = configDirs()
	}
	for _, dir := range dirs {
		if path, err := findSessionFile(filepath.Join(dir, "projects"), sessionID); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("session not found: %s", sessionID)
}

// resumeEnv returns environ adjusted so that claude, started with it, uses
// dir as its config directory. The default directory is selected by
// leaving CLAUDE_CONFIG_DIR unset rather than pointing it at ~/.claude,
// because claude keeps its global settings in ~/.claude.json only when the
// variable is unset; with it set they move to $CLAUDE_CONFIG_DIR/.claude.json.
func resumeEnv(environ []string, dir string) []string {
	if dir == "" || sameDir(dir, configDir()) {
		return environ
	}
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			out = append(out, kv)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && sameDir(dir, filepath.Join(home, ".claude")) {
		return out
	}
	return append(out, "CLAUDE_CONFIG_DIR="+dir)
}

// sameDir reports whether a and b name the same directory, looking through
// symlinks when both exist.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
