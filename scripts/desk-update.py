#!/usr/bin/env python3
"""Managed Desk installation. Updates are applied only before Desk starts.

python3 desk-update.py install
python3 ~/.local/share/jpack-desk-install/desk-update.py run -- /path/to/desk

Only immutable, verified release directories are executed. Neither source
checkouts nor Desk data/credentials/job runtimes are replaced by the updater.
"""
import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

REPO = 'Judgment-Pack/judgment-pack-desk'
API = 'https://api.github.com/repos/' + REPO + '/releases/latest'
MAX_ARCHIVE = 512 * 1024 * 1024
MAX_EXPANDED = 2 * 1024 * 1024 * 1024
VERSION = re.compile(r'v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$')
SHA = re.compile(r'[0-9a-f]{64}$')

def semver(value):
    match = VERSION.fullmatch(value)
    if not match: raise ValueError('Expected a stable release version')
    return tuple(map(int, match.groups()))

def atomic_json(path, value):
    fd, name = tempfile.mkstemp(prefix='.update-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as out:
            json.dump(value, out, indent=2); out.write('\n'); out.flush(); os.fsync(out.fileno())
        os.replace(name, path)
        fd = os.open(path.parent, os.O_RDONLY)
        try: os.fsync(fd)
        finally: os.close(fd)
    finally:
        if os.path.exists(name): os.unlink(name)

def private(path):
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError('Installation directory must be an owner-only directory')

def installation(root, create=False):
    if os.name != 'posix': raise ValueError('Managed updates currently support Linux and macOS')
    if not root.is_absolute(): raise ValueError('Installation root must be absolute')
    if not root.exists():
        if not create: raise ValueError('No managed installation exists')
        root.mkdir(parents=True, mode=0o700)
    private(root)
    marker = root / 'installation.json'
    if not marker.exists():
        if not create or list(root.iterdir()): raise ValueError('Refusing an unrecognized installation directory')
        atomic_json(marker, {'schemaVersion': 1, 'current': None, 'previous': None, 'pending': None, 'autoInstall': False})
    state = read_state(root)
    releases = root / 'releases'
    releases.mkdir(mode=0o700, exist_ok=True); private(releases)
    return state

def read_state(root):
    path = root / 'installation.json'
    if path.is_symlink(): raise ValueError('Invalid installation state')
    state = json.loads(path.read_text())
    if state.get('schemaVersion') != 1 or type(state.get('autoInstall')) is not bool: raise ValueError('Unsupported installation state')
    for field in ('current', 'previous', 'pending'):
        value = state.get(field)
        if value is not None:
            if not isinstance(value, dict) or not SHA.fullmatch(value.get('manifestDigest', '')): raise ValueError('Invalid installed release')
            semver(value.get('version', ''))
    return state

@contextlib.contextmanager
def lock(root, name):
    import fcntl
    fd = os.open(root / name, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        if info.st_uid != os.getuid() or not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077: raise ValueError('Unsafe update lock')
        try: fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise ValueError('Desk or another update operation is running') from None
        yield
    finally: os.close(fd)

def request(url, limit):
    # All callers construct GitHub URLs. No credentials or arbitrary endpoints.
    req = urllib.request.Request(url, headers={'Accept': 'application/vnd.github+json', 'User-Agent': 'Judgment-Pack-Desk-updater'})
    with urllib.request.urlopen(req, timeout=30) as reply:
        if not reply.url.startswith('https://'): raise ValueError('Refused insecure release redirect')
        data = reply.read(limit + 1)
    if len(data) > limit: raise ValueError('Release response exceeded its size limit')
    return data

def host_platform():
    os_name = {'Linux': 'linux', 'Darwin': 'darwin'}.get(platform.system())
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
    if not os_name or not arch: raise ValueError('Unsupported installation platform')
    return os_name + '/' + arch

def latest_release():
    doc = json.loads(request(API, 2 * 1024 * 1024))
    tag = doc.get('tag_name', '')
    semver(tag)
    if doc.get('draft') or doc.get('prerelease'): raise ValueError('Expected a published stable release')
    platform_name = host_platform()
    suffix = platform_name.replace('/', '_')
    name = 'judgment-pack-desk_' + tag.lstrip('v') + '_' + suffix + '.tar.gz'
    assets = {a['name']: a for a in doc.get('assets', [])}
    asset = assets.get(name)
    return {'version': tag.lstrip('v'), 'url': 'https://github.com/' + REPO + '/releases/tag/' + tag,
            'platform': platform_name, 'asset': name if asset else None,
            'assetDigest': asset.get('digest') if asset else None, 'tag': tag}

def checked(root, force=False):
    with lock(root, 'state.lock'):
        state = read_state(root)
        if not force and time.time() - state.get('checkedAt', 0) < 86400: return state
        try:
            state['latest'] = latest_release(); state.pop('error', None)
        except Exception:
            # Keep the previous successful check visible; never claim current on error.
            state['error'] = 'Release check failed. The installed version is unchanged.'
        state['checkedAt'] = int(time.time())
        atomic_json(root / 'installation.json', state)
        return state

def safe_name(value):
    path = PurePosixPath(value)
    if not value or '\\' in value or path.is_absolute() or any(part in ('..', '.') for part in value.split('/')) or any(ord(c) < 32 for c in value):
        raise ValueError('Unsafe release path')
    return path

def unpack(archive, target):
    total = 0; names = set()
    with tarfile.open(archive, 'r:gz') as src:
        for member in src:
            path = safe_name(member.name.rstrip('/'))
            if str(path) in names: raise ValueError('Duplicate release path')
            names.add(str(path))
            if len(names) > 10000: raise ValueError('Too many release entries')
            if not member.isfile() and not member.isdir(): raise ValueError('Release links and special files are forbidden')
            destination = target.joinpath(*path.parts)
            if member.isdir(): destination.mkdir(parents=True, exist_ok=True, mode=0o700); continue
            total += member.size
            if member.size < 0 or total > MAX_EXPANDED: raise ValueError('Release expanded size exceeded')
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            with src.extractfile(member) as data, destination.open('xb') as out:
                shutil.copyfileobj(data, out)
            destination.chmod(0o700 if member.mode & 0o111 else 0o600)

def digest_file(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''): h.update(block)
    return h.hexdigest()

def verify(directory, expected, platform_name=None):
    if directory.is_symlink() or not directory.is_dir(): raise ValueError('Unsafe release directory')
    manifest_path = directory / 'release-manifest.json'
    if manifest_path.is_symlink() or not manifest_path.is_file(): raise ValueError('Missing release manifest')
    if digest_file(manifest_path) != expected['manifestDigest']: raise ValueError('Release manifest changed')
    if manifest_path.stat().st_size > 2 * 1024 * 1024: raise ValueError('Oversized release manifest')
    doc = json.loads(manifest_path.read_text())
    if doc.get('version') != expected['version'] or doc.get('stateEpoch', 1) != 1: raise ValueError('Release needs an explicit data migration')
    if doc.get('platform') != host_platform() or (platform_name and doc.get('platform') != platform_name):
        raise ValueError('Wrong release platform')
    files = doc.get('files', {})
    if not isinstance(files, dict) or len(files) > 10000 or not {'jpack-desk', 'jpack', 'jpack-runner', 'jpack-source-worker', 'gateway', 'gateway-bundle.json'} <= set(files): raise ValueError('Incomplete release bundle')
    if doc.get('desk', {}).get('repository') != 'https://github.com/' + REPO: raise ValueError('Wrong release repository')
    for name, digest in files.items():
        path = directory.joinpath(*safe_name(name).parts)
        if path.is_symlink() or not path.is_file() or not SHA.fullmatch(digest) or digest_file(path) != digest: raise ValueError('Release file verification failed')
    actual = {str(p.relative_to(directory)) for p in directory.rglob('*') if not p.is_dir()}
    if actual != set(files) | {'release-manifest.json'}: raise ValueError('Release contains unlisted files')
    for p in directory.rglob('*'):
        if p.is_symlink(): raise ValueError('Release contains a link')
    return doc

def stage(root):
    with lock(root, 'download.lock'):
        state = checked(root, True)
        if state.get('error'): raise ValueError(state['error'])
        release = state['latest']
        if not release['asset']: raise ValueError('No release asset is available for this platform')
        if state['current'] and semver(release['version']) <= semver(state['current']['version']): return state
        base = 'https://github.com/' + REPO + '/releases/download/' + release['tag'] + '/'
        sums = request(base + 'checksums.txt', 1024 * 1024).decode('utf8')
        entries = {}
        for line in sums.splitlines():
            fields = line.split()
            if len(fields) == 2 and SHA.fullmatch(fields[0]):
                if fields[1].lstrip('*') in entries: raise ValueError('Duplicate checksum entry')
                entries[fields[1].lstrip('*')] = fields[0]
        expected_archive = entries.get(release['asset'])
        if not expected_archive: raise ValueError('No archive checksum was published')
        if release['assetDigest'] and release['assetDigest'] != 'sha256:' + expected_archive: raise ValueError('GitHub asset digest disagrees with checksums')
        with tempfile.TemporaryDirectory(prefix='.download-', dir=root) as temp:
            temp = Path(temp); archive = temp / 'bundle.tar.gz'
            # Bounded streaming prevents a release download consuming unbounded memory.
            req = urllib.request.Request(base + release['asset'], headers={'User-Agent': 'Judgment-Pack-Desk-updater'})
            with urllib.request.urlopen(req, timeout=30) as reply, archive.open('xb') as out:
                if not reply.url.startswith('https://'): raise ValueError('Refused insecure release redirect')
                remaining = MAX_ARCHIVE
                while True:
                    block = reply.read(min(1024 * 1024, remaining + 1))
                    if not block: break
                    remaining -= len(block)
                    if remaining < 0: raise ValueError('Release archive exceeded size limit')
                    out.write(block)
            if digest_file(archive) != expected_archive: raise ValueError('Release archive checksum mismatch')
            extracted = temp / 'files'; extracted.mkdir(mode=0o700); unpack(archive, extracted)
            # Both flat bundles and a single enclosing directory are accepted.
            candidates = list(extracted.glob('release-manifest.json')) + list(extracted.glob('*/release-manifest.json'))
            if len(candidates) != 1: raise ValueError('Expected one release manifest')
            bundle = candidates[0].parent
            if bundle != extracted and list(extracted.iterdir()) != [bundle]: raise ValueError('Unexpected archive entries')
            selected = {'version': release['version'], 'manifestDigest': digest_file(candidates[0])}
            verify(bundle, selected, release['platform'])
            target = root / 'releases' / selected['version']
            if target.exists(): verify(target, selected, release['platform'])
            else: os.rename(bundle, target)
            with lock(root, 'state.lock'):
                state = read_state(root)
                if state['current'] and semver(selected['version']) <= semver(state['current']['version']): return state
                state['pending'] = selected
                atomic_json(root / 'installation.json', state)
        return state

def install_launcher(root, source):
    destination = root / 'desk-update.py'
    if source.resolve() == destination: return
    data = source.read_bytes(); fd, temp = tempfile.mkstemp(dir=root, prefix='.launcher-')
    try:
        with os.fdopen(fd, 'wb') as out:
            out.write(data); out.flush(); os.fsync(out.fileno())
        os.chmod(temp, 0o700); os.replace(temp, destination)
    finally:
        if os.path.exists(temp): os.unlink(temp)

def activate(root, rollback=False):
    with lock(root, 'state.lock'):
        state = read_state(root)
        target = state['previous'] if rollback else state['pending']
        if target:
            bundle = root / 'releases' / target['version']
            verify(bundle, target)
            if (bundle / 'desk-update.py').is_file(): install_launcher(root, bundle / 'desk-update.py')
            state['previous'], state['current'], state['pending'] = state['current'], target, None
            if rollback: state['autoInstall'] = False
            atomic_json(root / 'installation.json', state)
        return state

def launch(root, arguments):
    with lock(root, 'running.lock'):
        state = read_state(root)
        if state['autoInstall']:
            state = checked(root)
            latest = state.get('latest')
            selected = state['pending'] or state['current']
            if not state.get('error') and latest and (not selected or semver(latest['version']) > semver(selected['version'])):
                try: stage(root)
                except Exception as error: print('Update deferred: ' + str(error), file=sys.stderr)
        state = activate(root)
        if not state['current']: raise ValueError('Install a release before starting Desk')
        bundle = root / 'releases' / state['current']['version']
        verify(bundle, state['current'])
        args = list(arguments)
        if args[:1] == ['--']: args = args[1:]
        if any(a.startswith('-') and a.lstrip('-').split('=', 1)[0] in ('jpack', 'runner', 'dev-token', 'local-gateway-worker') for a in args): raise ValueError('Managed launches use their bundled components; use a development launch for overrides')
        env = dict(os.environ, JPACK_DESK_INSTALL_ROOT=str(root))
        # Never inherit overrides that would mix components from another installation.
        env.pop('JPACK_DESK_GATEWAY_BUNDLE', None)
        env.pop('JPACK_DESK_GATEWAY_MANIFEST_SHA256', None)
        process = subprocess.Popen([str(bundle / 'jpack-desk'), '--jpack', str(bundle / 'jpack'), '--runner', str(bundle / 'jpack-runner'), *args], env=env)
        previous = {}
        for sig in (signal.SIGINT, signal.SIGTERM): previous[sig] = signal.signal(sig, lambda signum, _frame: process.send_signal(signum))
        try: return process.wait()
        finally:
            for sig, handler in previous.items(): signal.signal(sig, handler)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(os.environ.get('XDG_DATA_HOME', str(Path.home() / '.local/share'))) / 'jpack-desk-install')
    parser.add_argument('action', choices=('install', 'check', 'status', 'stage', 'cancel', 'auto-on', 'auto-off', 'run', 'rollback'))
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    args = parser.parse_args(); root = args.root
    installation(root, create=args.action == 'install')
    if args.action == 'install':
        with lock(root, 'running.lock'):
            stage(root); state = activate(root)
            selected = root / 'releases' / state['current']['version'] / 'desk-update.py'
            install_launcher(root, selected if selected.is_file() else Path(__file__))
    elif args.action == 'run': return launch(root, args.arguments)
    elif args.action == 'check': state = checked(root, True)
    elif args.action == 'stage': state = stage(root)
    elif args.action == 'rollback':
        with lock(root, 'running.lock'): state = activate(root, rollback=True)
    elif args.action in ('auto-on', 'auto-off', 'cancel'):
        with lock(root, 'state.lock'):
            state = read_state(root)
            if args.action == 'cancel': state['pending'] = None
            else: state['autoInstall'] = args.action == 'auto-on'
            atomic_json(root / 'installation.json', state)
    else: state = read_state(root)
    print(json.dumps(state))
    return 0
if __name__ == '__main__':
    try: sys.exit(main())
    except Exception as error:
        print('Desk update: ' + str(error), file=sys.stderr); sys.exit(1)
