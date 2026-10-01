#!/usr/bin/env python3
"""Verify a built Desk archive and exercise its actual bundled executables."""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('desk_updater', ROOT / 'scripts/desk-update.py')
updater = importlib.util.module_from_spec(spec)
spec.loader.exec_module(updater)
spec = importlib.util.spec_from_file_location('published', ROOT / 'scripts/published-components.py')
published = importlib.util.module_from_spec(spec)
spec.loader.exec_module(published)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('version')
    args = parser.parse_args()
    artifacts = args.directory.resolve()
    updater.semver(args.version)
    archives = list(artifacts.glob('judgment-pack-desk_' + args.version + '_*.tar.gz'))
    if len(archives) != 1:
        raise ValueError('Expected exactly one platform archive')
    platform_name = updater.host_platform()
    suffix = platform_name.replace('/', '_')
    if archives[0].name != 'judgment-pack-desk_' + args.version + '_' + suffix + '.tar.gz':
        raise ValueError('Archive name does not match this smoke-test host')
    manifest_name = 'release-manifest_' + suffix + '.json'
    expected = {}
    for line in (artifacts / 'checksums.txt').read_text().splitlines():
        digest, name = line.split()
        if name in expected or not updater.SHA.fullmatch(digest):
            raise ValueError('Invalid or duplicate checksum')
        expected[name] = digest
    if set(expected) != {archives[0].name, manifest_name}:
        raise ValueError('Incomplete release checksums')
    for name, digest in expected.items():
        if updater.digest_file(artifacts / name) != digest:
            raise ValueError('Artifact checksum mismatch')
    with tempfile.TemporaryDirectory(prefix='desk-release-check-') as temp:
        bundle = Path(temp)
        updater.unpack(archives[0], bundle)
        manifest = updater.verify(bundle, {'version': args.version, 'manifestDigest': expected[manifest_name]})
        plan = json.loads((ROOT / 'internal/releaseplan/components.json').read_text())
        for name, component in plan['components'].items():
            if manifest.get(name) != component:
                raise ValueError('Archive component disagrees with the release lock: ' + name)
        with tempfile.TemporaryDirectory(prefix='desk-components-check-') as downloaded:
            published.verify_bundle(plan, platform_name, bundle, Path(downloaded))
        commands = [
            ('jpack', '--version', 'jpack ' + plan['components']['runtime']['version'].lstrip('v')),
            ('gateway', 'version', 'gateway ' + plan['components']['gateway']['version']),
            ('jpack-runner', 'version', 'jpack-runner ' + plan['components']['runner']['version']),
            ('jpack-source-worker', 'version', 'jpack-source-worker ' + plan['components']['runner']['version']),
        ]
        for binary, argument, version in commands:
            actual = subprocess.check_output([str(bundle / binary), argument], text=True, timeout=15).strip()
            if actual != version:
                raise ValueError('Incorrect executable version: ' + binary)
        subprocess.run([sys.executable, str(ROOT / 'scripts/local-gateway-check.py'), str(bundle), str(bundle / 'jpack')], check=True, timeout=180)
        print('Release archive verified:', args.version, manifest['platform'], len(manifest['files']), 'files')


if __name__ == '__main__':
    main()
