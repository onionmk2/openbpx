#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "usage: $0 [vX.Y.Z|X.Y.Z]" >&2
  exit 2
fi

raw="${1:-${GITHUB_REF_NAME:-}}"
if [[ -z "${raw}" ]]; then
  echo "error: version is required (argument or GITHUB_REF_NAME)" >&2
  exit 2
fi

version="${raw#v}"
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: expected semantic version (got: ${raw})" >&2
  exit 2
fi

if [[ ! -f CHANGELOG.md ]]; then
  echo "error: CHANGELOG.md not found" >&2
  exit 1
fi

if ! grep -Eq "^## \\[${version//./\\.}\\](\\s|$)" CHANGELOG.md; then
  echo "error: CHANGELOG.md is missing section for ${version}" >&2
  echo "expected heading: ## [${version}]" >&2
  exit 1
fi

section="$(
  awk -v v="${version}" '
    $0 ~ "^## \\[" v "\\](\\s|$)" { in_section = 1; next }
    in_section && $0 ~ "^## \\[" { exit }
    in_section { print }
  ' CHANGELOG.md
)"

if ! grep -Eq '^[[:space:]]*-[[:space:]]+[^[:space:]]' <<<"${section}"; then
  echo "error: CHANGELOG.md section ${version} has no bullet entries" >&2
  echo "add at least one line like: - Your change summary." >&2
  exit 1
fi

echo "changelog check passed: ${version}"
