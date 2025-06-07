# go-nico-list

Command line tool to get video list from niconico userID

```text
niconico {user}/video url get video list

Usage:
  go-nico-list [flags]

Flags:
  -c, --comment number        lower comment limit number
  -a, --dateafter YYYYMMDD    date YYYYMMDD after (default "10000101")
  -b, --datebefore YYYYMMDD   date YYYYMMDD before (default "99991231")
  -h, --help                  help for go-nico-list
  -t, --tab                   id tab Separated flag
  -u, --url                   output id add url
  -p, --pages number          maximum number of pages to fetch
  -v, --version               version for go-nico-list
```

## インストール

```
go install github.com/sh4869221b/go-nico-list@latest
```

## ビルド

```bash
git clone https://github.com/sh4869221b/go-nico-list.git
cd go-nico-list
go build -o go-nico-list
```

## 使い方

```
go-nico-list <ユーザーIDまたはURL> [flags]
```

### 例

```bash
go-nico-list https://www.nicovideo.jp/user/12345678/video -c 100 -u
```
