# ccr

[日本語](README.ja.md)

`ccr` is a small CLI tool written in Go that lets you resume a
[Claude Code](https://claude.com/claude-code) session from any directory.
It shows an interactive picker of your past sessions, then `cd`s into the
selected session's original working directory and replaces itself via
`exec` with `claude --resume <session_id>`.

Session files are looked up under the `CLAUDE_CONFIG_DIR` environment
variable, or `$HOME/.claude` if it isn't set. If you keep several Claude
Code config directories (e.g. work and personal), list them in the
[config file](#config-file) and pass `-A` to browse all of them at once.

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
targets sessions from every project instead. Adding `-A` also lists
sessions from every Claude Code config directory registered in the
[config file](#config-file); it combines with `-g`.

On a machine with no browser — a server you are working on over SSH,
typically — add `-n` (`-no-browser`) so that `v` only prints the transcript
URL instead of trying to open it. `CCR_NO_BROWSER=1` does the same without
the flag, and `ccr` behaves that way on its own whenever there is no way to
open a browser at all. See [Viewing a full transcript (`v`)](#viewing-a-full-transcript-v).

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
| `↑`/`k`/`p`, `↓`/`j`/`n` | Move the cursor |
| `Space`/`PageDown`, `b`/`Backspace`/`PageUp` | Page through the list |
| `g`, `G` | Jump to the first or last session |
| `/` | Filter the list as you type (id and directory) |
| `s` | Search past prompts and answers (see [below](#in-the-picker-s)) |
| `Enter` | Resume the selected session (`cd` + `exec claude --resume <id>`) |
| `i` | Inspect the selected session's raw `jsonl` without leaving the terminal |
| `v` | View the full transcript of the selected session in your browser (see below) |
| `q`, `Esc`, `Ctrl-C` | Quit without doing anything |

### Environment variables

- `CLAUDE_CONFIG_DIR` — where Claude Code stores its data. Defaults to `${HOME}/.claude`. More directories can be added in the [config file](#config-file) for `-A`.
- `CCR_HISTORY_DB` — the database `ccr hook` records to and `ccr search` searches (see [below](#searching-past-prompts-and-answers-ccr-search)).
- `CCR_CONFIG` — path of ccr's config file. Defaults to `${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json`.
- `BROWSER` — the command used to open the transcript viewer (see below). Follows the common convention: if any word contains `%s`, the URL is substituted there; otherwise the URL is appended as the last argument. If `BROWSER` is unset, macOS falls back to opening the URL with `open`; on other platforms there is nothing to fall back to, so `v` shows the URL instead of opening it.
- `CCR_NO_BROWSER` — set it to `1` (anything but `0`, `false`, `no`, or `off`) to make `v` show the transcript URL instead of opening a browser, the same as passing `-n`.
- If a `.envrc` file exists in the destination directory, it is loaded via `direnv exec`.

## Inspecting the raw `jsonl` (`i`)

Pressing `i` opens the selected session's file in a full-screen viewer,
without leaving the terminal. It lists one row per line — the line number
in the file, its `type`, its timestamp, and the start of the raw text —
`n` and `p` step a line, `Space` pages the list down, `b` and `Backspace`
page it up, `/` searches it incrementally, and `i` (or `Enter`) opens the
line under the cursor as pretty-printed JSON, in a modal floating over the
list.

Filtering works the same on both lists: type and only the matching rows
stay, with the prompt showing how many of how many. `Enter` ends the
typing but keeps the list narrowed, so the ordinary keys then move,
resume and open within it; `Esc` clears the filter, and only then does it
leave the screen. On the session list a query matches the id and the
directory; in the file it matches the whole raw line, so `tool_use` or a
path finds the lines carrying it. `Enter` only means resume on the
picker's own list, so it is free here.

Inside the modal, `n` and `p` step to the next and previous line and show
it straight away, so you can walk the file without closing and reopening
it at every line; the list behind follows along. Every movement key means
the same on both screens: `n`/`p` step a line, `Space` pages down, and `b`
and `Backspace` page up. The list stays visible behind it,
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

If the file records a `cost-state` line, the header shows what the session
cost, with a collapsed breakdown of the time it spent and what each model
was asked to do. Sessions written without one show no cost at all.

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

### Without a browser

Run `ccr -n` (or set `CCR_NO_BROWSER=1`) and `v` starts the server without
opening anything, printing `serving at http://localhost:<port>/<session_id>`
on the picker's bottom line for you to copy. `ccr` does this by itself when
there is no browser to open — `$BROWSER` unset with no platform fallback —
so a headless machine needs no flag at all.

The server only listens on `127.0.0.1`, so to read the page from another
machine, forward the port over SSH:

```bash
ssh -L 8000:localhost:8000 user@server
```

Then open the URL that `ccr` printed, on your own machine.

## Searching past prompts and answers (`ccr search`)

`ccr hook` records every prompt you submit and every answer Claude gives
in a SQLite database, and `ccr search` finds them again — by keyword
(BM25, Japanese included) and, once an embedding model is configured, by
meaning (vector search with SQLite's
[vec1](https://sqlite.org/vec1) extension), the two rankings fused.

### Recording

Register `ccr hook` for Claude Code's `UserPromptSubmit` and `Stop` hooks:

```bash
ccr hook install                  # you, every project: $CLAUDE_CONFIG_DIR/settings.json
ccr hook install -A               # ...in every config directory of the config file
ccr hook install -scope project   # this project, for everyone: ./.claude/settings.json
sudo ccr hook install -scope system   # every user of this machine (managed settings)
```

| Scope | File written |
| --- | --- |
| `user` (default) | `$CLAUDE_CONFIG_DIR/settings.json` (`~/.claude/settings.json`) |
| `project` | `.claude/settings.json` in the current directory |
| `system` | `managed-settings.d/ccr.json` in the managed settings directory: `/Library/Application Support/ClaudeCode` (macOS), `/etc/claude-code` (Linux, WSL), `C:\Program Files\ClaudeCode` (Windows) |

The rest of the settings file, other hooks included, is left as it is,
and running it again changes nothing. For `system`, ccr writes a file of
its own in `managed-settings.d/` (which Claude Code merges with
`managed-settings.json`) rather than editing an administrator's file;
note that managed settings delivered by MDM or the claude.ai console take
the place of these files. The command registered is `ccr hook` when the
`ccr` on `PATH` is the one running, otherwise its full path; `-command`
sets it, and `-print` shows the result without writing it.
`ccr hook uninstall` (same options) takes the hooks out again.

Written by hand, the hooks are:

```json
{
  "hooks": {
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "ccr hook", "async": true }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "ccr hook", "async": true }] }]
  }
}
```

`async` keeps the hook (and the embedding request it makes) from delaying
your prompt. The database is `${XDG_DATA_HOME:-$HOME/.local/share}/ccr/history.db`
unless `history.db` in the [config file](#config-file) or `CCR_HISTORY_DB`
says otherwise.

### Importing existing sessions

The hook only sees what is said after it is set up. To make earlier
sessions searchable too, import their transcripts:

```bash
ccr import        # every session under the current CLAUDE_CONFIG_DIR
ccr import -A     # ...and under every config_dirs entry of the config file
```

Each prompt you typed and, for each turn, Claude's final answer are
recorded, as the hook would have recorded them. Messages already in the
history (recorded by the hook, or by an earlier import) are not added
again, and transcripts unchanged since the last import are not read again,
so it is safe to run as often as you like. A turn still in progress in a
running session is left for a later import to pick up once it has
finished. With an embedding model configured the imported messages are
embedded straight away (`-no-embed` leaves that to the next `ccr search`).

### Searching

```bash
ccr search 全文検索                    # this directory's sessions
ccr search -g sqlite ベクトル          # every project; all words must match
ccr search -g -role user -l 20 hook   # only your prompts, 20 results
```

| Option | Meaning |
| --- | --- |
| `-g` | Search every project, not just sessions run in the current directory |
| `-l n` | Number of results (default 10) |
| `-role user\|assistant` | Only your prompts, or only Claude's answers |
| `-mode hybrid\|keyword\|vector` | Default is `hybrid` with an embedding model, `keyword` without |

Each result shows its time, who said it, and the session id (for
`claude --resume`), with the matching part of the text highlighted.

### In the picker (`s`)

Press `s` in the session list, type the query and press `Enter`. The
results are listed with the matching part highlighted, and the message
under the cursor is shown in full below the list, scrolled to where it
matches. From a result:

| Key | Action |
| --- | --- |
| `↑`/`↓`, `j`/`k`, `n`/`p`, `Space`/`b`, `g`/`G` | Move, as in the session list |
| `Enter` | Resume the session the message is from |
| `i` / `v` | Open that session's `jsonl` / transcript, as in the session list |
| `s`, `/` | Edit the query and search again |
| `q`, `Esc` | Back to the session list (`s` brings the last search back) |

It searches the current directory's messages, or every project's under
`ccr -g`, with the default mode (`hybrid` when an embedding model is
configured).

Keyword search splits the query on spaces; every word must appear.
Japanese in the query is split into words with the morphological analyzer
[kagome](https://github.com/ikawaha/kagome) (IPA dictionary), dropping
particles, auxiliaries and pronouns, so `日本語の検索` finds messages with
both 日本語 and 検索 anywhere in them. Runs of nouns stay together
(`全文検索`), and verbs and adjectives are reduced to the part their
conjugations share (`変えたい` → `変え`), and left out when that is a single
character (`行きたい` → `行`), which would match far too much. Put a phrase in double quotes to
search for it exactly as written. Loading kagome's dictionary adds about a
second to a search with Japanese in it.

Stored text is indexed differently: runs of Japanese (or other CJK) text
become overlapping two-character pieces, so any word of two or more
characters is found as a substring — whatever kagome makes of it — and a
single character matches too. Full-width letters, half-width
katakana and case are folded. (The index uses ccr's own tokenizer, so the
`sqlite3` shell can read the database but not query the index.)

### Vector search

Vector search uses [Ruri v3](https://huggingface.co/collections/cl-nagoya/ruri-v3),
a Japanese embedding model, by default. Start the bundled server (needs
[uv](https://docs.astral.sh/uv/); the model is downloaded from Hugging Face
on first start):

```bash
uv run https://raw.githubusercontent.com/yteraoka/ccr/main/scripts/ruri-embedding-server.py
# or, in a checkout: uv run scripts/ruri-embedding-server.py
```

and turn vector search on with an empty `embedding` section:

```json
{
  "history": {
    "embedding": {}
  }
}
```

The server listens on `http://127.0.0.1:8080/v1/embeddings` and serves
`cl-nagoya/ruri-v3-310m`; `--model cl-nagoya/ruri-v3-130m` (or `-70m`,
`-30m`) trades accuracy for speed, and `--port` / `--device` are there too.
Name the same model in the config if you change it. ccr adds the
`検索クエリ: ` / `検索文書: ` prefixes Ruri v3 is trained with to every model
whose name contains `ruri-v3`, so any other server for it (llama.cpp with a
GGUF conversion, Text Embeddings Inference, ...) works the same way.

Any other OpenAI-compatible embeddings endpoint works as well — Ollama,
LM Studio, llama.cpp, OpenAI, Voyage AI:

```json
{"history": {"embedding": {"url": "http://localhost:11434/v1/embeddings", "model": "bge-m3"}}}
```

| Key | Meaning |
| --- | --- |
| `url` | The endpoint. Default `http://127.0.0.1:8080/v1/embeddings` |
| `model` | The model. Default `cl-nagoya/ruri-v3-310m` |
| `api_key_env` | Environment variable holding the API key, sent as a bearer token |
| `dimensions` | Ask the model for shorter vectors, where it supports it |
| `query_prefix`, `document_prefix` | Prepended to queries / stored texts. Default: Ruri v3's for Ruri v3, none otherwise (e5 wants `query: ` / `passage: `); `""` turns the Ruri default off |

The hook embeds each message as it records it; anything it could not
embed (the server was down, or embedding was configured later) is embedded
by the next `ccr search`. Changing the model re-embeds everything.

## Config file

`ccr` reads an optional JSON config file from
`${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json` (override the path with
the `CCR_CONFIG` environment variable). `config_dirs` lists additional
Claude Code config directories that `ccr -A` lists sessions from. Without
`-A` the file is not read and only the current `CLAUDE_CONFIG_DIR` is used:

```json
{
  "config_dirs": [
    "~/.claude",
    "~/.claude-work"
  ]
}
```

- Entries must be absolute paths; a leading `~` and `$VARS` are expanded.
- The directory in effect for the current shell (`CLAUDE_CONFIG_DIR`, or
  `$HOME/.claude`) is always included, and listed first. Duplicates
  (including the same directory reached through a symlink) count once.
  Directories that don't exist are skipped.
- With `-A`, sessions from every directory are merged into one list, both
  for the default current-directory view and for `-g`. When more than one
  directory is in use, the preview shows which one the session lives in
  (`Config:`).
- `Enter` resumes the session with `CLAUDE_CONFIG_DIR` pointing at the
  directory it was found in, so it continues in the right profile. For
  `$HOME/.claude` the variable is unset instead, as Claude Code reads
  `~/.claude.json` only when it isn't set. (If the session's directory has
  a `.envrc` that sets `CLAUDE_CONFIG_DIR`, direnv's value wins.)
- Running sessions are detected in every directory's `sessions/`.

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
