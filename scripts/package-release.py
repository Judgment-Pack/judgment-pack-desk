#!/usr/bin/env python3
"""Publishable Desk bundles are built from a clean, tagged tree and the component lock."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('components', ROOT / 'scripts/component-releases.py')
components = importlib.util.module_from_spec(spec); spec.loader.exec_module(components)
spec = importlib.util.spec_from_file_location('published', ROOT / 'scripts/published-components.py')
published = importlib.util.module_from_spec(spec); spec.loader.exec_module(published)

def run(args, cwd=ROOT, **kwargs): return subprocess.run(args, cwd=cwd, check=True, **kwargs)

def source(component, destination):
    run(['git', 'init', '-q', str(destination)])
    run(['git', 'fetch', '--depth=1', 'https://github.com/' + component['repository'] + '.git', component['revision']], destination)
    run(['git', 'checkout', '--detach', 'FETCH_HEAD'], destination)
    actual = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=destination, text=True).strip()
    if actual != component['revision']: raise ValueError('Component checkout disagrees with lock')
    tag = 'refs/tags/' + component['version']
    run(['git', 'fetch', '--depth=1', 'https://github.com/' + component['repository'] + '.git', tag], destination)
    tagged = subprocess.check_output(['git', 'rev-parse', 'FETCH_HEAD^{commit}'], cwd=destination, text=True).strip()
    if tagged != actual: raise ValueError('Component version tag disagrees with locked commit')

def licenses(source_dir, target):
    for file in source_dir.rglob('*'):
        if '.git' not in file.parts and file.is_file() and file.name.upper().startswith(('LICENSE', 'NOTICE', 'COPYING', 'PATENTS', 'THIRD_PARTY_NOTICES')):
            out = target / file.relative_to(source_dir); out.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(file, out)

def validate_release(version, plan):
    if components.stable_version('v' + version) is None: raise ValueError('Expected a stable version without v prefix')
    if any(c['channel'] == 'development' for c in plan['components'].values()): raise ValueError('Publish and promote development component pins before a stable Desk release')
    if subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=normal'], cwd=ROOT): raise ValueError('Release source tree must be clean')
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    tag = subprocess.check_output(['git', 'rev-parse', 'v' + version + '^{commit}'], cwd=ROOT, text=True).strip()
    if tag != commit: raise ValueError('Release tag must name this exact commit')
    return commit

def dependency_licenses(source_dir, target):
    # Build dependencies have already been downloaded. Retain their notices in
    # each component's package, in addition to its own repository license.
    raw = subprocess.check_output(['go', 'list', '-m', '-json', 'all'], cwd=source_dir, text=True)
    decoder = json.JSONDecoder()
    while raw.strip():
        module, size = decoder.raw_decode(raw.lstrip()); raw = raw.lstrip()[size:]
        module = module.get('Replace', module)
        if not module.get('Dir') or module.get('Main'): continue
        licenses(Path(module['Dir']), target / module['Path'])

def web_licenses(target):
    # JS dependencies and bundled fonts retain their redistribution notices.
    lock = json.loads((ROOT / 'web/package-lock.json').read_text())
    for name in lock['packages']:
        if not name: continue
        folder = ROOT / 'web' / name
        if not folder.is_dir(): continue
        for file in folder.iterdir():
            if file.is_file() and file.name.upper().startswith(('LICENSE', 'NOTICE', 'COPYING', 'PATENTS', 'OFL')):
                out = target / name / file.name
                out.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(file, out)
    for file in (ROOT / 'web/public/fonts').iterdir():
        if file.is_file() and file.suffix == '.txt':
            out = target / 'fonts' / file.name
            out.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(file, out)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('version'); parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(); plan = components.read_plan()
    commit = validate_release(args.version, plan)
    os_name = {'Linux': 'linux', 'Darwin': 'darwin'}[platform.system()]
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
    output = args.output.resolve(); output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='desk-release-') as tmp:
        tmp = Path(tmp); bundle = tmp / 'bundle'; bundle.mkdir()
        components.verify_plan(plan)
        published.install(plan, os_name + '/' + arch, bundle, tmp / 'downloads')
        env = dict(os.environ, CGO_ENABLED='0', GOWORK='off', GOOS=os_name, GOARCH=arch)
        run(['npm', '--prefix', 'web', 'run', 'build'])
        run(['go', 'build', '-trimpath', '-ldflags', '-X github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan.Version=' + args.version, '-o', str(bundle / 'jpack-desk'), '.'], env=env)
        notices = bundle / 'desk-licenses'
        notices.mkdir(parents=True)
        shutil.copyfile(ROOT / 'LICENSE', notices / 'LICENSE')
        dependency_licenses(ROOT, notices / 'dependencies')
        web_licenses(bundle / 'web-licenses')
        shutil.copyfile(ROOT / 'scripts/desk-update.py', bundle / 'desk-update.py')
        shutil.copyfile(ROOT / 'LICENSE', bundle / 'LICENSE')
        shutil.copyfile(ROOT / 'docs/updates.md', bundle / 'START-HERE.md')
        manifest = {'formatVersion': 1, 'stateEpoch': plan['stateEpoch'], 'version': args.version, 'platform': os_name + '/' + arch,
                    'desk': {'repository': 'https://github.com/Judgment-Pack/judgment-pack-desk', 'revision': commit},
                    **plan['components'], 'files': {str(p.relative_to(bundle)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(bundle.rglob('*')) if p.is_file()}}
        (bundle / 'release-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        asset = output / ('judgment-pack-desk_' + args.version + '_' + os_name + '_' + arch + '.tar.gz')
        with tarfile.open(asset, 'w:gz') as archive:
            for item in sorted(bundle.iterdir()): archive.add(item, arcname=item.name)
        published_manifest = output / ('release-manifest_' + os_name + '_' + arch + '.json')
        shutil.copyfile(bundle / 'release-manifest.json', published_manifest)
        (output / 'checksums.txt').write_text('\n'.join(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name for p in (asset, published_manifest)) + '\n')
if __name__ == '__main__': main()
