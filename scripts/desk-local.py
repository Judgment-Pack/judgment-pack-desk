#!/usr/bin/env python3
"""Install and run an explicitly selected local Desk bundle. No release activation.

The managed releases and installation.json remain unchanged. The local launcher
holds the same running.lock as the updater and verifies every bundled file on
start. Its paths are relative to --root, so the installation can be relocated.
"""
import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile

SHA = re.compile(r'[0-9a-f]{64}$')
REQUIRED = {'jpack-desk', 'jpack', 'jpack-runner', 'jpack-source-worker', 'gateway', 'gateway-bundle.json'}


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''): value.update(block)
    return value.hexdigest()


def owned(path, directory=False):
    info = path.lstat()
    if info.st_uid != os.getuid() or info.st_mode & 0o022 or not (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)):
        raise ValueError('Local bundle contains an unsafe path')
    return info


def private(path):
    if owned(path, True).st_mode & 0o077: raise ValueError('Installation must be owner-only')


def read_json(path):
    if owned(path).st_size > 2 * 1024 * 1024: raise ValueError('Oversized local manifest')
    return json.loads(path.read_text())


def atomic(path, data, mode=0o600):
    fd, name = tempfile.mkstemp(prefix='.local-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data); stream.flush(); os.fsync(stream.fileno())
        os.chmod(name, mode); os.replace(name, path)
        fd = os.open(path.parent, os.O_RDONLY)
        try: os.fsync(fd)
        finally: os.close(fd)
    finally:
        if os.path.exists(name): os.unlink(name)


@contextlib.contextmanager
def running_lock(root):
    import fcntl
    fd = os.open(root / 'running.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        if info.st_uid != os.getuid() or not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077: raise ValueError('Unsafe running lock')
        try: fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise ValueError('Desk or another install operation is running') from None
        yield
    finally: os.close(fd)


def verify(bundle, expected):
    private(bundle)
    manifest = bundle / 'local-manifest.json'
    doc = read_json(manifest)
    if digest(manifest) != expected: raise ValueError('Local manifest changed')
    if doc.get('schemaVersion') != 1 or doc.get('kind') != 'local-development' or doc.get('stateEpoch') != 1:
        raise ValueError('Unsupported local bundle')
    files = doc.get('files')
    if not isinstance(files, dict) or not REQUIRED <= set(files) or len(files) > 10000: raise ValueError('Incomplete local bundle')
    for name, checksum in files.items():
        rel = PurePosixPath(name)
        if not name or '\\' in name or rel.is_absolute() or any(part in ('', '.', '..') for part in name.split('/')) or any(ord(c) < 32 for c in name):
            raise ValueError('Unsafe local bundle path')
        target = bundle
        for part in rel.parts[:-1]:
            target = target / part; owned(target, True)
        target = target / rel.parts[-1]; owned(target)
        if not isinstance(checksum, str) or not SHA.fullmatch(checksum) or digest(target) != checksum:
            raise ValueError('Local bundle file changed')
    actual = set()
    for path in bundle.rglob('*'):
        owned(path, path.is_dir())
        if path.is_file(): actual.add(path.relative_to(bundle).as_posix())
    if actual != set(files) | {'local-manifest.json'}: raise ValueError('Local bundle contains unlisted files')
    gateway = read_json(bundle / 'gateway-bundle.json')
    if not isinstance(gateway.get('files'), dict): raise ValueError('Invalid Gateway manifest')
    for name, checksum in gateway['files'].items():
        if files.get(name) != checksum: raise ValueError('Gateway manifest disagrees with local bundle')
    return doc


def selected(root):
    private(root)
    marker = read_json(root / 'local-launch.json')
    identifier = marker.get('manifestDigest', '')
    if marker.get('schemaVersion') != 1 or not isinstance(identifier, str) or not SHA.fullmatch(identifier):
        raise ValueError('Invalid local launch selection')
    script = root / 'desk-local.py'; owned(script)
    if marker.get('launcherDigest') != digest(script): raise ValueError('Local launcher changed')
    folder = root / 'local-builds'; private(folder)
    bundle = folder / identifier
    verify(bundle, identifier)
    return bundle


def probe(bundle):
    # Capability probes do not open a Desk or its user data.
    reply = subprocess.run([str(bundle / 'jpack-desk'), '--desk-home-layout-version'], capture_output=True, timeout=3, check=True)
    if reply.stdout.strip() != b'1': raise ValueError('Local Desk does not support home moves')
    reply = subprocess.run([str(bundle / 'jpack-runner'), 'relocate-workspace', '--capabilities'], capture_output=True, timeout=3, check=True)
    if json.loads(reply.stdout).get('version') != 1: raise ValueError('Local Runner does not support relocation')
    return bundle


def check(root):
    return probe(selected(root))


def install(root, source):
    private(root)
    if read_json(root / 'installation.json').get('schemaVersion') != 1: raise ValueError('Unrecognized managed installation')
    identifier = digest(source / 'local-manifest.json'); verify(source, identifier); probe(source)
    with running_lock(root):
        parent = root / 'local-builds'; parent.mkdir(mode=0o700, exist_ok=True); private(parent)
        target = parent / identifier
        if target.exists(): verify(target, identifier)
        else:
            with tempfile.TemporaryDirectory(prefix='.local-build-', dir=root) as temp:
                copy = Path(temp) / 'bundle'; shutil.copytree(source, copy)
                # Flush the complete copied bundle before publishing its selection.
                verify(copy, identifier)
                for path in copy.rglob('*'):
                    fd = os.open(path, os.O_RDONLY)
                    try: os.fsync(fd)
                    finally: os.close(fd)
                os.rename(copy, target)
            fd = os.open(parent, os.O_RDONLY)
            try: os.fsync(fd)
            finally: os.close(fd)
        script = Path(__file__).read_bytes()
        # A mismatched script/selection after interruption fails closed.
        atomic(root / 'desk-local.py', script, 0o700)
        atomic(root / 'local-launch.json', (json.dumps({'schemaVersion': 1, 'manifestDigest': identifier, 'launcherDigest': hashlib.sha256(script).hexdigest()}, indent=2) + '\n').encode())
        check(root)
    return identifier


def launch(root, arguments):
    with running_lock(root):
        bundle = check(root)
        args = list(arguments)
        if args[:1] == ['--']: args = args[1:]
        if any(a.startswith('-') and a.lstrip('-').split('=', 1)[0] in ('jpack', 'runner', 'dev-token', 'local-gateway-worker') for a in args):
            raise ValueError('Local bundle launches use their saved components')
        env = dict(os.environ, JPACK_DESK_INSTALL_ROOT=str(root))
        env.pop('JPACK_DESK_GATEWAY_BUNDLE', None)
        env.pop('JPACK_DESK_GATEWAY_MANIFEST_SHA256', None)
        child = subprocess.Popen([str(bundle / 'jpack-desk'), '--jpack', str(bundle / 'jpack'), '--runner', str(bundle / 'jpack-runner'), *args], env=env)
        handlers = {}
        for sig in (signal.SIGINT, signal.SIGTERM): handlers[sig] = signal.signal(sig, lambda num, _frame: child.send_signal(num))
        try: return child.wait()
        finally:
            for sig, handler in handlers.items(): signal.signal(sig, handler)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('action', choices=('install', 'check', 'run'))
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if not args.root.is_absolute(): raise ValueError('Installation root must be absolute')
    if args.action == 'install':
        if len(args.arguments) != 1: raise ValueError('Expected one local bundle directory')
        print(install(args.root, Path(args.arguments[0])))
    elif args.action == 'check': check(args.root); print('1')
    else: return launch(args.root, args.arguments)
    return 0


if __name__ == '__main__':
    try: sys.exit(main())
    except Exception as error:
        print('Local Desk: ' + str(error), file=sys.stderr); sys.exit(1)
