#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root_dir"
"$root_dir/scripts/build-web.sh"
temp_dir="$(mktemp -d)"
cleanup() {
  kill -TERM "${proxy_pid:-}" "${admin_pid:-}" "${mock_pid:-}" 2>/dev/null || true
  wait "${proxy_pid:-}" "${admin_pid:-}" "${mock_pid:-}" 2>/dev/null || true
  rm -rf "$temp_dir"
}
trap cleanup EXIT

go build -o "$temp_dir/hydra-admin" ./cmd/hydra-admin
go build -o "$temp_dir/mock-hydra" ./tests/e2e/mockhydra
go build -o "$temp_dir/proxy" ./tests/e2e/proxy
cp tests/e2e/config.yaml "$temp_dir/config.yaml"
"$temp_dir/mock-hydra" >"$temp_dir/mock.out" 2>&1 & mock_pid=$!
for i in {1..50}; do
  if [[ "$(curl -fsS http://127.0.0.1:14445/__fixture 2>/dev/null || true)" == hydra-admin-e2e-mock ]]; then break; fi
  if ! kill -0 "$mock_pid" 2>/dev/null; then cat "$temp_dir/mock.out"; exit 1; fi
  sleep 0.2
done
[[ "$(curl -fsS http://127.0.0.1:14445/__fixture 2>/dev/null || true)" == hydra-admin-e2e-mock ]] || { echo "mock Hydra did not start" >&2; exit 1; }
"$temp_dir/hydra-admin" serve --config "$temp_dir/config.yaml" >"$temp_dir/admin.out" 2>&1 & admin_pid=$!
for i in {1..50}; do
  if curl -fsS http://127.0.0.1:18085/healthz >/dev/null 2>&1; then break; fi
  if ! kill -0 "$admin_pid" 2>/dev/null; then cat "$temp_dir/admin.out"; exit 1; fi
  sleep 0.2
done
curl -fsS http://127.0.0.1:18085/healthz >/dev/null || { cat "$temp_dir/admin.out"; exit 1; }
kill -0 "$admin_pid" 2>/dev/null || { cat "$temp_dir/admin.out"; exit 1; }
"$temp_dir/proxy" >"$temp_dir/proxy.out" 2>&1 & proxy_pid=$!
for i in {1..50}; do
  if curl -fsS http://127.0.0.1:18086/healthz >/dev/null 2>&1 && curl -fsS http://127.0.0.1:18087/healthz >/dev/null 2>&1; then break; fi
  if ! kill -0 "$proxy_pid" 2>/dev/null; then cat "$temp_dir/proxy.out"; exit 1; fi
  sleep 0.2
done
curl -fsS http://127.0.0.1:18086/healthz >/dev/null && curl -fsS http://127.0.0.1:18087/healthz >/dev/null || { cat "$temp_dir/proxy.out"; exit 1; }
(
  cd web
  if [[ "$(uname -s)" == Darwin && -z "${PLAYWRIGHT_CHROME_PATH:-}" ]]; then
    export PLAYWRIGHT_CHROME_PATH="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  fi
  npm run test:e2e
)
