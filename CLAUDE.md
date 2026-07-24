# ccr

Claude Code の session resume をどのディレクトリにいても実行できるコマンド
指定した session id の cwd に移動してから exec で `claude --resume session_id` を
実行するためのサポートツールです。

以降の `${CLAUDE_CONFIG_DIR}`  の値は環境変数 `CLAUDE_CONFIG_DIR` が指定されていれば
その値、そうでなければ `${HOME}/.claude` とします。

## デフォルト動作(インタラクティブ picker)

引数なしで `ccr` を実行すると、対象セッションを、jsonl 内で最後に見つかった
`timestamp` フィールド(無ければファイルの mtime)の新しい順にソートし、
ターミナルを上下 pane に分割したインタラクティブ picker を起動します。
上 pane に `TIMESTAMP / SESSION ID / PID / CWD` の header 行と、それに続くセッション
一覧(上記の timestamp をローカルタイムで、session id、実行中セッションのみ pid、
cwd の basename)、下 pane にカーソルで選択中の
セッションのプレビュー(`Directory:` に cwd、`Title:` に type = "ai-title" の
最後の行の aiTitle の値(あれば)、`Size:` に session ファイルサイズを human
readable 形式で、続けて `Started:` / `Ended:` に jsonl 中で最初/最後に見つかった
`timestamp` フィールド(UTC の ISO8601)をローカルタイムに変換して表示(値が
見つからない場合はその行自体を表示しません)、その後に `Prompts:` に続けて
type = "last-prompt" の行の
lastPrompt の値(同じ値が連続する場合は uniq(1) のように1つにまとめた上で
最後から3件、この行には timestamp フィールドはありません)を、prompt 間に空行を
挟まず行頭 `·` 付きで表示。1行が pane の幅に収まらない場合は切り詰めずに
折り返します)を表示します。
カーソルキー
(または j/k)で選択を移動し、Enter で選択したセッションの cwd に移動して
exec で `claude --resume <session_id>` を実行します。移動先の cwd に `.envrc` が
あり `direnv` が PATH 上にあれば、`direnv exec <cwd> claude --resume <session_id>`
経由で実行し、その環境を読み込みます(direnv 自身が claude に exec するため
余計なプロセスは残りません)。`v` で選択中セッションの jsonl 全体を Human/AI が
見やすく区別された自己完結 HTML(light 基調の配色、Markdown レンダリング、コマンド
実行結果や diff の syntax highlight 付き)として表示するページを `$BROWSER` で
開きます。コマンドの入力やツールの呼び出し内容はそのまま表示されますが、実行結果
(output)やファイル内容、diff は既定で折り畳まれており、クリックすると開きます。この
HTML は `localhost` の 8000 番から順に listen 可能な port を探して起動する単一の
HTTP サーバーが、リクエストパスの session id (`/<session_id>`)ごとにその場で
レンダリングして返します(`v` を押すたびに新しいサーバーを起動するのではなく、
初回押下時に起動した1つのサーバーを使い回します)。
(サーバーは ccr プロセスが終了するまで動作し続けます。picker は終了しません。
結果は一覧の下に1行のステータスとして表示されます)。q / Esc / Ctrl-C で何も
実行せず終了します。

対象セッションの範囲:

- デフォルト: `${CLAUDE_CONFIG_DIR}/projects/<encoded_cwd>/<session_id>.jsonl`
  のみを対象にします。`<encoded_cwd>` はカレントディレクトリのうち
  `a-zA-Z0-9` 以外の文字をすべて `-` に置換したものです。
- `ccr -g`: `projects` 配下の全ディレクトリを対象に全セッションをリストアップします。

## 実行中セッションの検出

`${CLAUDE_CONFIG_DIR}/sessions/<pid>.json` には、実行中またはクリーンに終了
されなかった claude プロセスの情報 (`pid` と `sessionId` を含む) が記録されて
います。ccr はセッション一覧を作成する前にこれらのファイルを読み込み、記載
された `pid` のプロセスが実際に存在し `claude` であることを確認します。存在
しない、または `claude` プロセスでなかった場合はそのファイルの情報を無視し
ます(ファイル自体は削除しません)。有効と判定できたセッション ID について
は、一覧の PID 列に対応するプロセスの pid を表示します。
