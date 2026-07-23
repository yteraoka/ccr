# cc-resume

Claude Code の session resume をどのディレクトリにいても実行できるコマンド
指定した session id の cwd に移動してから exec で `claude -r session_id` を
実行するためのサポートツールです。

以降の `${CLAUDE_CONFIG_DIR}`  の値は環境変数 `CLAUDE_CONFIG_DIR` が指定されていれば
その値、そうでなければ `${HOME}/.claude` とします。

`cc-resume list` コマンドを実行すると `${CLAUDE_CONFIG_DIR}/projects/<dir>/<session_id>.jsonl`
の `session_id` のリストを stdout に出力します。

`cc-resume list --timestamps` コマンドを実行すると `timestamp session_id` のリストを stdout に
出力します。timestamp は session ファイルの更新時刻で YYYY-MM-DD HH:MM とします。
表示順は timestamp でソートします。

`cc-resume info <session_id>` コマンドを実行すると
`${CLAUDE_CONFIG_DIR}/projects/<dir>/<session_id>.jsonl` からファイルを見つけて
json に `cwd` を含む最初の行から cwd の値を取得します。
type = "user" でかつ origin.kind = "human" の行の最後から3つまでの message.content の値を取得します。
最後の行の timestamp の値を取得します。
stdout に cwd, timestamp, message.content の値を出力します。複数の message.content の値は空行を挟んで出力します。

