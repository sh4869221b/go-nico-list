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
                           (recommended 30, minimum 1)
      --timeout duration      HTTP client timeout (default "10s")
  --retries number        number of retries for requests (default 100)
                           (recommended 100, minimum 1)
  -v, --version               version for go-nico-list
```

The recommended values are `30` for `concurrency` and `100` for `retries`. Setting these to a value less than 1 will result in an error.
