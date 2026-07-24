package ccr

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
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

// handleTranscriptRequest renders the session named by the request path
// (e.g. "/<session-id>") on demand.
func handleTranscriptRequest(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	path, err := findSessionFile(projectsDir(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	htmlContent, err := buildSessionHTML(sessionID, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlContent))
}

// startCommand launches argv[0] with the remaining elements as arguments,
// without waiting for it to exit. It's a package variable so tests can
// stub it out.
var startCommand = func(argv []string) error {
	return exec.Command(argv[0], argv[1:]...).Start()
}

// openInBrowser opens url using the $BROWSER command, following the
// common convention: if any whitespace-separated token in $BROWSER
// contains "%s", url is substituted there; otherwise url is appended as
// the final argument.
func openInBrowser(url string) error {
	browser := os.Getenv("BROWSER")
	if browser == "" {
		return fmt.Errorf("BROWSER environment variable is not set")
	}
	argv := strings.Fields(browser)
	if len(argv) == 0 {
		return fmt.Errorf("BROWSER environment variable is empty")
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
