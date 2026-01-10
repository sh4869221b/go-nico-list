# go-nico-list

niconico のユーザーページから動画IDを取得するコマンドラインツールです。

## Overview
`nicovideo.jp/user/<id>` のページを1つ以上指定し、コメント数と日付範囲で絞り込み、結果をソートして stdout に出力します。

## Install

### Go install
```bash
go install github.com/sh4869221b/go-nico-list@latest
```

### Prebuilt binaries
事前ビルド済みバイナリは GitHub Releases ページで提供しています。

## Usage

```bash
go-nico-list [nicovideo.jp/user/<id>...] [flags]
```

Examples:

```bash
go-nico-list nicovideo.jp/user/12345
go-nico-list https://www.nicovideo.jp/user/12345/video --url
go-nico-list nicovideo.jp/user/1 nicovideo.jp/user/2 --concurrency 10
go-nico-list --input-file users.txt
cat users.txt | go-nico-list --stdin
```

## Output
- 1行に1つの動画IDを出力します（例: `sm123`）。
- `--url` 指定時は各行に `https://www.nicovideo.jp/watch/` を付与します。
- `--tab` 指定時は各行にタブを付与します。

## Exit status
- `0`: 取得エラーなし（無効入力はスキップされ、出力が空になる場合があります）。
- 非0: 取得が1件でも失敗した場合（取得できたIDは出力されます）。
- 検証エラー（例: `--concurrency < 1`）は非0で終了します。
- 取得中の `context.Canceled` / `context.DeadlineExceeded` は成功として空結果扱いになります。

## Flags

| Flag | Description | Default |
| --- | --- | --- |
| `-c, --comment` | lower comment limit number | `0` |
| `-a, --dateafter` | date `YYYYMMDD` after | `10000101` |
| `-b, --datebefore` | date `YYYYMMDD` before | `99991231` |
| `-t, --tab` | id tab separated flag | `false` |
| `-u, --url` | output id add url | `false` |
| `-n, --concurrency` | number of concurrent requests | `3` |
| `--timeout` | HTTP client timeout | `10s` |
| `--retries` | number of retries for requests | `10` |
| `--input-file` | read inputs from file (newline-separated) | `""` |
| `--stdin` | read inputs from stdin (newline-separated) | `false` |
| `--logfile` | log output file path | `""` |
| `--progress` | force enable progress output | `false` |
| `--no-progress` | disable progress output | `false` |

Notes:
- 入力は引数、`--input-file`、`--stdin` で指定できます（改行区切り）。
- 各入力は `nicovideo.jp/user/<id>` を含む必要があります（スキームは任意）。数字のみや `user/<id>` だけの入力は無効としてスキップされます。
- 結果は stdout、進捗とログは stderr に出力されます。`--logfile` でログ出力先を変更できます。
- `concurrency` または `retries` を 1 未満にすると実行時エラーになります。
- stderr が TTY でない場合は進捗表示を自動で無効化します。`--progress` で強制表示、`--no-progress` で無効化します（優先）。
- 処理後に実行サマリを stderr に出力します（非0終了時も含む）。

## Design
CLI 層とドメインロジックを分離し、テストと保守性を高めています。

- `main.go`: バージョン解決とキャンセル可能なコンテキスト生成。
- `cmd/`: Cobra コマンド定義、フラグ、入出力処理（stdout/stderr分離）。
- `internal/niconico/`: 取得・リトライ・ソートなどのドメインロジックとAPIレスポンス定義。

### Flow
1. CLI がフラグとユーザーIDを解析。
2. `internal/niconico` を呼び出して動画IDを取得・フィルタ。
3. 結果をソートし、stderrに進捗を出しつつstdoutへ出力。

## CI
GitHub Actions は全ブランチの push / pull request で実行され、以下をチェックします。
- `gofmt`（format + diff チェック）
- `go vet ./...`
- `go test -count=1 ./...`
- `go test -race -count=1 ./...`

## Contributing
`CONTRIBUTING.md` を参照してください。

## Release
リリースはタグを作成して GitHub に push することで行います。

1. `vX.Y.Z` の形式でタグを作成します。
2. タグを GitHub に push します。
3. GitHub Actions がリリースワークフローを実行します（gofmt/go vet/go test/go test -race + third-party notices の同期チェック）。
4. GoReleaser が GitHub Release を作成し、成果物をアップロードします。
