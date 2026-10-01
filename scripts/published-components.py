#!/usr/bin/env python3
"""Consume attested component release archives without rebuilding their programs."""
import base64
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile

PROGRAMS = {
    'runtime': ('jpack',),
    'runner': ('jpack-runner', 'jpack-source-worker'),
    'gateway': ('gateway', 'adapter-document', 'gateway-connections', 'adapter-drive',
                'adapter-gmail', 'adapter-sources', 'adapter-web', 'adapter-render'),
}
NOTICES = {'runtime': ('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES'),
           'runner': ('LICENSE', 'THIRD_PARTY_NOTICES'),
           'gateway': ('LICENSE', 'THIRD_PARTY_NOTICES')}
MAX_ARCHIVE = 128 * 1024 * 1024
MAX_EXPANDED = 512 * 1024 * 1024


def run(args, **kwargs):
    return subprocess.run(args, check=True, timeout=180, **kwargs)


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def checksum(path, archive_name):
    if path.stat().st_size > 64 * 1024:
        raise ValueError('Oversized component checksums')
    found = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9._-]+)', line)
        if not match or match[2] in found:
            raise ValueError('Invalid or duplicate component checksum')
        found[match[2]] = match[1]
    if archive_name not in found:
        raise ValueError('Missing component archive checksum')
    return found[archive_name]


def require_unregistered_gateway(component):
    # Preserve the public-bundle guard even though we no longer compile this
    # source. The attestation pins the executable to this same source commit.
    raw = run(['gh', 'api', 'repos/' + component['repository'] +
        '/contents/adapters/cmd/gateway-connections/publisher-google.json?ref=' + component['revision']],
        capture_output=True, text=True).stdout
    response = json.loads(raw)
    try:
        if response.get('encoding') != 'base64' or response.get('size', 65) > 64:
            raise ValueError()
        content = base64.b64decode(''.join(response['content'].split()), validate=True)
        if len(content) > 64 or content.strip() != b'{}':
            raise ValueError()
    except (KeyError, TypeError, ValueError):
        raise ValueError('Public Desk bundles require an unconfigured Google registration') from None


def read_archive(archive, wanted):
    """Read only named regular files; never extract a path or link from a tar."""
    if archive.stat().st_size > MAX_ARCHIVE:
        raise ValueError('Oversized component archive')
    found, seen, total = {}, set(), 0
    with tarfile.open(archive, 'r:gz') as source:
        for member in source:
            name = member.name
            parts = PurePosixPath(name).parts
            if (not parts or name.startswith('/') or '\\' in name or '..' in parts or
                    str(PurePosixPath(name)) != name or name in seen or not member.isfile()):
                raise ValueError('Unsafe or duplicate component archive member')
            seen.add(name)
            total += member.size
            if member.size < 0 or total > MAX_EXPANDED or len(seen) > 10000:
                raise ValueError('Component archive expansion limit exceeded')
            if name in wanted:
                found[name] = source.extractfile(member).read()
    if set(found) != set(wanted):
        raise ValueError('Component archive is missing required programs or notices')
    return found


def fetch(name, component, platform_name, directory):
    """Fail closed: a checksum is insufficient without the pinned signer/ref/SHA."""
    repository, version, revision = (component[k] for k in ('repository', 'version', 'revision'))
    if (name not in PROGRAMS or repository != 'Judgment-Pack/judgment-pack-' + name or
            not re.fullmatch(r'v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)', version) or
            not re.fullmatch(r'[0-9a-f]{40}', revision) or
            platform_name not in ('linux/amd64', 'darwin/amd64', 'darwin/arm64')):
        raise ValueError('Invalid published component identity or platform')
    directory.mkdir(parents=True)
    prefix = 'judgment-pack' if name == 'runtime' else 'judgment-pack-' + name
    asset = prefix + '_' + version[1:] + '_' + platform_name.replace('/', '_') + '.tar.gz'
    run(['gh', 'release', 'download', version, '--repo', repository, '--dir', str(directory),
         '--pattern', asset, '--pattern', 'checksums.txt'])
    archive = directory / asset
    expected = checksum(directory / 'checksums.txt', asset)
    if archive.stat().st_size > MAX_ARCHIVE or digest(archive) != expected:
        raise ValueError('Component archive checksum mismatch or size limit exceeded')
    workflow = repository + '/.github/workflows/release.yml'
    run(['gh', 'attestation', 'verify', str(archive), '--repo', repository,
         '--signer-workflow', workflow, '--source-ref', 'refs/tags/' + version,
         '--source-digest', revision, '--deny-self-hosted-runners'])
    if name == 'gateway':
        require_unregistered_gateway(component)
    contents = read_archive(archive, PROGRAMS[name] + NOTICES[name])
    files = {file: contents[file] for file in PROGRAMS[name]}
    files.update({name + '-licenses/' + file: contents[file] for file in NOTICES[name]})
    record = {'repository': repository, 'version': version, 'revision': revision,
              'archive': asset, 'archiveSha256': expected, 'signerWorkflow': workflow,
              'files': {file: hashlib.sha256(data).hexdigest() for file, data in files.items()}}
    return files, record


def install(plan, platform_name, bundle, downloads):
    records = {}
    for name, component in plan['components'].items():
        files, records[name] = fetch(name, component, platform_name, downloads / name)
        for file, data in files.items():
            target = bundle / file
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            target.chmod(0o755 if file in PROGRAMS[name] else 0o644)
    gateway = plan['components']['gateway']
    manifest = {'version': gateway['version'], 'revision': gateway['revision'],
                'files': {file: records['gateway']['files'][file] for file in PROGRAMS['gateway']}}
    (bundle / 'gateway-bundle.json').write_text(json.dumps(manifest, indent=2) + '\n')
    (bundle / 'component-artifacts.json').write_text(json.dumps(records, indent=2) + '\n')


def verify_bundle(plan, platform_name, bundle, downloads):
    # Independent check after packaging: catches a later build/copy replacing
    # even a correctly named and version-stamped program with different bytes.
    records = json.loads((bundle / 'component-artifacts.json').read_text())
    if set(records) != set(plan['components']):
        raise ValueError('Incomplete published component records')
    for name, component in plan['components'].items():
        files, expected = fetch(name, component, platform_name, downloads / name)
        if records[name] != expected:
            raise ValueError('Component provenance disagrees with published release: ' + name)
        for file in files:
            if digest(bundle / file) != expected['files'][file]:
                raise ValueError('Bundled file differs from published component: ' + file)
