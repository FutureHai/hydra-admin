#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$root_dir/scripts/release-targets.sh"
release_init "$@"

cd "$root_dir"
"$root_dir/scripts/build-release.sh" "$release_version" "${release_targets[@]}"

license_dir="$(mktemp -d "$root_dir/dist/.release-licenses.XXXXXX")"
trap 'rm -rf "$license_dir"' EXIT
python3 scripts/collect-licenses.py "$license_dir"

for target in "${release_targets[@]}"; do
  release_name="hydra-admin-v${release_version}-${target}"
  release_dir="$root_dir/dist/$release_name"
  binary_name=hydra-admin
  if [[ "$target" == windows-* ]]; then binary_name+=.exe; fi

  rm -rf "$release_dir"
  mkdir -p "$release_dir"
  cp "dist/.release-build/$target/$binary_name" "$release_dir/$binary_name"
  cp configs/config.example.yaml "$release_dir/config.yaml"
  chmod 0600 "$release_dir/config.yaml"
  cp LICENSE SUPPORTED_HYDRA_VERSIONS.md THIRD_PARTY_NOTICES.md "$release_dir/"
  cp -R "$license_dir" "$release_dir/licenses"
  {
    printf '# Hydra Admin Console — %s release\n\n' "$target"
    cat <<'EOF'
The executable includes the web UI. The target host needs a supported Hydra
Admin API but does not need Node.js, Go or a console database. The example
config contains placeholders and an invalid password hash; replace them
before starting the service.

1. Verify the archive checksum against its adjacent `.sha256` file, then
   verify the files against `SHA256SUMS` in this directory.
2. Run `hydra-admin version` (or `hydra-admin.exe version` on Windows) and
   review `SUPPORTED_HYDRA_VERSIONS.md`.
3. Run `hydra-admin hash-password` in an interactive terminal, then set
   `auth.password_hash`, the console URL and Hydra URLs in `config.yaml`.
4. Restrict access to `config.yaml` and choose private writable log and audit
   paths. On Unix, run `chmod 600 config.yaml`.
5. Run `hydra-admin check --config config.yaml`, then
   `hydra-admin serve --config config.yaml`. On Unix, prefix the executable
   with `./`; on Windows PowerShell, use `.\hydra-admin.exe`.

On Linux, `sha256sum -c SHA256SUMS` verifies the extracted files. On macOS,
use `shasum -a 256 -c SHA256SUMS`. On Windows, use `Get-FileHash` in
PowerShell and compare each result with `SHA256SUMS`.

Keep the Hydra Admin API on a restricted network and expose this console only
through a correctly configured HTTPS proxy. `check` performs read-only
probes; complete target-environment acceptance before production writes.
Linux systemd setup and release guidance are in the
[public repository README](https://github.com/FutureHai/hydra-admin).

The software code is MIT licensed. See LICENSE, THIRD_PARTY_NOTICES.md and
licenses/ for the retained dependency and ORY license material.
EOF
  } > "$release_dir/README.md"

  python3 - "$release_dir" <<'PY'
import hashlib
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
files = sorted(p for p in root.rglob('*') if p.is_file() and p.name != 'SHA256SUMS')
(root / 'SHA256SUMS').write_text(''.join(
    f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(root)}\n'
    for p in files
), encoding='utf-8')
PY

  if [[ "$target" == windows-* ]]; then
    archive="$root_dir/dist/$release_name.zip"
    python3 - "$release_dir" "$archive" <<'PY'
import pathlib
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], 'w', compression=zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(root.rglob('*')):
        if path.is_file():
            archive.write(path, arcname=str(path.relative_to(root.parent)))
PY
  else
    archive="$root_dir/dist/$release_name.tar.gz"
    COPYFILE_DISABLE=1 LC_ALL=C tar --no-xattrs -C "$root_dir/dist" -czf "$archive" "$release_name"
  fi
  python3 - "$archive" <<'PY'
import hashlib
import pathlib
import sys

archive = pathlib.Path(sys.argv[1])
archive.with_name(archive.name + '.sha256').write_text(
    f'{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n',
    encoding='utf-8',
)
PY
  echo "Packaged ${archive#"$root_dir/"}"
done
