#!/usr/bin/env python3
"""Collect native, smoke-tested platform artifacts into one release checksum set."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil

spec = importlib.util.spec_from_file_location('components', Path(__file__).with_name('component-releases.py'))
components = importlib.util.module_from_spec(spec)
spec.loader.exec_module(components)
PLATFORMS = ('linux_amd64', 'darwin_arm64', 'darwin_amd64')


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as file:
        for block in iter(lambda: file.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()


def assemble(source, output, version):
    if components.stable_version('v' + version) is None:
        raise ValueError('Expected a stable version without v prefix')
    plan = components.read_plan()
    verified = []
    revision = None
    for platform in PLATFORMS:
        directory = source / ('desk-release-' + platform)
        archive = directory / ('judgment-pack-desk_' + version + '_' + platform + '.tar.gz')
        manifest = directory / ('release-manifest_' + platform + '.json')
        expected = {}
        for line in (directory / 'checksums.txt').read_text().splitlines():
            parts = line.split()
            if len(parts) != 2 or parts[1] in expected:
                raise ValueError('Invalid or duplicate platform checksum')
            expected[parts[1]] = parts[0]
        if set(expected) != {archive.name, manifest.name}:
            raise ValueError('Incomplete platform checksums')
        for file in (archive, manifest):
            if file.is_symlink() or not file.is_file() or digest(file) != expected[file.name]:
                raise ValueError('Platform artifact checksum mismatch')
        doc = json.loads(manifest.read_text())
        if (doc.get('platform') != platform.replace('_', '/') or doc.get('version') != version or
                doc.get('formatVersion') != 1 or doc.get('stateEpoch') != plan['stateEpoch'] or
                doc.get('desk', {}).get('repository') != 'https://github.com/Judgment-Pack/judgment-pack-desk'):
            raise ValueError('Platform manifest disagrees with the release')
        for name, component in plan['components'].items():
            if doc.get(name) != component:
                raise ValueError('Platform component disagrees with the release lock')
        current = doc.get('desk', {}).get('revision')
        if not isinstance(current, str) or len(current) != 40 or any(c not in '0123456789abcdef' for c in current):
            raise ValueError('Invalid Desk revision')
        if revision is not None and revision != current:
            raise ValueError('Platform archives use different Desk revisions')
        revision = current
        verified.extend((archive, manifest))
    if output.exists() and list(output.iterdir()):
        raise ValueError('Release output must be empty')
    output.mkdir(parents=True, exist_ok=True)
    for file in verified:
        shutil.copyfile(file, output / file.name)
    # Retain the historical Linux sidecar name for existing release consumers.
    shutil.copyfile(output / 'release-manifest_linux_amd64.json', output / 'release-manifest.json')
    (output / 'checksums.txt').write_text(''.join(
        digest(file) + '  ' + file.name + '\n' for file in sorted(output.iterdir())))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('version')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    assemble(args.directory, args.output, args.version)
