#!/usr/bin/env python3
"""Build a complete Desk distribution from the exact reviewed gateway revision.

Usage: python3 scripts/build-bundle.py --gateway-checkout /path/to/gateway
The supplied checkout is read via git archive; its working tree is never changed.
Without it, fetch the pinned revision into a temporary clone. No runtime downloads.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
REVISION = re.search(r'const GatewayRevision = "([a-f0-9]{40})"', (ROOT / 'internal/desk/local_gateway.go').read_text())[1]

def run(args, cwd=None, **kwargs):
    return subprocess.run(args, cwd=cwd, check=True, **kwargs)

def require_unregistered_gateway(source):
    """Public bundles never inherit a publisher's Google registration."""
    path = source / 'adapters/cmd/gateway-connections/publisher-google.json'
    try:
        with path.open('rb') as file:
            raw = file.read(65)
        # The upstream sentinel is an empty object. Refuse rather than silently
        # removing an embedded client from an unexpected dependency revision.
        if len(raw) > 64 or raw.strip() != b'{}':
            raise ValueError()
    except (OSError, ValueError):
        raise ValueError('Public Desk bundles require an unconfigured Google registration. Configure your own app in Admin > Connections after installation.') from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gateway-checkout', type=Path)
    parser.add_argument('--output', type=Path, default=ROOT / 'bin')
    parser.add_argument('--skip-web', action='store_true', help='Use existing embedded assets (backend smoke tests only).')
    args = parser.parse_args()
    target = args.output.resolve()
    target.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='jpack-desk-bundle-') as temp:
        temp = Path(temp)
        repo = args.gateway_checkout
        if repo is None:
            repo = temp / 'checkout'
            run(['git', 'init', str(repo)])
            run(['git', '-c', 'http.version=HTTP/1.1', 'fetch', '--depth=1', 'https://github.com/Judgment-Pack/judgment-pack-gateway.git', REVISION], repo)
        data = run(['git', 'archive', REVISION], repo, stdout=subprocess.PIPE).stdout
        source = temp / 'source'
        source.mkdir()
        with tarfile.open(fileobj=io.BytesIO(data)) as archive:
            archive.extractall(source, filter='data')
        require_unregistered_gateway(source)
        suffix = '.exe' if os.environ.get('GOOS', '') == 'windows' or os.name == 'nt' else ''
        files = {}
        for name, module, package in [('gateway', 'go', '.'), ('adapter-document', 'adapters', './cmd/adapter-document'), ('gateway-connections', 'adapters', './cmd/gateway-connections'), ('adapter-drive', 'adapters', './cmd/adapter-drive'), ('adapter-gmail', 'adapters', './cmd/adapter-gmail')]:
            artifact = temp / (name + suffix)
            run(['go', 'build', '-buildvcs=false', '-trimpath', '-o', str(artifact), package], source / module)
            files[artifact.name] = hashlib.sha256(artifact.read_bytes()).hexdigest()
            # Replace the inode atomically: a running companion may still have
            # the previous executable open during a local rebuild.
            descriptor, staged = tempfile.mkstemp(prefix='.companion-', dir=target)
            os.close(descriptor)
            try:
                shutil.copy2(artifact, staged)
                os.replace(staged, target / artifact.name)
            finally:
                if os.path.exists(staged): os.unlink(staged)
        # Ship upstream license material with the companion executables.
        licenses = target / 'gateway-licenses'
        licenses.mkdir(exist_ok=True)
        for path in source.rglob('*'):
            if path.is_file() and (path.name.upper().startswith(('LICENSE', 'NOTICE', 'COPYING', 'PATENTS'))):
                destination = licenses / path.relative_to(source)
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(path, destination)
        manifest = target / 'gateway-bundle.json.tmp'
        manifest.write_text(json.dumps({'revision': REVISION, 'files': files}, indent=2) + '\n')
        manifest.replace(target / 'gateway-bundle.json')
        if not args.skip_web:
            run(['npm', '--prefix', 'web', 'run', 'build'], ROOT)
        run(['go', 'build', '-trimpath', '-o', str(target / ('jpack-desk' + suffix)), '.'], ROOT)
    print('Complete Desk bundle:', target)
    print('Google registration: not included; configure your own app in Admin > Connections.')

if __name__ == '__main__':
    main()
