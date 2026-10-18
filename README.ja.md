# ccr

[English](README.md)

`ccr` は、どのディレクトリにいても [Claude Code](https://claude.com/claude-code)
の session resume を実行できる Go 言語で書かれた小さな CLI ツールです。
過去のセッション一覧をインタラクティブな picker で表示し、選択したセッションの
元の working directory に `cd` してから、自分自身を `claude --resume <session_id>` に
`exec` で置き換えます。

session ファイルは環境変数 `CLAUDE_CONFIG_DIR` か `$HOME/.claude` から探します。
仕事用・個人用など複数の Claude Code の config ディレクトリを使い分けている場合は、
[設定ファイル](#設定ファイル)に登録して `-A` を付けるとまとめて一覧できます。

デフォルトではカレントディレクトリでの session のみをリストアップしますが、
`-g` オプションを指定すると他のディレクトリの session もリストアップします。

session ファイルから `ai-title` や `last-prompt` の情報も表示されます。

リスト上で `v` をタイプすると HTML に変換した session の情報をウェブブラウザで
確認することができます。

## インストール

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

またはチェックアウトしたリポジトリからビルド:

```sh
go build -o ccr ./cmd/ccr
```

## 使い方

```bash
ccr
```

OR

```bash
ccr -g
```

デフォルトではカレントディレクトリに紐づくセッションのみを対象にします
(Claude Code が `${CLAUDE_CONFIG_DIR}/projects/<dir>` を作る際と同じ規則で、
cwd のうち `a-zA-Z0-9` 以外の文字をすべて `-` に置換して照合します)。
`-g` を付けると全プロジェクトのセッションを対象にします。`-A` を付けると
[設定ファイル](#設定ファイル)に登録したすべての Claude Code の config
ディレクトリのセッションも対象にします(`-g` と組み合わせられます):

ブラウザのない環境(SSH 経由で作業しているサーバーなど)では `-n`
(`-no-browser`)を付けると、`v` がブラウザを開かずトランスクリプトの URL を
表示するだけになります。`CCR_NO_BROWSER=1` でも同じで、そもそもブラウザを
開く手段が無い環境では何も指定しなくてもこの動作になります。詳しくは
[セッション全文の表示(`v` キー)](#セッション全文の表示v-キー)を参照してください。

ターミナルは上下2つの pane に分割されます:

- **上 pane** — セッションを最終更新の新しい順に並べたリスト。列は
  `TIMESTAMP`(ローカルタイム)、`SESSION ID`、`PID`(後述する仕組みで
  実行中と確認できた `claude` プロセスがある場合のみ表示)、`TOKENS`
  (セッション内の assistant メッセージの累計トークン使用量)、`CWD`
  (basename)。両方を表示するには幅が足りない狭いターミナル(95 桁未満)では、
  `CWD` が読めるように session id を先頭 8 文字に短縮します。
  この pane の最下部には使用可能なキーの説明を表示します。
- **下 pane** — カーソルで選択中のセッションのプレビュー。session id 全体
  (上の一覧が短縮していても完全な値)、ディレクトリ、(Claude Code が生成して
  いれば)タイトル、ファイルサイズ、トークン使用量(合計と、in / out /
  cache write / cache read の種別ごとの内訳)、開始/終了時刻、そのセッションで
  送った直近のプロンプトを表示します。

### キー操作

| キー | 動作 |
| --- | --- |
| `↑`/`k`/`p`、`↓`/`j`/`n` | カーソルを移動 |
| `Space`/`PageDown`、`b`/`Backspace`/`PageUp` | 1画面送り / 戻し |
| `g`、`G` | 先頭 / 末尾のセッションへ |
| `/` | 入力に応じて一覧を絞り込み(session id と cwd が対象)|
| `Enter` | 選択したセッションを resume(`cd` してから `exec claude --resume <id>`) |
| `i` | 選択したセッションの生の `jsonl` をターミナル内で確認 |
| `v` | 選択したセッションの全文をブラウザで表示(後述) |
| `q`、`Esc`、`Ctrl-C` | 何もせず終了 |

### 環境変数

- `CLAUDE_CONFIG_DIR` — Claude Code がデータを保存する場所。未設定時は `${HOME}/.claude`。
  [設定ファイル](#設定ファイル)で `-A` 用のディレクトリを追加できます。
- `CCR_HISTORY_DB` — `ccr hook` が記録し `ccr search` が検索するデータベース
  ([後述](#過去のプロンプトと回答の検索ccr-search))。
- `CCR_CONFIG` — ccr の設定ファイルのパス。未設定時は
  `${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json`。
- `BROWSER` — トランスクリプトビューア(後述)を開くコマンド。一般的な慣習に従い、
  いずれかの単語に `%s` が含まれていればそこに URL を埋め込み、無ければ最後の
  引数として URL を追加します。`BROWSER` が未設定の場合、macOS では `open`
  コマンドで開くフォールバックが働きます。それ以外の OS ではフォールバック先が
  無いため、`v` はブラウザを開く代わりに URL を表示します。
- `CCR_NO_BROWSER` — `1`(`0`、`false`、`no`、`off` 以外の値)を設定すると、
  `-n` と同じく `v` がブラウザを開かず URL を表示するだけになります。
- Change Directory 先に .envrc ファイルが存在した場合は direnv exec を使って読み込みます。

## 生の `jsonl` の確認(`i` キー)

`i` を押すと、ターミナルを離れずに選択中セッションのファイルを全画面で
プレビューします。1行につき1行を表示し(ファイル上の行番号、`type`、時刻、
生テキストの先頭)、`n` / `p` で1行移動、space で1画面送り・`b` / Backspace で
1画面戻しでスクロールできます。`/` でインクリメンタルサーチもできます。

絞り込みの操作は session 一覧と共通で、入力すると一致した行だけが残り、
プロンプトに `<一致数>/<全体数>` が出ます。Enter で入力を終えても絞り込みは
維持されるので、そのまま通常のキーで移動・resume・JSON 表示ができます。Esc で
絞り込みを解除し、解除済みの状態で押すとその画面を抜けます。session 一覧では
session id と cwd が、jsonl 一覧では行の生テキスト全体が対象なので、`tool_use` や
ファイルパスでも探せます。もう一度 `i`(または `Enter`)を押すとカーソル行の整形済み
JSON を一覧の上に重なるモーダルとして開きます。Enter が resume を意味するのは
picker のセッション一覧だけなので、この画面では使えます。

モーダル内では `n` / `p` で前後の行に移動して、そのまま次の JSON を表示できます
(いちいち閉じて開き直す必要はありません)。背後の一覧も追随します。
移動キーは一覧と共通で、`n` / `p` が1行、space が1画面送り、`b` / Backspace が
1画面戻しです。背後の一覧は faint になりますが
そのまま見えるので、開いてもファイル内の位置を見失いません。モーダル内は
折り返し表示でスクロールできます。JSON として解釈できない行も一覧に残り、
開くと生のまま表示されます。`q` / `Esc` でモーダルを閉じ、さらに押すと
picker へ戻ります。

## セッション全文の表示(`v` キー)

セッションを選択した状態で `v` を押すと、その jsonl 全体を、light 基調の
配色を持つ自己完結した HTML ページとしてレンダリングします。Human と AI の
発言は視覚的に区別され、通常の発言は Markdown としてレンダリングされ、
コマンドや diff、ファイル内容は syntax highlight されます。sub agent の結果報告や
monitor イベントなど、harness がユーザーに代わって注入した通知は user の行として
記録されますが、Human ではなく `🔔 Notification` として表示します。

tool 呼び出しだけのターンが連続する範囲は `N tool calls` という1つの折り畳みに
まとまり、その中の各実行もさらに折り畳まれた状態で並びます。どちらも個別に
トグルで展開でき、折り畳んだままでも summary に tool と対象(Bash は説明または
コマンド、Read/Edit などはファイルパス)が出るので、開かなくても何をしたか
たどれます。失敗した実行はその行自体に色が付くので、開かなくても気づけます。
文章を含むターンは従来どおり card として表示され、そこで連続が途切れます。

ページ右側の filter pane で表示内容を切り替えられます。メッセージ種別
(Human / Claude / Notification / System と thinking ブロック)と、実際に呼ばれた
ツール(Read / Edit / Bash など)ごとに件数付きの checkbox が並び、`All` / `None`
ボタンで一括切り替えもできます。そのセッションに存在しない種別は行自体が出ません。
ツールを隠した結果、中身が何も残らなくなった message card も一緒に隠れます。
トークン使用量が記録されている assistant のターンは、card のヘッダーの timestamp の
隣に使用量(合計と種別ごとの内訳)を表示します。

jsonl に `cost-state` の行があれば、ヘッダーにそのセッションの金額を表示し、
折り畳みの中に時間の内訳とモデルごとの使用量・金額を並べます。この行を持たない
セッションでは何も表示しません。

各イベントには `{ }` ボタンが付き、押すとその元になった jsonl の行を整形して
表示します。JSON をページに埋め込むと、もともと数 MB ある transcript が倍近くに
なってしまうため、ページが持つのは行の位置と長さだけで、押された時点でサーバーに
その範囲を要求します。

セッションが sub agent を起動していた場合は、その transcript にもリンクします。
起動元の tool 呼び出しの直後、結果報告の Notification card、そして pane の
`Sub agents` の3か所からたどれます。sub agent のページも session と同じ見た目で
表示され、元の session へ戻るリンクが付きます。コマンドの実行
結果や diff、ファイル内容は既定で折り畳まれており、クリックすると開きます。

このページはファイルには書き出しません。`ccr` は小さなローカル HTTP サーバー
(`8000` 番から順に空いている port を探して起動)を立て、
`http://localhost:<port>/<session_id>` へのリクエストごとにその場でセッションを
レンダリングして返し、そのURLを `$BROWSER` で開きます。サーバーは `ccr` の
実行ごとに一度だけ起動され、以降に表示するセッションはすべて同じサーバーを
使い回します。サーバーが起動済みであれば、まだ `v` を押していないセッションでも
プレビュー pane に `Serving at: <url>` と表示されます。トランスクリプトを開いても
picker は終了しません。

### ブラウザのない環境で

`ccr -n`(または `CCR_NO_BROWSER=1`)で起動すると、`v` はサーバーを立てるだけで
何も開かず、picker の最下行に
`serving at http://localhost:<port>/<session_id>` と表示します。`$BROWSER` が
未設定でフォールバックも無い環境では、指定しなくても同じ動作になります。

サーバーは `127.0.0.1` でしか listen しないため、手元のマシンから見るには SSH で
port forward してください:

```bash
ssh -L 8000:localhost:8000 user@server
```

その上で、`ccr` が表示した URL を手元のブラウザで開きます。

## 過去のプロンプトと回答の検索(`ccr search`)

`ccr hook` が送信したプロンプトと Claude の回答をすべて SQLite のデータベースに
記録し、`ccr search` でそれを検索します。キーワード検索(BM25、日本語対応)に加え、
embedding モデルを設定すると意味による検索(SQLite の
[vec1](https://sqlite.org/vec1) 拡張によるベクトル検索)も行い、両者の順位を
統合して表示します。

### 記録

`~/.claude/settings.json`(複数の config ディレクトリを使っている場合はそれぞれの
`settings.json`)で `UserPromptSubmit` と `Stop` の hook に `ccr hook` を登録します:

```json
{
  "hooks": {
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "ccr hook", "async": true }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "ccr hook", "async": true }] }]
  }
}
```

`async` にしておくと、hook(とその中で行う embedding の問い合わせ)がプロンプトの
送信を待たせません。データベースは `${XDG_DATA_HOME:-$HOME/.local/share}/ccr/history.db`
です([設定ファイル](#設定ファイル)の `history.db` か環境変数 `CCR_HISTORY_DB` で
変更できます)。検索できるのは hook を登録した後に記録されたものだけです。

### 検索

```bash
ccr search 全文検索                    # カレントディレクトリのセッションから
ccr search -g sqlite ベクトル          # 全プロジェクトから。すべての語を含むもの
ccr search -g -role user -l 20 hook   # 自分のプロンプトだけを 20 件
```

| オプション | 意味 |
| --- | --- |
| `-g` | カレントディレクトリで実行したセッションに限らず全プロジェクトを検索 |
| `-l n` | 表示件数(既定は 10) |
| `-role user\|assistant` | 自分のプロンプトだけ、または Claude の回答だけ |
| `-mode hybrid\|keyword\|vector` | embedding モデルを設定していれば `hybrid`、無ければ `keyword` が既定 |

結果には日時、発言者、session id(`claude --resume` に渡せます)と、一致した
部分を強調した本文の抜粋を表示します。

キーワード検索はクエリを空白で区切り、すべての語を含むものを探します。クエリ中の
日本語は形態素解析器 [kagome](https://github.com/ikawaha/kagome)(IPA 辞書)で
単語に分け、助詞・助動詞・代名詞などを除くので、`日本語の検索` は「日本語」と
「検索」を両方含むメッセージを見つけます。名詞の連続は1語のまま(`全文検索`)、
動詞・形容詞は活用しても変わらない部分(`変えたい` → `変え`)にします。書いたとおりの
並びで探したいときはダブルクォートで囲んでください。日本語を含む検索では kagome の
辞書の読み込みに1秒ほどかかります。

保存するテキストの索引は別の方法で作ります。日本語(などの CJK)の文字の並びは
2文字ずつずらした bigram にするので、2文字以上の語は(kagome がどう区切るかに
関わらず)部分一致で見つかり、1文字でも検索できます。全角英数字・半角カナ・大文字小文字の
違いは区別しません(索引は ccr 独自の tokenizer で作るため、`sqlite3` コマンドで
データベースは読めますが索引の検索はできません)。

### ベクトル検索

ベクトル検索の既定のモデルは日本語の embedding モデル
[Ruri v3](https://huggingface.co/collections/cl-nagoya/ruri-v3) です。付属の
サーバーを起動し([uv](https://docs.astral.sh/uv/) が必要です。モデルは初回起動時に
Hugging Face からダウンロードされます):

```bash
uv run https://raw.githubusercontent.com/yteraoka/ccr/main/scripts/ruri-embedding-server.py
# リポジトリを clone してあれば: uv run scripts/ruri-embedding-server.py
```

設定ファイルに空の `embedding` セクションを書くとベクトル検索が有効になります:

```json
{
  "history": {
    "embedding": {}
  }
}
```

サーバーは `http://127.0.0.1:8080/v1/embeddings` で `cl-nagoya/ruri-v3-310m` を
提供します。`--model cl-nagoya/ruri-v3-130m`(または `-70m`、`-30m`)で精度と
引き換えに軽くでき、`--port` / `--device` も指定できます。モデルを変えたときは
設定ファイルの `model` も同じものにしてください。ccr は名前に `ruri-v3` を含む
モデルには Ruri v3 が前提とする `検索クエリ: ` / `検索文書: ` の prefix を自動で
付けるので、他の方法(GGUF に変換したものを llama.cpp で、Text Embeddings
Inference で、など)で Ruri v3 を動かしても同じように使えます。

それ以外の OpenAI 互換の embeddings エンドポイント(Ollama、LM Studio、llama.cpp、
OpenAI、Voyage AI)も使えます:

```json
{"history": {"embedding": {"url": "http://localhost:11434/v1/embeddings", "model": "bge-m3"}}}
```

| キー | 意味 |
| --- | --- |
| `url` | エンドポイント。既定は `http://127.0.0.1:8080/v1/embeddings` |
| `model` | モデル。既定は `cl-nagoya/ruri-v3-310m` |
| `api_key_env` | API キーを格納した環境変数名。bearer token として送ります |
| `dimensions` | 対応するモデルでベクトルの次元を短くする |
| `query_prefix`、`document_prefix` | クエリ / 保存するテキストの先頭に付ける文字列。既定は Ruri v3 なら Ruri v3 のもの、それ以外は無し(e5 なら `query: ` / `passage: `)。`""` で Ruri v3 の既定を無効にできます |

hook は記録するたびにそのメッセージを embedding します。できなかったもの(サーバーが
止まっていた、後から embedding を設定したなど)は次の `ccr search` で embedding
します。モデルを変更するとすべて embedding し直します。

## 設定ファイル

`ccr` は省略可能な JSON の設定ファイルを
`${XDG_CONFIG_HOME:-$HOME/.config}/ccr/config.json` から読み込みます(環境変数
`CCR_CONFIG` でパスを変更できます)。`config_dirs` に、セッションを一覧する
Claude Code の config ディレクトリを追加で並べます。設定ファイルは `-A` を
指定したときだけ読み込み、指定しなければ現在の `CLAUDE_CONFIG_DIR` のみを
対象にします。

```json
{
  "config_dirs": [
    "~/.claude",
    "~/.claude-work"
  ]
}
```

- 各エントリは絶対パスで指定します。先頭の `~` と `$VAR` は展開されます。
- 現在のシェルで有効なディレクトリ(`CLAUDE_CONFIG_DIR`、未設定なら
  `$HOME/.claude`)は常に含まれ、先頭になります。重複(symlink 経由で同じ
  ディレクトリを指すものを含む)は1つにまとめ、存在しないディレクトリは
  無視します。
- `-A` を指定すると、すべてのディレクトリのセッションを1つの一覧にまとめます(カレントディレクトリ
  のみのデフォルト表示でも `-g` でも同じ)。複数のディレクトリを使っているときは、
  プレビューにそのセッションがどのディレクトリのものかを `Config:` として表示します。
- `Enter` で resume する際は、そのセッションが見つかったディレクトリを
  `CLAUDE_CONFIG_DIR` に設定して claude を起動するので、元のプロファイルのまま
  続きを再開できます。`$HOME/.claude` の場合は、Claude Code が `~/.claude.json`
  を読むのは変数が未設定のときだけなので、設定する代わりに unset します
  (移動先の `.envrc` が `CLAUDE_CONFIG_DIR` を設定している場合は direnv の値が
  優先されます)。
- 実行中セッションの検出も、すべてのディレクトリの `sessions/` を対象にします。

## 実行中セッションの検出

Claude Code は、実行中またはクリーンに終了しなかったセッションのスナップショット
(`pid` と `sessionId` を含む)を `${CLAUDE_CONFIG_DIR}/sessions/<pid>.json` に
記録しています。`ccr` はセッション一覧を作成する前にこれらのファイルを読み込み、
記載された `pid` が実際に存在し、かつ `claude` プロセスであることを確認します。
古くなった、または一致しないエントリは無視します(ファイル自体は削除しません)。
実行中のプロセスが見つかったセッションについては、一覧の `PID` 列にその pid を
表示します。

## ライセンス

MIT — [LICENSE](LICENSE) を参照してください。
