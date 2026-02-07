#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_file="${root_dir}/THIRD_PARTY_NOTICES.md"
go_licenses_version="${GO_LICENSES_VERSION:-v1.6.0}"

cache_dir="${root_dir}/.cache"
export GOMODCACHE="${cache_dir}/gomod"
export GOCACHE="${cache_dir}/go-build"
export GOPATH="${cache_dir}/go"

mkdir -p "${GOMODCACHE}" "${GOCACHE}" "${GOPATH}"

module_path="$(go list -m -f '{{.Path}}')"
readonly_go_flags="${GOFLAGS:-}"
if [[ "${readonly_go_flags}" != *"-mod="* ]]; then
  if [[ -n "${readonly_go_flags}" ]]; then
    readonly_go_flags="${readonly_go_flags} -mod=readonly"
  else
    readonly_go_flags="-mod=readonly"
  fi
fi
report_template="$(mktemp)"
report_csv="$(mktemp)"
trap 'rm -f "${report_template}" "${report_csv}"' EXIT

cat >"${report_template}" <<'EOF'
{{range .}}{{printf "%s,%s,%s,%s\n" .Name .Version .LicenseName .LicenseURL}}{{end}}
EOF

GOFLAGS="${readonly_go_flags}" go run "github.com/google/go-licenses@${go_licenses_version}" report \
  --ignore "${module_path}" \
  --template "${report_template}" \
  ./... >"${report_csv}"

if [[ -n "${GO_LICENSES_SAVE_PATH:-}" ]]; then
  GOFLAGS="${readonly_go_flags}" go run "github.com/google/go-licenses@${go_licenses_version}" save \
    --ignore "${module_path}" \
    --save_path "${GO_LICENSES_SAVE_PATH}" \
    --force \
    ./... >/dev/null
fi

{
  cat <<'EOF'
# Third-Party Notices

This project is licensed under the MIT License (see `LICENSE`).

The following list covers third-party Go dependencies used by this repository
(excluding test-only dependencies by default).
For each dependency, refer to the upstream license text via the reference link.

Note: this is provided for convenience and is not legal advice.

## Dependencies

| Dependency | Version | License | Reference |
| --- | --- | --- | --- |
EOF

  sort "${report_csv}" | while IFS=, read -r dep version license reference; do
    [[ -z "${dep}" ]] && continue
    printf '| %s | %s | %s | %s |\n' "${dep}" "${version}" "${license}" "${reference}"
  done
} >"${out_file}"

echo "updated: ${out_file}"
