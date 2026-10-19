package ccr

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunShow implements `ccr show`: prints what the history recorded of one
// session (each prompt and Claude's final answer to it, oldest first), or
// with -around only the exchange around one message, so a search hit can
// be read in its context.
func RunShow(args []string) error {
	fs := flag.NewFlagSet("ccr show", flag.ExitOnError)
	around := fs.Int64("around", 0, "show only the messages around this message id (as ccr search prints it)")
	context := fs.Int("context", 2, "with -around, how many messages to show before and after it")
	asJSON := fs.Bool("json", false, "print one JSON object per message")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ccr show [-around id [-context n]] [-json] <session_id>")
		fs.PrintDefaults()
	}
	// options may come after the session id too: a session id never
	// starts with "-", and it is an easy slip to put them there
	var ids []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		ids = append(ids, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(ids) != 1 {
		fs.Usage()
		return errors.New("give one session id")
	}
	sessionID := ids[0]

	msgs, err := recordedSessionMessages(sessionID)
	if errors.Is(err, errNoHistory) {
		return fmt.Errorf("the history has nothing recorded for session %s", sessionID)
	}
	if err != nil {
		return err
	}
	if *around != 0 {
		if msgs, err = messagesAround(msgs, *around, *context); err != nil {
			return err
		}
	}

	var out []byte
	if *asJSON {
		out, err = formatShowJSON(msgs)
		if err != nil {
			return err
		}
	} else {
		out = []byte(formatShow(sessionID, msgs, *around))
	}
	_, err = os.Stdout.Write(out)
	return err
}

// messagesAround returns the message id and up to n messages on either
// side of it.
func messagesAround(msgs []historyMessage, id int64, n int) ([]historyMessage, error) {
	for i, m := range msgs {
		if m.id == id {
			return msgs[max(0, i-n):min(len(msgs), i+n+1)], nil
		}
	}
	return nil, fmt.Errorf("message #%d is not in this session", id)
}

// formatShow renders msgs as text: a line naming the session, then each
// message under a header with its id, speaker and time. The message -around
// asked for is marked.
func formatShow(sessionID string, msgs []historyMessage, marked int64) string {
	var b strings.Builder
	b.WriteString("session " + sessionID)
	if cwd := msgs[0].cwd; cwd != "" {
		b.WriteString("  " + cwd)
	}
	b.WriteString("\n")
	for _, m := range msgs {
		header := fmt.Sprintf("--- #%d %s %s", m.id, roleLabel(m.role), m.createdAt.Local().Format("2006-01-02 15:04"))
		if m.id == marked {
			header += "  <<<"
		}
		b.WriteString("\n" + header + "\n")
		b.WriteString(strings.TrimRight(m.text, "\n") + "\n")
	}
	return b.String()
}

// showMessageJSON is one line of `ccr show -json`.
type showMessageJSON struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Time      string `json:"time"`
	Cwd       string `json:"cwd,omitempty"`
	Text      string `json:"text"`
}

func formatShowJSON(msgs []historyMessage) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, m := range msgs {
		if err := enc.Encode(showMessageJSON{
			ID:        m.id,
			SessionID: m.sessionID,
			Role:      m.role,
			Time:      m.createdAt.Local().Format(time.RFC3339),
			Cwd:       m.cwd,
			Text:      m.text,
		}); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}
