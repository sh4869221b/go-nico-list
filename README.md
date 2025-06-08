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
  -n, --concurrency number    number of concurrent requests (default 30)
  -v, --version               version for go-nico-list
```
