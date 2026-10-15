#!/usr/bin/env bash

# Keep the supported release matrix in one place for both build and packaging.
release_init() {
  release_version="${1:-}"
  if [[ ! "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    echo "usage: $0 <version> [linux-amd64|linux-arm64|darwin-amd64|darwin-arm64|windows-amd64|windows-arm64 ...]" >&2
    return 2
  fi
  shift

  release_targets=("$@")
  if [[ ${#release_targets[@]} -eq 0 ]]; then
    release_targets=(linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64 windows-arm64)
  fi

  local target seen_targets='|'
  for target in "${release_targets[@]}"; do
    case "$target" in
      linux-amd64|linux-arm64|darwin-amd64|darwin-arm64|windows-amd64|windows-arm64) ;;
      *) echo "unsupported release target: $target" >&2; return 2 ;;
    esac
    if [[ "$seen_targets" == *"|$target|"* ]]; then
      echo "duplicate release target: $target" >&2
      return 2
    fi
    seen_targets+="$target|"
  done
}
