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

# Remove generated release outputs from earlier builds, including packages for
# other versions and targets. Leave the development binary and unrelated files
# in dist untouched.
release_clean_artifacts() {
  local dist_dir="$1" artifact name
  if [[ -L "$dist_dir" ]]; then
    echo "release output directory must not be a symlink: $dist_dir" >&2
    return 1
  fi
  mkdir -p "$dist_dir"
  rm -rf -- "$dist_dir/.release-build"
  for artifact in "$dist_dir"/hydra-admin-v*; do
    [[ -e "$artifact" || -L "$artifact" ]] || continue
    name="${artifact##*/}"
    if [[ "$name" =~ ^hydra-admin-v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?-((linux|darwin)-(amd64|arm64)(\.tar\.gz(\.sha256)?)?|windows-(amd64|arm64)(\.zip(\.sha256)?)?)$ ]]; then
      rm -rf -- "$artifact"
    fi
  done
}
