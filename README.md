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
  and `CWD` (basename). The bottom line of this pane shows the available
  keys.
- **Bottom pane** — a live preview of the highlighted session: directory,
  title (if Claude Code has generated one), file size, start/end time,
  and the last few prompts you sent in that session.

### Keybindings

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `Enter` | Resume the selected session (`cd` + `exec claude --resume <id>`) |
| `v` | View the full transcript of the selected session in your browser (see below) |
| `q`, `Esc`, `Ctrl-C` | Quit without doing anything |

### Environment variables

- `CLAUDE_CONFIG_DIR` — where Claude Code stores its data. Defaults to `${HOME}/.claude`.
- `BROWSER` — the command used to open the transcript viewer (see below). Follows the common convention: if any word contains `%s`, the URL is substituted there; otherwise the URL is appended as the last argument.
- If a `.envrc` file exists in the destination directory, it is loaded via `direnv exec`.

## Viewing a full transcript (`v`)

Pressing `v` on a session renders its entire `jsonl` transcript as a
self-contained, light-themed HTML page — Human and AI turns are visually
distinguished, prose is rendered as Markdown, and commands/diffs/file
content are syntax-highlighted. Command output, diffs, and file content
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
