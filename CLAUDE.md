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
上 pane に `TIMESTAMP / SESSION ID / PID / TOKENS / CWD` の header 行、それに続く
セッション一覧(上記の timestamp をローカルタイムで、session id、実行中セッション
のみ pid、TOKENS はセッションの jsonl 中の type = "assistant" の行にある
`message.usage` の `input_tokens` + `output_tokens` + `cache_creation_input_tokens`
+ `cache_read_input_tokens` を `message.id` で重複除去(同じメッセージが content
block ごとに複数行に分かれて同じ usage を繰り返すため)した上で合計した値を
1000 未満はそのまま、1000 以上は小数点1桁 + K/M などの単位で表示、cwd の
basename)、最下部に使用可能なキーの説明を1行で表示します。
CWD 列に 20 桁を残せないほどターミナルの幅が狭い場合(75 + 20 = 95 桁未満)は、
session id を先頭 8 文字に短縮し(header も `ID` に変更)、その分を CWD に
充てます。幅が足りていれば session id は 36 桁の全体を表示します。下 pane に
カーソルで選択中の
セッションのプレビュー(`Session:` に session id 全体(一覧側が短縮していても
常に完全な値)、`Directory:` に cwd、`Title:` に type = "ai-title" の
最後の行の aiTitle の値(あれば)、`Size:` に session ファイルサイズを human
readable 形式で、`Tokens:` に上記 TOKENS と同じ合計値に続けて種別ごとの内訳を
`合計 (in <input_tokens> / out <output_tokens> / cache write
<cache_creation_input_tokens> / cache read <cache_read_input_tokens>)` の形式で
(いずれも TOKENS 列と同じ単位表記。80 桁の端末でも折り返さない長さに収めます)、
続けて `Started:` / `Ended:` に jsonl 中で最初/最後に見つかった
`timestamp` フィールド(UTC の ISO8601)をローカルタイムに変換して表示(値が
見つからない場合はその行自体を表示しません)、`v` で既にサーバーが起動済みなら
`Ended:` の直後に `Serving at: <url>` を表示、その後に `Prompts:` に続けて
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
開きます(`$BROWSER` が未設定の場合、macOS では `open` コマンドにフォールバック
します)。コマンドの入力やツールの呼び出し内容はそのまま表示されますが、実行結果
(output)やファイル内容、diff は既定で折り畳まれており、クリックすると開きます。この
HTML は `localhost` の 8000 番から順に listen 可能な port を探して起動する単一の
HTTP サーバーが、リクエストパスの session id (`/<session_id>`)ごとにその場で
レンダリングして返します(`v` を押すたびに新しいサーバーを起動するのではなく、
初回押下時に起動した1つのサーバーを使い回します。サーバーは ccr プロセスが
終了するまで動作し続けます)。picker は終了せず、エラーが起きた場合のみ一覧の下に
1行のステータスとして表示されます。q / Esc / Ctrl-C で何も実行せず終了します。

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
