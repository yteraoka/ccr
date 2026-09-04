# ccr

[English](README.md)

`ccr` は、どのディレクトリにいても [Claude Code](https://claude.com/claude-code)
の session resume を実行できる Go 言語で書かれた小さな CLI ツールです。
過去のセッション一覧をインタラクティブな picker で表示し、選択したセッションの
元の working directory に `cd` してから、自分自身を `claude --resume <session_id>` に
`exec` で置き換えます。

session ファイルは環境変数 `CLAUDE_CONFIG_DIR` か `$HOME/.claude` から探します。

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
`-g` を付けると全プロジェクトのセッションを対象にします:

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
| `↑`/`k`、`↓`/`j` | カーソルを移動 |
| `Enter` | 選択したセッションを resume(`cd` してから `exec claude --resume <id>`) |
| `v` | 選択したセッションの全文をブラウザで表示(後述) |
| `q`、`Esc`、`Ctrl-C` | 何もせず終了 |

### 環境変数

- `CLAUDE_CONFIG_DIR` — Claude Code がデータを保存する場所。未設定時は `${HOME}/.claude`。
- `BROWSER` — トランスクリプトビューア(後述)を開くコマンド。一般的な慣習に従い、
  いずれかの単語に `%s` が含まれていればそこに URL を埋め込み、無ければ最後の
  引数として URL を追加します。`BROWSER` が未設定の場合、macOS では `open`
  コマンドで開くフォールバックが働きます。それ以外の OS では、現時点では
  未設定だとエラーになります。
- Change Directory 先に .envrc ファイルが存在した場合は direnv exec を使って読み込みます。

## セッション全文の表示(`v` キー)

セッションを選択した状態で `v` を押すと、その jsonl 全体を、light 基調の
配色を持つ自己完結した HTML ページとしてレンダリングします。Human と AI の
発言は視覚的に区別され、通常の発言は Markdown としてレンダリングされ、
コマンドや diff、ファイル内容は syntax highlight されます。sub agent の結果報告や
monitor イベントなど、harness がユーザーに代わって注入した通知は user の行として
記録されますが、Human ではなく `🔔 Notification` として表示します。

ページ右側の filter pane で表示内容を切り替えられます。メッセージ種別
(Human / Claude / Notification / System と thinking ブロック)と、実際に呼ばれた
ツール(Read / Edit / Bash など)ごとに件数付きの checkbox が並び、`All` / `None`
ボタンで一括切り替えもできます。そのセッションに存在しない種別は行自体が出ません。
ツールを隠した結果、中身が何も残らなくなった message card も一緒に隠れます。
トークン使用量が記録されている assistant のターンは、card のヘッダーの timestamp の
隣に使用量(合計と種別ごとの内訳)を表示します。

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
