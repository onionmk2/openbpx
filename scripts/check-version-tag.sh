#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "usage: $0 [vX.Y.Z]" >&2
  exit 2
fi

tag="${1:-${GITHUB_REF_NAME:-}}"
if [[ -z "${tag}" ]]; then
  echo "error: tag is required (argument or GITHUB_REF_NAME)" >&2
  exit 2
fi

if [[ "${tag}" != v* ]]; then
  echo "error: tag must start with 'v' (got: ${tag})" >&2
  exit 2
fi

expected="${tag#v}"
actual="$(go run ./cmd/bpx version | tr -d '\r\n')"

if [[ -z "${actual}" ]]; then
  echo "error: bpx version returned empty output" >&2
  exit 1
fi

if [[ "${actual}" != "${expected}" ]]; then
  echo "error: version mismatch detected" >&2
  echo "  release tag : ${tag}" >&2
  echo "  expected    : ${expected}" >&2
  echo "  bpx version : ${actual}" >&2
  exit 1
fi

echo "version check passed: ${actual} == ${expected}"
