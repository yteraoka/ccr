# ccr

[日本語](README.ja.md)

`ccr` is a small CLI tool written in Go that lets you resume a
[Claude Code](https://claude.com/claude-code) session from any directory.
It shows an interactive picker of your past sessions, then `cd`s into the
selected session's original working directory and replaces itself via
`exec` with `claude --resume <session_id>`.

Session files are looked up under the `CLAUDE_CONFIG_DIR` environment
variable, or `$HOME/.claude` if it isn't set.

By default only sessions for the current directory are listed; pass `-g`
to also list sessions from other directories.

`ai-title` and `last-prompt` information from the session file is shown
as well.

Typing `v` in the list renders the session as HTML and opens it in your
web browser.

## Installation

### Homebrew

```bash
brew tap yteraoka/cask
brew trust yteraoka/cask
brew install --cask ccr
```

### mise

```bash
mise use -g github:yteraoka/ccr@0.0.1
```

### Go

```sh
go install github.com/yteraoka/ccr/cmd/ccr@latest
```

Or build from a checkout:

```sh
go build -o ccr ./cmd/ccr
```

## Usage

```bash
ccr
```

OR

```bash
ccr -g
```

By default, only sessions belonging to the current directory are targeted
(matched the same way Claude Code encodes `${CLAUDE_CONFIG_DIR}/projects/<dir>`
— every character outside `a-zA-Z0-9` in the cwd becomes `-`). Adding `-g`
targets sessions from every project instead.

The terminal splits into two panes:

- **Top pane** — sessions sorted by recency (most recently active first),
  with columns `TIMESTAMP` (local time), `SESSION ID`, `PID` (only shown
  for sessions with a currently running `claude` process — see below),
  `TOKENS` (cumulative token usage across the session's assistant messages),
  and `CWD` (basename). On a terminal too narrow to fit both (under 95
  columns), the session id is shortened to its first 8 characters so `CWD`
  stays readable. The bottom line of this pane shows the available keys.
- **Bottom pane** — a live preview of the highlighted session: its full
  session id (even when the list above shortens it), directory, title (if
  Claude Code has generated one), file size, token usage broken down by
  kind (in / out / cache write / cache read) behind the total, start/end
  time, and the last few prompts you sent in that session.

### Keybindings

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `Enter` | Resume the selected session (`cd` + `exec claude --resume <id>`) |
| `i` | Inspect the selected session's raw `jsonl` without leaving the terminal |
| `v` | View the full transcript of the selected session in your browser (see below) |
| `q`, `Esc`, `Ctrl-C` | Quit without doing anything |

### Environment variables

- `CLAUDE_CONFIG_DIR` — where Claude Code stores its data. Defaults to `${HOME}/.claude`.
- `BROWSER` — the command used to open the transcript viewer (see below). Follows the common convention: if any word contains `%s`, the URL is substituted there; otherwise the URL is appended as the last argument. If `BROWSER` is unset, macOS falls back to opening the URL with `open`; on other platforms, an error is shown instead.
- If a `.envrc` file exists in the destination directory, it is loaded via `direnv exec`.

## Inspecting the raw `jsonl` (`i`)

Pressing `i` opens the selected session's file in a full-screen viewer,
without leaving the terminal. It lists one row per line — the line number
in the file, its `type`, its timestamp, and the start of the raw text —
and `i` (or `Enter`) opens the line under the cursor as pretty-printed
JSON, in a modal floating over the list. `Enter` only means resume on the
picker's own list, so it is free here.

Inside the modal, `n` and `p` step to the next and previous line and show
it straight away, so you can walk the file without closing and reopening
it at every line; the list behind follows along. `Space` pages down, `b`
and `Backspace` page back up. The list stays visible behind it,
dimmed, so opening a line never loses your place in the file. The modal
wraps to its width and scrolls. Lines that are not valid JSON are listed
too and shown as they are: seeing them is the point of a raw preview.
`q`/`Esc` steps back out, first closing the modal and then the viewer.

## Viewing a full transcript (`v`)

Pressing `v` on a session renders its entire `jsonl` transcript as a
self-contained, light-themed HTML page — Human and AI turns are visually
distinguished, prose is rendered as Markdown, and commands/diffs/file
content are syntax-highlighted. Notifications the harness injects on your
behalf (a sub agent reporting its result, a monitor event) arrive as user
lines but are labelled `🔔 Notification` rather than attributed to you.

Runs of back-to-back tool calls are folded into a single collapsed
`N tool calls` block, with each call collapsed again inside it and
individually expandable — its summary names the tool and what it acted on,
so the transcript reads as a conversation until you open one. A failed call
is coloured on that summary line, so you can spot it without opening
anything. A turn that also says something keeps its own card and ends the
run.

A filter pane on the right toggles what the page shows: message kinds
(Human, Claude, Notification, System, and thinking blocks) and each tool
that was actually called (Read, Edit, Bash, …), with a count next to each
and `All`/`None` buttons. Only kinds present in that session get a row,
and a message left with nothing visible is hidden along with its contents.
Assistant turns that report token usage show it in the card header next to
the timestamp, broken down the same way as in the picker.

Every event carries a `{ }` button that shows the original jsonl line
behind it. The JSON is not embedded in the page — that would roughly double
a transcript already measured in megabytes — so the page holds only the
line's offset and length and asks the server for those bytes when you press
the button.

If the session spawned sub agents, each one's own transcript is a click
away: next to the tool call that started it, on the report it sent back,
and listed under `Sub agents` in the pane. Those pages render exactly like
a session and link back to the one they belong to. Command output, diffs, and file content
are collapsed by default (click to expand) so the page stays scannable.

The page isn't written to a file: `ccr` starts a small local HTTP server
(the first free port starting at `8000`) that renders each session on
demand at `http://localhost:<port>/<session_id>`, then opens that URL via
`$BROWSER`. The server is started once per `ccr` run and reused for every
session you view afterwards; once it's running, the preview pane shows
`Serving at: <url>` for the highlighted session even if you haven't
pressed `v` on it yet. The picker keeps running — it doesn't exit after
opening a transcript.

## Detecting already-running sessions

Claude Code records a snapshot of every running (or not cleanly
terminated) session at `${CLAUDE_CONFIG_DIR}/sessions/<pid>.json`,
including its `pid` and `sessionId`. Before building the session list,
`ccr` reads these files and checks that the recorded `pid` is both alive
and actually a `claude` process; stale or mismatched entries are ignored
(the files themselves are never deleted). For sessions with a live
process, the `PID` column in the list shows it.

## License

MIT — see [LICENSE](LICENSE).
