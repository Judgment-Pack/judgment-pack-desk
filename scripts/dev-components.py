#!/usr/bin/env python3
"""Synchronize development companions from Desk's reviewed component lock.

Runtime, Runner, its source worker, Gateway and adapters are built from fresh
checkouts of the exact locked commits (each checked against its version tag)
into a new cache directory; a running installation or a sibling checkout is
never changed. A verified cache works offline until the lock or recipe changes.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('release_builder', ROOT / 'scripts/package-release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
MANIFEST = 'dev-components.json'
EXECUTABLES = {'jpack', 'jpack-runner', 'jpack-source-worker', 'gateway',
               'gateway-connections', 'adapter-document', 'adapter-drive',
               'adapter-gmail', 'adapter-sources', 'adapter-web', 'adapter-render'}


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()


def identity(plan):
    return {'formatVersion': 1, 'components': plan['components'],
            'platform': platform.system() + '/' + platform.machine(),
            'recipes': {name: digest(ROOT / 'scripts' / name) for name in
                        ('dev-components.py', 'build-bundle.py', 'package-release.py', 'component-releases.py')}}


def verify(directory, expected):
    try:
        if directory.is_symlink():
            return False
        path = directory / MANIFEST
        if path.is_symlink() or path.stat().st_size > 1024 * 1024:
            return False
        record = json.loads(path.read_text())
        if record['identity'] != expected:
            return False
        entries = list(directory.rglob('*'))
        if any(path.is_symlink() for path in entries):
            return False
        files = record['files']
        if not isinstance(files, dict) or not EXECUTABLES | {'gateway-bundle.json'} <= files.keys():
            return False
        for name, checksum in files.items():
            relative = Path(name)
            if relative.is_absolute() or any(part in ('', '.', '..') for part in name.split('/')):
                return False
            target = directory / relative
            if any((directory / parent).is_symlink() for parent in (relative, *relative.parents)):
                return False
            if not target.is_file() or digest(target) != checksum:
                return False
        if {str(path.relative_to(directory)) for path in entries if path.is_file()} != set(files) | {MANIFEST}:
            return False
        if any(not os.access(directory / name, os.X_OK) for name in EXECUTABLES):
            return False
        gateway = json.loads((directory / 'gateway-bundle.json').read_text())
        if gateway['revision'] != expected['components']['gateway']['revision']:
            return False
        if any(files.get(name) != checksum for name, checksum in gateway['files'].items()):
            return False
        return True
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        return False


def stamp(binary):
    """The VCS revision and modified flag Go embedded in a built executable."""
    settings = {}
    for line in subprocess.check_output(['go', 'version', '-m', str(binary)], text=True).splitlines():
        fields = line.strip().split('\t')
        if len(fields) == 2 and fields[0] == 'build' and '=' in fields[1]:
            key, value = fields[1].split('=', 1)
            settings[key] = value
    return settings.get('vcs.revision'), settings.get('vcs.modified')


def build(plan, output):
    # Isolated exact-commit checkouts also retain Go VCS metadata. No archive or
    # module cache is allowed to silently substitute a developer's dirty tree.
    # A non-empty GOFLAGS also overrides one saved with `go env -w` (an empty
    # value would not), so no saved -overlay or -modfile reaches these builds.
    env = dict(os.environ, CGO_ENABLED='0', GOWORK='off', GOFLAGS='-mod=readonly')
    host = subprocess.check_output(['go', 'env', 'GOHOSTOS', 'GOHOSTARCH'], text=True).splitlines()
    env.update(GOOS=host[0], GOARCH=host[1])
    with tempfile.TemporaryDirectory(prefix='desk-component-sources-') as temp:
        sources = {}
        for name, component in plan['components'].items():
            sources[name] = Path(temp) / name
            release.source(component, sources[name])
        subprocess.run([sys.executable, str(ROOT / 'scripts/build-bundle.py'),
                        '--gateway-only', '--gateway-checkout', str(sources['gateway']),
                        '--output', str(output)], check=True, env=env)
        runtime = plan['components']['runtime']
        runner = plan['components']['runner']
        for binary, component, symbol, version in (
            ('jpack', 'runtime', 'github.com/Judgment-Pack/judgment-pack-runtime/internal/result.CLIVersion', runtime['version'].lstrip('v')),
            ('jpack-runner', 'runner', 'github.com/Judgment-Pack/judgment-pack-runner/internal/buildinfo.releaseVersion', runner['version']),
            ('jpack-source-worker', 'runner', 'github.com/Judgment-Pack/judgment-pack-runner/internal/buildinfo.releaseVersion', runner['version']),
        ):
            subprocess.run(['go', 'build', '-trimpath', '-buildvcs=true', '-ldflags', '-X ' + symbol + '=' + version,
                            '-o', str(output / binary), './cmd/' + binary], cwd=sources[component], env=env, check=True)
            if stamp(output / binary) != (plan['components'][component]['revision'], 'false'):
                raise RuntimeError('Built companion does not match its locked source: ' + binary)
        for name in ('runtime', 'runner'):
            release.licenses(sources[name], output / (name + '-licenses'))


def synchronize():
    plan = release.components.read_plan()
    expected = identity(plan)
    key = hashlib.sha256(json.dumps(expected, sort_keys=True).encode()).hexdigest()[:24]
    cache = ROOT / 'bin/dev-components'
    cache.mkdir(parents=True, exist_ok=True)
    for directory in sorted(cache.glob(key + '-*')):
        if verify(directory, expected):
            print('Companions verified: ' + ', '.join(name + ' ' + value['version'] for name, value in plan['components'].items()), flush=True)
            return directory
    print('Synchronizing companions from the component lock…', flush=True)
    with tempfile.TemporaryDirectory(prefix='.building-', dir=cache) as temp:
        output = Path(temp) / 'bundle'
        output.mkdir()
        build(plan, output)
        files = {str(file.relative_to(output)): digest(file) for file in sorted(output.rglob('*')) if file.is_file()}
        (output / MANIFEST).write_text(json.dumps({'identity': expected, 'files': files}, indent=2) + '\n')
        if not verify(output, expected):
            raise RuntimeError('Companion verification failed; the running Desk is unchanged.')
        # A corrupt cache gets a new location; running processes retain their
        # original directory. No pointer is changed until a new Desk is launched.
        destination = cache / (key + '-' + Path(temp).name[len('.building-'):])
        output.rename(destination)
    return destination


if __name__ == '__main__':
    print(synchronize())
