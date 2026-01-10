#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_file="${root_dir}/THIRD_PARTY_NOTICES.md"

cache_dir="${root_dir}/.cache"
export GOMODCACHE="${cache_dir}/gomod"
export GOCACHE="${cache_dir}/go-build"

mkdir -p "${GOMODCACHE}" "${GOCACHE}"

go mod download -json all >/dev/null

detect_license() {
  local module_dir="$1"
  local license_files

  mapfile -t license_files < <(find "${module_dir}" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) | sort || true)
  if [[ "${#license_files[@]}" -eq 0 ]]; then
    printf '%s' "Unknown (no LICENSE file found in module source at this version)"
    return 0
  fi

  local has_mit=0
  local has_apache=0
  local has_isc=0
  local has_bsd2=0
  local has_bsd3=0
  local head_text

  for license_file in "${license_files[@]}"; do
    head_text="$(sed -n '1,80p' "${license_file}" | tr -d '\r')"

    if grep -qi 'Apache License' <<<"${head_text}"; then
      has_apache=1
    fi
    if grep -qi 'MIT License' <<<"${head_text}"; then
      has_mit=1
    fi
    if grep -qi 'ISC License' <<<"${head_text}"; then
      has_isc=1
    fi
    if grep -qi 'Simplified BSD' <<<"${head_text}"; then
      has_bsd2=1
    fi
    if grep -qi 'Redistribution and use' <<<"${head_text}"; then
      if grep -qi 'Neither the name' <<<"${head_text}" || grep -qi 'contributors may not be used' <<<"${head_text}"; then
        has_bsd3=1
      else
        has_bsd2=1
      fi
    fi
    if grep -qi 'covered by two different licenses' <<<"${head_text}" && grep -qi 'apache' <<<"${head_text}" && grep -qi 'mit' <<<"${head_text}"; then
      has_mit=1
      has_apache=1
    fi
  done

  if [[ "${has_mit}" -eq 1 && "${has_apache}" -eq 1 ]]; then
    printf '%s' "MIT OR Apache-2.0"
    return 0
  fi
  if [[ "${has_apache}" -eq 1 ]]; then
    printf '%s' "Apache-2.0"
    return 0
  fi
  if [[ "${has_isc}" -eq 1 ]]; then
    printf '%s' "ISC"
    return 0
  fi
  if [[ "${has_mit}" -eq 1 ]]; then
    printf '%s' "MIT"
    return 0
  fi
  if [[ "${has_bsd3}" -eq 1 ]]; then
    printf '%s' "BSD-3-Clause"
    return 0
  fi
  if [[ "${has_bsd2}" -eq 1 ]]; then
    printf '%s' "BSD-2-Clause"
    return 0
  fi

  printf '%s' "Unknown"
}

{
  cat <<'EOF'
# Third-Party Notices

This project is licensed under the MIT License (see `LICENSE`).

The following list covers third-party Go modules used by this repository (direct and transitive).
For each dependency, refer to the upstream license text via the reference link.

Note: this is provided for convenience and is not legal advice.

## Dependencies

| Module | Version | License | Reference |
| --- | --- | --- | --- |
EOF

  go list -m -f '{{if not .Main}}{{.Path}} {{.Version}}{{end}}' all | sort | while read -r mod version; do
    [[ -z "${mod}" ]] && continue
    [[ -z "${version}" ]] && continue

    module_dir="${GOMODCACHE}/${mod}@${version}"
    license="$(detect_license "${module_dir}")"
    ref="https://pkg.go.dev/${mod}@${version}?tab=licenses"

    printf '| %s | %s | %s | %s |\n' "${mod}" "${version}" "${license}" "${ref}"
  done
} >"${out_file}"

echo "updated: ${out_file}"
