package ccr

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	serverPortRangeStart = 8000
	serverPortRangeEnd   = 8999
)

// transcriptServerReady/BaseURL/Err are only ever written from inside
// transcriptServerOnce.Do, and every read in this file happens from the
// picker's single-threaded Update loop (the only exception, the HTTP
// handler goroutine, never touches them), so plain reads are safe here
// without extra synchronization.
var (
	transcriptServerOnce    sync.Once
	transcriptServerBaseURL string
	transcriptServerErr     error
	transcriptServerReady   bool
)

// serveTranscriptSession returns the URL at which sessionID's transcript
// can be viewed, starting the shared transcript HTTP server on first use.
// The server listens on a single port for the remaining lifetime of the
// process (never explicitly shut down) and renders each session's jsonl
// on demand, keyed by the session id in the request path.
func serveTranscriptSession(sessionID string) (string, error) {
	transcriptServerOnce.Do(func() {
		transcriptServerBaseURL, transcriptServerErr = startTranscriptServer()
		transcriptServerReady = true
	})
	if transcriptServerErr != nil {
		return "", transcriptServerErr
	}
	return transcriptServerBaseURL + "/" + sessionID, nil
}

// runningTranscriptServerURL returns the URL at which sessionID's
// transcript can already be viewed, without starting the shared server if
// it hasn't been started yet. Used by the picker's preview pane, which
// must stay passive and never start the server just by scrolling the
// list.
func runningTranscriptServerURL(sessionID string) (string, bool) {
	if !transcriptServerReady || transcriptServerErr != nil {
		return "", false
	}
	return transcriptServerBaseURL + "/" + sessionID, true
}

// startTranscriptServer starts an HTTP server on the first available
// localhost port at or after serverPortRangeStart (up to
// serverPortRangeEnd), returning its base URL.
func startTranscriptServer() (string, error) {
	var lastErr error
	for port := serverPortRangeStart; port <= serverPortRangeEnd; port++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			lastErr = err
			continue
		}
		go func() {
			_ = http.Serve(ln, http.HandlerFunc(handleTranscriptRequest))
		}()
		return fmt.Sprintf("http://localhost:%d", port), nil
	}
	return "", fmt.Errorf("no available port in %d-%d: %w", serverPortRangeStart, serverPortRangeEnd, lastErr)
}

// handleTranscriptRequest renders, on demand, either the session named by
// the request path ("/<session-id>") or one of the sub agents that session
// spawned ("/<session-id>/subagents/<agent-id>").
func handleTranscriptRequest(w http.ResponseWriter, r *http.Request) {
	sessionID, agentID, err := parseTranscriptPath(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	path, err := findSessionFile(projectsDir(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var htmlContent string
	if agentID == "" {
		htmlContent, err = buildSessionHTML(sessionID, path)
	} else {
		agent, ok := findSubagent(path, agentID)
		if !ok {
			http.Error(w, "sub agent not found: "+agentID, http.StatusNotFound)
			return
		}
		htmlContent, err = buildSubagentHTML(sessionID, agent)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlContent))
}

// parseTranscriptPath splits a request path into the session id and, for a
// sub agent page, that agent's id (empty for the session itself). Both ids
// end up being matched against what's on disk rather than pasted into a
// path, but they are still rejected here if they contain a separator, so a
// malformed request fails as a 404 rather than reaching the filesystem.
func parseTranscriptPath(urlPath string) (sessionID, agentID string, err error) {
	parts := strings.Split(strings.Trim(urlPath, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return parts[0], "", nil
	case len(parts) == 3 && parts[0] != "" && parts[1] == subagentDirName && parts[2] != "":
		return parts[0], parts[2], nil
	default:
		return "", "", fmt.Errorf("unrecognized transcript path: %q", urlPath)
	}
}

// startCommand launches argv[0] with the remaining elements as arguments,
// without waiting for it to exit. It's a package variable so tests can
// stub it out.
var startCommand = func(argv []string) error {
	return exec.Command(argv[0], argv[1:]...).Start()
}

// fallbackOpenerArgvFor returns the argv of the platform default command
// that can open a URL when $BROWSER is not set, for the given GOOS value,
// or nil if there is no supported fallback for that platform.
func fallbackOpenerArgvFor(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"open"}
	default:
		return nil
	}
}

// fallbackOpenerArgv returns the argv of the current platform's fallback
// URL opener, or nil if none is available. It's a package variable — like
// isClaudeProcess in running.go and startCommand above — so tests can
// stub the platform behavior instead of depending on the real
// runtime.GOOS of whichever machine happens to run the tests.
var fallbackOpenerArgv = func() []string {
	return fallbackOpenerArgvFor(runtime.GOOS)
}

// openInBrowser opens url using the $BROWSER command, following the
// common convention: if any whitespace-separated token in $BROWSER
// contains "%s", url is substituted there; otherwise url is appended as
// the final argument.
//
// If $BROWSER is unset or empty, it falls back to the platform's default
// opener when one is available (currently just macOS's "open" command);
// if none is available for the current platform, it returns an error.
func openInBrowser(url string) error {
	argv := strings.Fields(os.Getenv("BROWSER"))
	if len(argv) == 0 {
		argv = fallbackOpenerArgv()
	}
	if len(argv) == 0 {
		return fmt.Errorf("BROWSER environment variable is not set and no fallback browser opener is available for this platform")
	}

	substituted := false
	for i, tok := range argv {
		if strings.Contains(tok, "%s") {
			argv[i] = strings.ReplaceAll(tok, "%s", url)
			substituted = true
		}
	}
	if !substituted {
		argv = append(argv, url)
	}

	return startCommand(argv)
}
