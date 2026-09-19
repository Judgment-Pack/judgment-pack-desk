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

def publisher_registration(path):
    """Normalize a Desktop client without carrying endpoints or other secrets."""
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError()
            result[key] = value
        return result
    try:
        with path.open('rb') as file:
            raw = file.read(16_385)
        if len(raw) > 16_384:
            raise ValueError()
        data = json.loads(raw, object_pairs_hook=unique)
        if not isinstance(data, dict) or 'web' in data or 'type' in data:
            raise ValueError()
        client = data['installed']
        client_id = client['client_id']
        secret = client.get('client_secret', '')
        if not isinstance(client_id, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,220}\.apps\.googleusercontent\.com', client_id):
            raise ValueError()
        if not isinstance(secret, str) or len(secret.encode('utf-8')) > 4096 or any(c in secret for c in '\r\n\0'):
            raise ValueError()
        return json.dumps({'installed': {'client_id': client_id, 'client_secret': secret}}, ensure_ascii=True) + '\n'
    except (OSError, ValueError, TypeError, KeyError, AttributeError, UnicodeError):
        raise ValueError('Choose a valid Google Desktop app registration JSON (at most 16 KiB).') from None

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gateway-checkout', type=Path)
    parser.add_argument('--output', type=Path, default=ROOT / 'bin')
    parser.add_argument('--skip-web', action='store_true', help='Use existing embedded assets (backend smoke tests only).')
    parser.add_argument('--google-oauth-client', type=Path, help='Publisher Google Desktop app registration JSON; embedded only in the gateway connection companion.')
    parser.add_argument('--require-google-oauth', action='store_true', help='Refuse a release build without publisher Google sign-in configuration.')
    args = parser.parse_args()
    if args.require_google_oauth and args.google_oauth_client is None:
        parser.error('--require-google-oauth needs --google-oauth-client')
    registration = None
    if args.google_oauth_client is not None:
        try:
            registration = publisher_registration(args.google_oauth_client)
        except ValueError as error:
            parser.error(str(error))
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
        if registration is not None:
            embedded = source / 'adapters/cmd/gateway-connections/publisher-google.json'
            if not embedded.is_file():
                raise SystemExit('The pinned gateway does not support publisher Google registration.')
            # Change only the disposable archive, never the supplied checkout.
            # The compiled companion hash covers this release configuration.
            embedded.write_text(registration, encoding='utf-8')
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
    print('Google sign-in:', 'included' if registration is not None else 'not included (publisher registration was not supplied)')

if __name__ == '__main__':
    main()
