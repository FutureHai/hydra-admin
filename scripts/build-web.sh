#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "$root_dir/assets/dist"
find "$root_dir/assets/dist" -mindepth 1 -maxdepth 1 ! -name BUILD_WEB_FIRST.txt -exec rm -rf {} +
cd "$root_dir/web"
rm -rf dist
npm run build
test -f dist/index.html || { echo "web/dist/index.html is missing" >&2; exit 1; }
cp -R dist/. "$root_dir/assets/dist/"
test -f "$root_dir/assets/dist/index.html" || { echo "assets/dist/index.html is missing" >&2; exit 1; }
