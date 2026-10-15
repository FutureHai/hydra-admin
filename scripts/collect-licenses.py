#!/usr/bin/env python3
"""Copy dependency license/notice files from pinned Go and npm installations."""

import json
import hashlib
import pathlib
import re
import shutil
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
DEST = pathlib.Path(sys.argv[1]).resolve()
DEST.mkdir(parents=True, exist_ok=True)
PINNED_README_NOTICES = {
    ('npm', 'agent-base', '6.0.2'): 'f1425c3b72330fe4fb2aa5a2fb152e939bdf534692a32b5f0b38f74147b98556',
    ('npm', 'https-proxy-agent', '5.0.1'): '32f0856d2c43df7d05cca960fdee84e1e38ab545bd7b2186433dfa41aa90a712',
    ('npm', 'is-mobile', '5.0.0'): '1f1f35b6a103d7b010f51a6d7d61e3e47dbc71db2c69cbb7c0ffd40c641b04e2',
}


def safe(value):
    return re.sub(r'[^A-Za-z0-9._-]+', '_', value)


def notices(folder):
    return sorted(p for p in folder.iterdir() if p.is_file() and p.name.lower().startswith(('license', 'licence', 'copying', 'notice')))


manifest = ['ecosystem\tpackage\tversion\tdeclared_license\ttext_files']


def copy_texts(ecosystem, package, version, license_id, folder):
    found = notices(folder)
    readme_digest = PINNED_README_NOTICES.get((ecosystem, package, version))
    if not found and readme_digest:
        # These exact npm releases put their complete MIT text in README.md.
        candidate = folder / 'README.md'
        body = candidate.read_bytes()
        if hashlib.sha256(body).hexdigest() != readme_digest:
            raise SystemExit(f'pinned README notice digest mismatch: {package}@{version}')
        if not all(marker in body for marker in (b'Permission is hereby granted', b'THE SOFTWARE IS PROVIDED', b'Copyright')):
            raise SystemExit(f'README has no complete MIT notice: {package}@{version}')
        found.append(candidate)
    if not found and (ecosystem, package, version) == ('npm', '@ant-design/icons-svg', '4.6.0'):
        upstream = ROOT / 'third_party_licenses' / 'ant-design-icons-LICENSE'
        expected = '5d367fb0a07340571542eb4ee8eb1add62d37b71bad6786a57c2e9a86bf76c70'
        if hashlib.sha256(upstream.read_bytes()).hexdigest() != expected:
            raise SystemExit('pinned Ant Design Icons LICENSE digest mismatch')
        found.append(upstream)
    if not found:
        raise SystemExit(f'license notice unavailable: {ecosystem} {package}@{version}')
    copied = []
    for file in found:
        name = f'{safe(package)}@{safe(version)}-{safe(file.name)}'
        target = DEST / ecosystem / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(file, target)
        copied.append(str(target.relative_to(DEST)))
    manifest.append('\t'.join((ecosystem, package, version, license_id or 'UNDECLARED', ','.join(copied))))


stream = subprocess.check_output(['go', 'list', '-deps', '-json', './cmd/hydra-admin'], cwd=ROOT, text=True)
decoder = json.JSONDecoder()
offset = 0
modules = {}
while offset < len(stream):
    while offset < len(stream) and stream[offset].isspace():
        offset += 1
    if offset == len(stream):
        break
    package, offset = decoder.raw_decode(stream, offset)
    module = package.get('Module')
    if module and module.get('Path') != 'hydra-admin':
        modules[(module['Path'], module.get('Version', 'local'))] = module.get('Dir')
for (path, version), directory in sorted(modules.items()):
    if not directory:
        raise SystemExit(f'Go module unavailable: {path}')
    copy_texts('go', path, version, '', pathlib.Path(directory))

lock = json.loads((ROOT / 'web/package-lock.json').read_text(encoding='utf-8'))
for package_path, meta in sorted(lock['packages'].items()):
    if not package_path or meta.get('dev'):
        continue
    folder = ROOT / 'web' / package_path
    if not folder.is_dir():
        # Platform-specific optional packages are absent on this build host.
        if meta.get('optional'):
            continue
        raise SystemExit(f'npm dependency unavailable: {package_path}; run npm ci')
    name = package_path.removeprefix('node_modules/')
    copy_texts('npm', name, meta.get('version', 'unknown'), meta.get('license', ''), folder)

shutil.copyfile(ROOT / 'specs/v26.2.0/LICENSE', DEST / 'ORY_HYDRA_OPENAPI_LICENSE')
shutil.copyfile(ROOT / 'third_party_licenses' / 'SOURCE.md', DEST / 'UPSTREAM_SOURCES.md')
(DEST / 'MANIFEST.tsv').write_text('\n'.join(manifest) + '\n', encoding='utf-8')
