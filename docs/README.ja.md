# go-nico-list

niconico のユーザーページとマイリストから動画IDを取得するコマンドラインツールです。

## Overview
`nicovideo.jp/user/<id>` または `nicovideo.jp/mylist/<id>` のページを1つ以上指定し、コメント数と日付範囲で絞り込み、結果をソートして stdout に出力します。

## Install

### Go install
```bash
go install github.com/sh4869221b/go-nico-list@latest
```

### Prebuilt binaries
事前ビルド済みバイナリは GitHub Releases ページで提供しています。

## Usage

```bash
go-nico-list [nicovideo.jp/user/<id>|nicovideo.jp/mylist/<id>...] [flags]
```

Examples:

```bash
go-nico-list nicovideo.jp/user/12345
go-nico-list https://www.nicovideo.jp/user/12345/video --url
go-nico-list nicovideo.jp/user/1 nicovideo.jp/mylist/847130 --concurrency 10
go-nico-list --input-file users.txt
cat users.txt | go-nico-list --stdin
```

## Output
- 1行に1つの動画IDを出力します（例: `sm123`）。
- `--url` 指定時は各行に `https://www.nicovideo.jp/watch/` を付与します。
- `--json` 指定時は stdout に単一の JSON オブジェクトを出力します（行出力は無効化）。

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
| `-u, --url` | output id add url | `false` |
| `-n, --concurrency` | number of concurrent requests | `3` |
| `--page-concurrency` | number of concurrent page requests per target | `1` |
| `--http-concurrency` | コマンド全体の HTTP 同時数の上限（0 は追加上限なし） | `0` |
| `--adaptive-http-concurrency` | HTTP 同時数を自動調整（`--http-concurrency` の上限指定は任意） | `false` |
| `--http-metrics` | log aggregate HTTP performance metrics | `false` |
| `--rate-limit` | maximum requests per second (0 disables) | `0` |
| `--min-interval` | minimum interval between requests | `0s` |
| `--timeout` | HTTP client timeout | `10s` |
| `--retries` | number of retries for requests | `10` |
| `--input-file` | read inputs from file (newline-separated) | `""` |
| `--stdin` | read inputs from stdin (newline-separated) | `false` |
| `--logfile` | log output file path | `""` |
| `--progress` | force enable progress output | `false` |
| `--no-progress` | disable progress output | `false` |
| `--strict` | return non-zero if any input is invalid | `false` |
| `--best-effort` | always exit 0 while logging fetch errors | `false` |
| `--dedupe` | remove duplicate output IDs before output | `false` |
| `--no-sort` | skip sorting output IDs for faster output | `false` |
| `--json` | emit JSON output to stdout | `false` |

Notes:
- 入力は引数、`--input-file`、`--stdin` で指定できます（改行区切り）。
- 各入力は `nicovideo.jp/user/<id>` または `nicovideo.jp/mylist/<id>` を含む必要があります（スキームは任意）。数字のみやドメインなしのパスだけの入力は無効としてスキップされます。
- 結果は stdout、進捗とログは stderr に出力されます。`--logfile` でログ出力先を変更できます。
- `concurrency`、`page-concurrency`、`retries` を 1 未満にするか、`timeout` を 0 以下にすると実行時エラーになります。
- 各ターゲットは、ユーザーが設定できるページ数・動画数の上限なしで、API の自然な終了条件まで取得されます。
- API が `totalCount` を返す場合は、1ページ目から取得対象ページ範囲を確定し、残りのページを `--page-concurrency` の範囲で並列取得します。`totalCount` がない場合は、空ページまたは HTTP 404 まで逐次取得します。
- 代替の取得上限はありません。そのため、大規模なターゲットでは実行時間、リクエスト数、出力量が増える可能性があります。グローバルなレート制限、リトライ処理、コンテキストキャンセルは引き続き適用されます。
- 200/404 以外の HTTP ステータスがリトライ後も続く場合は取得エラー扱いになります。
- HTTP 200 でも `meta.status != 200` の場合は警告ログを出しつつ処理を続行します。
- `--page-concurrency` は、API が `totalCount` を返す場合にのみ、各入力ターゲット内のページ取得並列数を制御します。その bounded-page path での最大同時リクエスト数の目安は `--concurrency * --page-concurrency` です。
- すべてのリクエスト（リトライ含む）に対してレート制限が適用され、HTTP 429 の `Retry-After` は可能な限り尊重されます。高い並列数を使う場合は API 負荷を抑えるため `--rate-limit` または `--min-interval` を併用してください。
- stderr が TTY でない場合は進捗表示を自動で無効化します。`--progress` で強制表示、`--no-progress` で無効化します（優先）。
- 処理後に実行サマリを stderr に出力します（非0終了時も含む）。
- `--strict` を指定すると、無効な入力がある場合に非0で終了します（有効な結果は出力されます）。
- `--best-effort` を指定すると取得エラーがあっても終了コードは 0 になります（エラーはログに残ります）。
- 通常の行出力は、`--no-sort` を指定しない限り動画IDの数値順にソートします。
- `--dedupe` を指定すると動画IDの重複を除外してからソート/出力します。`--no-sort` 併用時は writer に先に到着した occurrence を採用します。
- `--no-sort` は行出力向けの unordered fast mode です。入力ターゲット順、ページ順、API items 順は保証されず、取得完了した結果から出力されます。
- `--json` は stdout に単一の JSON オブジェクトを出力します。`--url` は JSON の `items` に影響せず、サマリは引き続き stderr に出力します。

## HTTP 性能計測

```bash
# 既存設定のまま計測する（HTTP の追加上限なし）。
go-nico-list --input-file users.txt --http-metrics

# 全ターゲット・ページ・リトライで共有する固定上限を設定する。
go-nico-list --input-file users.txt -n 8 --page-concurrency 4 \
  --http-concurrency 8 --http-metrics --logfile run.jsonl
```

`--http-concurrency 0` は追加上限なし、正数は HTTP の共有上限、負数はエラーです。本文の読み取りと close が終わるまで枠を保持し、リトライの待機前に解放します。既存のレート制限も維持します。`--adaptive-http-concurrency` を指定しない場合は固定上限です。ターゲット・ページの並列設定を自動的に増やすものではありません。

`--http-metrics` は初期化済みの実行終了時に `http_metrics` の集約ログを1件出力します。出力先は stderr、または既存の `--logfile` です。試行・実送信した再試行・ステータス・エラー・接続再利用・同時数・待機・通信・本文・decode の時間を確認できます。stdout、結果 JSON、summary、終了コードは変更しません。計測ログに URL、対象ID、本文は含めません（既存のエラーログは従来どおりです）。

分位点は各計測の直近最大1024件から求めます。p95 は20件以上、p99 は100件以上の場合のみ表示します。区間は重なるため合計して実行時間として扱わないでください。[計測定義・ローカルベンチマーク](HTTP_METRICS.md)も参照してください。

## 実験的な HTTP 同時数の自動調整

```bash
go-nico-list --input-file users.txt -n 16 --page-concurrency 8 \
  --adaptive-http-concurrency --http-metrics
```

自動調整では `--http-concurrency` を省略するか `0` にすると、設定上の上限なしで動作します。同時数は8から開始し、その時点の正数の同時実行制限を自動調整します。正数を指定すれば任意の厳格な上限として機能し、8未満ならその値から開始します。十分な待機リクエストがあり正常な応答が続く場合は徐々に増やし、遅延やリトライ対象のエラーが継続する場合は減らします。ターゲット・ページの並列数、レート制限、最小間隔は変更しません。ワーカーが少ない場合、レート制限がある場合、短い実行では同時数が増えないことがあります。実行時間の短縮を保証する機能ではありません。

設定上の上限を外しても、代わりの隠れた上限やメモリ・ファイルディスクリプタの保護機構は追加しません。実際の同時数は利用可能なワーカーにも制限されますが、メモリ不足やファイルディスクリプタ枯渇の防止を保証するものではありません。

自動調整時に 429 を受けると、適用される `Retry-After` とリトライ期限に従いコマンド全体の新規送信を一時停止します。送信済みの処理は中断せず、再開時は送信間隔を設けて徐々に回復します。固定モードのリクエスト単位のリトライ動作は維持します。出力、フィルタ、部分結果、終了動作は変更しません。

`--http-metrics` なしでも自動調整は動作します。調整状態や判断理由を最終 `http_metrics` ログに追加するのは、この計測フラグを指定した場合だけです。状態は実行ごとに独立し、バックグラウンドの試験リクエストや学習状態の永続化は行いません。実験的なオプトイン機能であり、ローカルの合成ベンチマークから実 API の最適値を判断することはできません。

自動調整の詳細・評価・制約は [ADAPTIVE_HTTP.md](ADAPTIVE_HTTP.md) を参照してください。

## Design
CLI 層とドメインロジックを分離し、テストと保守性を高めています。

- `main.go`: バージョン解決とキャンセル可能なコンテキスト生成。
- `cmd/`: Cobra コマンド定義、フラグ、入出力処理（stdout/stderr分離）。
- `internal/niconico/`: 取得・リトライ・ソートなどのドメインロジックとAPIレスポンス定義。

### Flow
1. CLI がフラグとユーザーID/マイリストIDを解析。
2. `internal/niconico` を呼び出して動画IDを取得・フィルタ。
3. 結果をソートし、stderrに進捗を出しつつstdoutへ出力。

## CI
GitHub Actions は全ブランチの push / pull request で実行され、以下をチェックします。
- `gofmt`（format + diff チェック）
- `go vet ./...`
- `golangci-lint run ./...`
- `go test -count=1 ./...`
- `go test -race -count=1 ./...`

## Contributing
`CONTRIBUTING.md` を参照してください。

## Release
リリースはタグを作成して GitHub に push することで行います。

1. `vX.Y.Z` の形式でタグを作成します。
2. タグを GitHub に push します。
3. GitHub Actions がリリースワークフローを実行します（`go mod tidy`/`go generate ./...` の検証、gofmt/go vet/golangci-lint/go test/go test -race）。
4. GoReleaser が `THIRD_PARTY_NOTICES.md` を生成し、GitHub Release を作成して成果物をアップロードします。
5. リリースワークフロー成功後、マイルストーンを閉じます。

注記:
- バージョン付きマイルストーンが完了したら同じバージョン番号でリリースします。
