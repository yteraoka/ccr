# ccr

Claude Code の session resume をどのディレクトリにいても実行できるコマンド
指定した session id の cwd に移動してから exec で `claude --resume session_id` を
実行するためのサポートツールです。

以降の `${CLAUDE_CONFIG_DIR}`  の値は環境変数 `CLAUDE_CONFIG_DIR` が指定されていれば
その値、そうでなければ `${HOME}/.claude` とします。ただし `-A` を指定した場合は、
設定ファイルに登録した複数の config ディレクトリのすべてに対して同じ処理を行います
(後述の「設定ファイル」を参照)。

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
`i` で選択中セッションの jsonl を TUI 内で全画面プレビューします。
`LINE / TYPE / TIME / CONTENT` の一覧(ファイル上の行番号、`type`、`timestamp` を
ローカルタイムの時刻で、行の生テキストを幅に合わせて切り詰め)を表示し、
一覧は上下・`j` / `k`・`n` / `p`(いずれも1行移動)・home/end のほか、space で
1画面送り、`b` / Backspace で1画面戻し(いずれもモーダル内と同じ割り当て)で
スクロールします。`/` で同じ絞り込みができ、行の生テキスト全体(type も timestamp も payload も
含む)を対象に一致する行だけを表示します(legend 行がプロンプトに変わります)。
絞り込み中の移動・JSON 表示はすべて残った行に対して働きます。
一覧で再度 `i`(または Enter)を押すと、カーソル行の整形済み JSON をモーダルとして
一覧の上に重ねて表示します。Enter が resume を意味するのは picker のセッション一覧
だけなので、この画面では JSON を開くキーとして使えます。モーダルは角丸の枠で囲み、開いている間は背後の一覧を faint に
落とします(一覧はそのまま見えるので、開いてもファイル内の位置を見失いません)。
モーダル内は幅に合わせて折り返し、上下・home/end のほか、space で1画面送り、
`b` / Backspace で1画面戻し(pgdn / pgup と同じ)でスクロールでき、
`n` / `p` で前後の行に移動してそのまま次の JSON を表示します(背後の一覧の
カーソルとスクロール位置も追随するので、閉じたときに該当行が見えています)。
タイトル行に `line <n> of <total>` と残りの行数を出します。JSON として解釈できない
行も一覧に残し、開いたときは生のまま表示します(空行のみ除外し、行番号は
ファイル上の番号を保ちます)。モーダルは q / Esc / i で閉じ(Enter では閉じません)、一覧では q / Esc で
picker へ戻ります。
カーソルキー
(または `j` / `k`、`n` / `p`)で選択を移動し、space で1画面送り、`b` / Backspace で
1画面戻し(PageDown / PageUp も同じ)、`g` / `G` で先頭・末尾へ移動できます
(1画面の移動量は一覧が実際に表示している行数です)。`/` でインクリメンタルな
絞り込みを開始し、入力するたびに session id と cwd を対象に(大文字小文字を無視して)
**一致する行だけを表示**します。最下部のステータス行が `/<query>` のプロンプトになり、
`<一致数>/<全体数>` を添えます(一致が無い場合は `(no match, esc to clear)`)。
Enter で入力を終えますが絞り込みは維持し(プロンプトに `⏎` が付きます)、そのまま
通常のキーで移動・resume・`i` などを絞り込み後の一覧に対して行えます。Esc で
絞り込みを解除し、絞り込みが無い状態の Esc で ccr を終了します。入力中でも矢印や
ページキーはカーソル移動として働き、`n` や `q` など文字を伴うキーはクエリに入ります。
Enter で選択したセッションの cwd に移動して
exec で `claude --resume <session_id>` を実行します。移動先の cwd に `.envrc` が
あり `direnv` が PATH 上にあれば、`direnv exec <cwd> claude --resume <session_id>`
経由で実行し、その環境を読み込みます(direnv 自身が claude に exec するため
余計なプロセスは残りません)。`v` で選択中セッションの jsonl 全体を Human/AI が
見やすく区別された自己完結 HTML(light 基調の配色、Markdown レンダリング、コマンド
実行結果や diff の syntax highlight 付き)として表示するページを `$BROWSER` で
開きます(`$BROWSER` が未設定の場合、macOS では `open` コマンドにフォールバック
します)。ブラウザを開く手段が無い環境(`$BROWSER` 未設定でプラットフォームの
フォールバックも無い場合)では、ブラウザを開けなかったことをエラーにはせず、
サーバーだけを起動して一覧の下のステータス行に `serving at <url>` と URL を
表示します。`-n`(`-no-browser`)オプション、または環境変数 `CCR_NO_BROWSER`
(`0` / `false` / `no` / `off` 以外の値)を指定した場合は、ブラウザを開ける環境
でも同様に開かず URL の表示だけを行います。type = "user" の行のうち `promptSource` が `system` のもの(sub agent の
結果報告や monitor イベントなど、harness が注入した通知)は Human ではなく
`🔔 Notification` として表示します(`isMeta` の行は従来どおり `⚙️ System`)。
メッセージ本文の Markdown 中に現れる HTML タグ(`<system-reminder>` などの
harness が注入する疑似 XML タグを含む)はマークアップとして解釈せず、エスケープ
して書かれたとおりに表示します。ブロックとして現れた場合は改行を保ったまま
表示し、長い行は折り返します。
各 card のラベルのアイコンは Human が `🐰`、Claude は Claude のマーク(coral
`#d97757` の放射状のシンボル)をインライン SVG で描画したものを使います
(自己完結ページを保つため外部画像は使いません)。コマンドの入力やツールの呼び出し内容はそのまま表示されますが、実行結果
(output)やファイル内容、diff は既定で折り畳まれており、クリックすると開きます。
tool 呼び出しだけからなる assistant のターンが連続する範囲は、1つの折り畳み
(`N tool calls`、既定で閉じた状態)にまとめ、その中で各ターンをさらに折り畳んだ
状態(summary に tool の icon と対象 — Bash は description、無ければコマンドの
1行目を 80 文字まで、Edit/Write/Read はファイルパス — および timestamp と
トークン使用量)で並べ、それぞれ個別にトグルで展開できるようにします。連続が
1件だけの場合は外側のグループを作らず、そのターンの折り畳みのみを置きます。
折り畳み内の tool 実行が1件のときは、summary と同じ内容になる tool card 側の
ヘッダー(icon + タイトル)を表示しません。この場合、失敗した実行を示す色
(文字色と背景色)は tool card のヘッダーではなく折り畳みの summary に付けます。
1つの折り畳みに複数の tool 実行が含まれる場合は、card を見分ける手段が
ヘッダーしかないため従来どおり表示します。
文章を含む assistant のターンは従来どおり card として表示し、そこで連続は途切れます。
折り畳んだ要素にも `data-kind="assistant"` を付け、filter pane の Claude の
切り替え対象から外れないようにします。filter でツールを隠した結果、中身が
すべて隠れた折り畳みとグループも隠し、グループの summary の件数は表示中の
件数に更新します。
ページ右側には表示/非表示を切り替える filter pane を置きます。`Messages` グループに
メッセージ種別(Human / Claude / Notification / System と、assistant の thinking
ブロック)、`Tools` グループに Read / Edit / Bash など実際に呼ばれたツールを名前順に
並べ、それぞれ件数を添えて checkbox で切り替えられるようにします(そのセッションに
存在しない種別は行自体を出しません)。`All` / `None` ボタンで一括切り替えができます。
ツールを非表示にした結果 body が空になった message card も併せて隠します。
セッションが sub agent を起動していた場合
(`${CLAUDE_CONFIG_DIR}/projects/<encoded_cwd>/<session_id>/subagents/agent-<agent_id>.jsonl`
が存在する場合)、その transcript へのリンクを表示します。同じディレクトリの
`agent-<agent_id>.meta.json` から `agentType` / `name` / `description` / `toolUseId` を
読み、リンクのラベルにします。`toolUseId` が一致する tool_use の直後と、本文の
`<task-id>` が一致する Notification card の先頭にそれぞれインラインのリンクを出し、
さらに filter pane の `Sub agents` グループに全件を並べます(どの message にも
結びつけられない sub agent が取り残されないようにするため)。リンク先は
`/<session_id>/subagents/<agent_id>` で、sub agent の jsonl は session と同じ形式の
ため同じ描画経路で表示し、ヘッダーに元 session へ戻るリンクを置きます。agent_id は
ディレクトリの一覧と突き合わせて解決するので、URL 経由でセッションのディレクトリ外を
参照することはできません。
各 message card と tool 実行の折り畳みには `{ }` ボタンを置き、押すとその
イベントの元の jsonl 行を整形して表示します。HTML には JSON 本体を埋め込まず、
その行のファイル内での開始位置(バイトオフセット)と長さだけを
`data-offset` / `data-len` として持たせ、押された時点でページ自身のパスに
`/raw?offset=<offset>&len=<len>` を付けてサーバーに要求します
(sub agent のページなら `/<session_id>/subagents/<agent_id>/raw`)。
サーバーは対象ファイルのそのバイト範囲だけを返し、範囲がファイルの外に出る場合や
長さが 16MiB を超える場合は 400 を返します。一度取得した内容はページ内に保持し、
再度押すと表示/非表示が切り替わります。
jsonl に type = "cost-state" の行があれば、その最後の1行(累積スナップショットが
複数書かれるため)をヘッダーに表示します。`Cost:` のチップに合計金額を出し、その下に
折り畳み(既定で閉じた `💰 <合計> — cost and timing`)で、合計 / セッション時間 /
API 時間 / tool 実行時間 / 追加・削除行数と、`modelUsage` のモデルごとの内訳
(in / out / cache write / cache read とモデル別の金額)を表で並べます。モデルは
名前順に並べ、金額は 0.01 未満のときだけ小数4桁で表示します。
`hasUnknownModelCost` が true の場合は、合計が下限である旨を添えます。
cost-state を持たないファイルでは何も表示しません。
assistant のメッセージのうち `message.usage` を持つものは、card のヘッダーに
timestamp と並べてそのターンのトークン使用量(合計と種別ごとの内訳、picker と同じ
表記)を表示します。同一 `message.id` が content block ごとに複数行に分かれている
場合、使用量は実際に描画される最初の行にのみ付けます。この
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

## コマンドラインオプション

- `-g`: 上記のとおり全プロジェクトを対象にします。
- `-A`: 設定ファイルの `config_dirs` に登録したすべての config ディレクトリを
  対象にします(`-g` と組み合わせ可能)。指定しない場合は設定ファイルを読まず、
  現在の `${CLAUDE_CONFIG_DIR}` のみを対象にします。
- `-n` / `-no-browser`: `v` でブラウザを開かず URL の表示だけを行います
  (環境変数 `CCR_NO_BROWSER` でも同じ設定ができます)。

## プロンプトと回答の履歴検索

`ccr hook` を Claude Code の `UserPromptSubmit` / `Stop` hook に登録すると
(`"async": true` を推奨)、標準入力の hook JSON から、`UserPromptSubmit` では
`prompt` を role = user、`Stop` では `last_assistant_message` を role = assistant
として SQLite に記録します。`last_assistant_message` が無い古い Claude Code では
`transcript_path` の jsonl から、最後に人が入力したプロンプト以降の最後の assistant
メッセージのテキストを取り出します。それ以外のイベントや空のテキストは何もしません。
UserPromptSubmit の stdout は会話に追加されるため、hook は stdout に何も書きません。

- データベース: `CCR_HISTORY_DB`、設定ファイルの `history.db`、
  `${XDG_DATA_HOME:-$HOME/.local/share}/ccr/history.db` の順に決めます。
  CGO なしでビルドするため SQLite は `github.com/ncruces/go-sqlite3` を使い、
  WAL と busy_timeout で複数セッションの hook の同時書き込みに耐えます。
- `messages` テーブルに session id / role / text / cwd / `CLAUDE_CONFIG_DIR` /
  記録時刻を保存し、FTS5 の external content テーブル `messages_fts` で索引します。
  tokenizer は独自の `ccr_ja`: CJK(漢字・ひらがな・カタカナ・ハングルと `ー`
  など)の連続は重なりのある bigram と(文書側のみ)末尾の1文字、それ以外の
  文字・数字の連続は1語とし、全角英数字→半角、半角カナ→全角、小文字化で正規化します。
- 設定ファイルの `history.embedding`(OpenAI 互換の `/v1/embeddings`:
  `url` / `model` / `api_key_env` / `dimensions` / `query_prefix` /
  `document_prefix`)があればベクトル検索を有効にします。既定のモデルは Ruri v3
  (`url` 省略時 `http://127.0.0.1:8080/v1/embeddings`、`model` 省略時
  `cl-nagoya/ruri-v3-310m`)で、`scripts/ruri-embedding-server.py`(uv で実行する
  sentence-transformers のサーバー、prefix は付けない)がその既定に合わせて listen
  します。名前に `ruri-v3` を含むモデルには、`query_prefix` / `document_prefix` が
  未指定なら `検索クエリ: ` / `検索文書: ` を付けます(`""` を指定すれば付けません)。vec1 の仮想テーブル
  `messages_vec`(flat index、cos 距離、最初のベクトルで次元が決まるため初回保存時に
  作成)に rowid = messages.id で保存します。hook は記録直後にそのメッセージを
  embedding し、失敗したもの・未処理のものは `ccr search` が検索前にまとめて
  embedding します。モデル(url / model / dimensions / document_prefix)が
  変わったら `messages_vec` を作り直して全件 embedding し直します。
- `ccr search [-g] [-l n] [-role user|assistant] [-mode hybrid|keyword|vector] <query>`:
  既定はカレントディレクトリを cwd とする記録のみ(`-g` で全件)。keyword はクエリを
  空白で区切り、CJK を含む語は kagome(IPA 辞書、日本語を含むクエリのときだけ
  読み込む)で形態素解析して内容語を取り出します: 助詞・助動詞・代名詞・非自立名詞・
  軽動詞(する / できる / ある / いる / なる / おる)は捨て、名詞(と接頭詞、名詞に
  続く連用形の動詞)の連続は1語につなぎ、それ以外の動詞・形容詞は表層形と原形の
  共通部分(原形のままなら最後の1文字を除く)にし、それが1文字になるものは除外します。何も残らなければ元の語を
  使い、ダブルクォートで囲んだ部分は解析せずそのまま1語にします。各語をフレーズとして
  AND 検索し(1文字の CJK 語は前方一致)、bm25 順。vector は vec1 の近傍順(絞り込みがある場合は多めに取ってから絞る)。
  hybrid(embedding 設定時の既定)は両者を RRF (k = 60) で統合します。
  embedding が失敗した場合 hybrid は警告を出して keyword のみで続けます。
  結果は日時・発言者・session id(`-g` 時は cwd の basename)の行と、最初の一致
  箇所を中心にした 160 文字の抜粋(端末なら一致部分を強調)を表示します。

## 設定ファイル

`${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json`(環境変数 `CCR_CONFIG` が
指定されていればそのパス)を JSON の設定ファイルとして、`-A` を指定したときだけ
読み込みます。ファイルが無ければ `-A` を付けても従来どおりの動作です。

```json
{"config_dirs": ["~/.claude", "~/.claude-work"]}
```

- `-A` 指定時は `config_dirs` に列挙したディレクトリを `${CLAUDE_CONFIG_DIR}` として追加し、
  セッション一覧・プレビュー・`i` / `v` での session ファイルの探索・実行中
  セッションの検出をすべてのディレクトリに対して横断的に行います。
- 現在のシェルで有効な `${CLAUDE_CONFIG_DIR}` は常に先頭に含めます。先頭の `~`
  と `$VAR` は展開し、絶対パス以外はエラーにします。重複(symlink 先が同じものを
  含む)は1つにまとめ、存在しないディレクトリは無視します。設定ファイルが
  JSON として不正な場合はエラーで終了します。
- 複数のディレクトリを使っている場合のみ、プレビューの `Directory:` の直後に
  `Config: <dir>` としてセッションが見つかったディレクトリを表示します。
- resume 時は、そのセッションが見つかったディレクトリを `CLAUDE_CONFIG_DIR` に
  設定して claude を exec します。現在のシェルと同じディレクトリなら環境は
  そのまま、`${HOME}/.claude` の場合は(claude は変数が未設定のときだけ
  `~/.claude.json` を読むため)`CLAUDE_CONFIG_DIR` を unset します。

## 実行中セッションの検出

`${CLAUDE_CONFIG_DIR}/sessions/<pid>.json` には、実行中またはクリーンに終了
されなかった claude プロセスの情報 (`pid` と `sessionId` を含む) が記録されて
います。ccr はセッション一覧を作成する前にこれらのファイルを読み込み、記載
された `pid` のプロセスが実際に存在し `claude` であることを確認します。存在
しない、または `claude` プロセスでなかった場合はそのファイルの情報を無視し
ます(ファイル自体は削除しません)。有効と判定できたセッション ID について
は、一覧の PID 列に対応するプロセスの pid を表示します。
