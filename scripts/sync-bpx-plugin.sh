#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $(basename "$0") [--lyra-root <path>] [--force] [--config <path>]
USAGE
}

lyra_root=""
force="0"
config_path=""

to_windows_path() {
  local input="$1"
  if [[ "$input" =~ ^[A-Za-z]:[\\/].* ]] || [[ "$input" == \\\\* ]]; then
    printf '%s' "$input"
    return 0
  fi
  wslpath -w "$input"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --lyra-root)
      lyra_root="${2:-}"
      shift 2
      ;;
    --force)
      force="1"
      shift
      ;;
    --config)
      config_path="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ps_script="$script_dir/sync-bpx-plugin.ps1"

if [[ ! -f "$ps_script" ]]; then
  echo "PowerShell sync script not found: $ps_script" >&2
  exit 1
fi

ps_script_win="$(to_windows_path "$ps_script")"

cmd=(powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$ps_script_win")
if [[ -n "$lyra_root" ]]; then
  cmd+=(-LyraRoot "$(to_windows_path "$lyra_root")")
fi
if [[ "$force" == "1" ]]; then
  cmd+=(-Force)
fi
if [[ -n "$config_path" ]]; then
  cmd+=(-ConfigPath "$(to_windows_path "$config_path")")
fi

"${cmd[@]}"
