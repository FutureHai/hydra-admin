#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$root_dir/scripts/release-targets.sh"
release_init "$@"

cd "$root_dir"
if [[ "${ALLOW_DIRTY_RELEASE:-}" != 1 ]] && [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
  echo "release build requires a clean Git worktree" >&2
  exit 1
fi
if [[ "${ALLOW_UNTAGGED_RELEASE:-}" != 1 ]] && [[ "$(git tag --points-at HEAD | grep -Fx "v$release_version" || true)" != "v$release_version" ]]; then
  echo "release build requires tag v$release_version at HEAD" >&2
  exit 1
fi
"$root_dir/scripts/build-web.sh"
test -s assets/dist/index.html || { echo "assets/dist/index.html is missing" >&2; exit 1; }

commit="${BUILD_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
build_time="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
for target in "${release_targets[@]}"; do
  goos="${target%-*}"
  goarch="${target#*-}"
  binary_name=hydra-admin
  if [[ "$goos" == windows ]]; then binary_name+=.exe; fi
  binary_path="dist/.release-build/$target/$binary_name"
  mkdir -p "$(dirname "$binary_path")"
  rm -f "$binary_path"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags="-s -w -X main.version=$release_version -X main.commit=$commit -X main.buildTime=$build_time" \
    -o "$binary_path" ./cmd/hydra-admin
  test -s "$binary_path" || { echo "release binary is missing: $binary_path" >&2; exit 1; }
  echo "Built $binary_path ($release_version, $commit, $build_time)"
done
