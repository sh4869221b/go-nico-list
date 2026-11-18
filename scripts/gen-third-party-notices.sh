#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_file="${root_dir}/THIRD_PARTY_NOTICES.md"
go_licenses_version="${GO_LICENSES_VERSION:-v1.6.0}"
# Keep these operating systems in sync with builds.goos in .goreleaser.yaml.
release_goos=(linux windows darwin)
cd "${root_dir}"

cache_dir="${root_dir}/.cache"
export GOMODCACHE="${GOMODCACHE:-${cache_dir}/gomod}"
export GOCACHE="${GOCACHE:-${cache_dir}/go-build}"
export GOPATH="${GOPATH:-${cache_dir}/go}"

mkdir -p "${GOMODCACHE}" "${GOCACHE}" "${GOPATH}"

module_path="$(go list -m -f '{{.Path}}')"
readonly_go_flags="${GOFLAGS:-}"
if [[ "${readonly_go_flags}" != *"-mod="* ]]; then
  readonly_go_flags="${readonly_go_flags:+${readonly_go_flags} }-mod=readonly"
fi

report_dir="$(mktemp -d)"
trap 'rm -rf "${report_dir}"' EXIT
report_template="${report_dir}/template"
report_csv="${report_dir}/report.csv"

# Build a runnable host tool once. Only package discovery is cross-target.
host_os="$(go env GOHOSTOS)"
host_arch="$(go env GOHOSTARCH)"
GOOS="${host_os}" GOARCH="${host_arch}" GOBIN="${report_dir}/bin" \
  GOFLAGS="${readonly_go_flags}" go install "github.com/google/go-licenses@${go_licenses_version}"
license_tool="${report_dir}/bin/go-licenses$(GOOS="${host_os}" GOARCH="${host_arch}" go env GOEXE)"

go_licenses() {
  local target="$1" action="$2"
  shift 2
  GOOS="${target}" GOARCH=amd64 CGO_ENABLED=0 GOFLAGS="${readonly_go_flags}" \
    "${license_tool}" "${action}" --ignore "${module_path}" "$@" ./...
}

cat >"${report_template}" <<'EOF'
{{range .}}{{printf "%s,%s,%s,%s\n" .Name .Version .LicenseName .LicenseURL}}{{end}}
EOF

for target in "${release_goos[@]}"; do
  go_licenses "${target}" report --template "${report_template}" >>"${report_csv}"
done

if [[ -n "${GO_LICENSES_SAVE_PATH:-}" ]]; then
  go_licenses "${release_goos[0]}" save --save_path "${GO_LICENSES_SAVE_PATH}" --force >/dev/null
  for target in "${release_goos[@]:1}"; do
    save_dir="${report_dir}/licenses-${target}"
    go_licenses "${target}" save --save_path "${save_dir}" >/dev/null
    # Shared libraries export the same files on each platform. Allow replacing
    # read-only copies already exported by another target.
    cp -Rf -- "${save_dir}/." "${GO_LICENSES_SAVE_PATH}/"
  done
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

  LC_ALL=C sort -u "${report_csv}" | while IFS=, read -r dep version license reference; do
    [[ -z "${dep}" ]] && continue
    printf '| %s | %s | %s | %s |\n' "${dep}" "${version}" "${license}" "${reference}"
  done
} >"${out_file}"

echo "updated: ${out_file}"
