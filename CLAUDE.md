# ccr

Claude Code の session resume をどのディレクトリにいても実行できるコマンド
指定した session id の cwd に移動してから exec で `claude --resume session_id` を
実行するためのサポートツールです。

以降の `${CLAUDE_CONFIG_DIR}`  の値は環境変数 `CLAUDE_CONFIG_DIR` が指定されていれば
その値、そうでなければ `${HOME}/.claude` とします。

## デフォルト動作(インタラクティブ picker)

引数なしで `ccr` を実行すると、対象セッションを mtime の新しい順にソートし、
ターミナルを上下 pane に分割したインタラクティブ picker を起動します。
上 pane に `TIMESTAMP / SESSION ID / CWD` の header 行と、それに続くセッション
一覧(timestamp、session id、cwd の basename)、下 pane にカーソルで選択中の
セッションのプレビュー(`Directory:` に cwd、`Title:` に type = "ai-title" の
最後の行の aiTitle の値(あれば)、`Size:` に session ファイルサイズを human
readable 形式で、その後に `Prompts:` に続けて type = "last-prompt" の行の
lastPrompt の値(同じ値が連続する場合は uniq(1) のように1つにまとめた上で
最後から3件、この行には timestamp フィールドはありません)を、prompt 間に空行を
挟まず行頭 `·` 付きで表示。1行が pane の幅に収まらない場合は切り詰めずに
折り返します)を表示します。
カーソルキー
(または j/k)で選択を移動し、Enter で選択したセッションの cwd に移動して
exec で `claude --resume <session_id>` を実行します。移動先の cwd に `.envrc` が
あり `direnv` が PATH 上にあれば、`direnv exec <cwd> claude --resume <session_id>`
経由で実行し、その環境を読み込みます(direnv 自身が claude に exec するため
余計なプロセスは残りません)。q / Esc / Ctrl-C で何も実行せず終了します。

対象セッションの範囲:

- デフォルト: `${CLAUDE_CONFIG_DIR}/projects/<encoded_cwd>/<session_id>.jsonl`
  のみを対象にします。`<encoded_cwd>` はカレントディレクトリのうち
  `a-zA-Z0-9` 以外の文字をすべて `-` に置換したものです。
- `ccr -g`: `projects` 配下の全ディレクトリを対象に全セッションをリストアップします。
