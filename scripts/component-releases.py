#!/usr/bin/env python3
"""Validate published component pins, report or manually propose newer stable releases.

Development pins are deliberately held: a stable tag must not replace newer,
unreleased local work. Promotion requires a reviewed lock edit. `status` only
reports; it exits non-zero while a newer stable release is unadopted or a
lookup fails.

The lock's `web-packages` are the web page's own pinned packages, each one
release asset of its repository installed by web/package.json from that
asset's URL. `check` holds web/package.json and web/package-lock.json to the
lock's entry; `verify` downloads the asset and holds its digests to the lock
and the npm lockfile, its tag to the locked commit, and its attestation (`gh
attestation verify`) to the repository's release workflow at that tag and
commit. `status`, `propose` and `ci` read the binary components only.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'internal/releaseplan/components.json'
WEB = ROOT / 'web'
REPOS = {name: 'Judgment-Pack/judgment-pack-' + name for name in ('runtime', 'runner', 'gateway')}
# The web page's pinned packages: the lock's name for each, its npm name and its repository.
WEB_PACKAGES = {'harness': ('@judgment-pack/harness', 'Judgment-Pack/judgment-pack-agent')}
TRUSTED = set(REPOS.values()) | {repository for _, repository in WEB_PACKAGES.values()}
VERSION = r'v\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.+-]+)?'
ASSET = r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.tgz'
ASSET_BYTES = 16 * 1024 * 1024

def read_plan():
    plan = json.loads(LOCK.read_text())
    if plan.get('schemaVersion') != 1 or plan.get('stateEpoch') != 1 or set(plan.get('components', {})) != set(REPOS):
        raise ValueError('Unsupported component lock')
    if set(plan) - {'schemaVersion', 'stateEpoch', 'components', 'web-packages'} or set(plan.get('web-packages', {})) != set(WEB_PACKAGES):
        raise ValueError('Unsupported component lock')
    for name, package in plan['web-packages'].items():
        npm, repository = WEB_PACKAGES[name]
        if set(package) != {'package', 'repository', 'version', 'revision', 'asset', 'sha256', 'channel'} or package['package'] != npm or package['repository'] != repository:
            raise ValueError('Invalid web package pin: ' + name)
        if stable_version(package['version']) is None or package['channel'] != 'stable' or not re.fullmatch('[0-9a-f]{40}', package['revision']) or not re.fullmatch('[0-9a-f]{64}', package['sha256']) or not re.fullmatch(ASSET, package['asset']):
            raise ValueError('Invalid web package pin: ' + name)
    for name, component in plan['components'].items():
        if component.get('repository') != REPOS[name] or not re.fullmatch('[0-9a-f]{40}', component.get('revision', '')):
            raise ValueError('Invalid component pin: ' + name)
        if not re.fullmatch(VERSION, component.get('version', '')) or component.get('channel') not in ('stable', 'preview', 'development'):
            raise ValueError('Invalid version/channel: ' + name)
        if component['channel'] == 'stable' and stable_version(component['version']) is None:
            raise ValueError('Stable component requires a stable version: ' + name)
    return plan

def stable_version(value):
    match = re.fullmatch(r'v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)', value)
    return tuple(map(int, match.groups())) if match else None

def should_update(current, latest):
    candidate = stable_version(latest)
    if current['channel'] == 'development' or candidate is None:
        return False
    prior = stable_version(current['version'].split('-')[0].split('+')[0])
    return prior is not None and (candidate > prior or candidate == prior and current['channel'] == 'preview')

def get_release(repo, tag=None):
    if repo not in TRUSTED or tag is not None and not re.fullmatch(VERSION, tag):
        raise ValueError('Invalid release reference')
    endpoint = '/releases/tags/' + tag if tag else '/releases/latest'
    req = urllib.request.Request('https://api.github.com/repos/' + repo + endpoint, headers={'Accept': 'application/vnd.github+json', 'User-Agent': 'Desk-release-maintenance', **({'Authorization': 'Bearer ' + os.environ['GH_TOKEN']} if os.environ.get('GH_TOKEN') else {})})
    try:
        with urllib.request.urlopen(req, timeout=20) as reply:
            data = reply.read(2 * 1024 * 1024 + 1)
            if len(data) > 2 * 1024 * 1024: raise ValueError('Oversized release metadata')
            return json.loads(data)
    except urllib.error.HTTPError as error:
        if error.code == 404: return None
        raise

def tag_commit(repo, tag):
    # No ref taken from release prose or target_commitish (which can be a branch).
    if repo not in TRUSTED or not re.fullmatch(VERSION, tag): raise ValueError('Invalid component tag')
    ref = 'refs/tags/' + tag
    result = subprocess.check_output(['git', 'ls-remote', 'https://github.com/' + repo + '.git', ref, ref + '^{}'], text=True, timeout=30)
    refs = dict(line.split()[::-1] for line in result.splitlines())
    commit = refs.get(ref + '^{}', refs.get(ref, ''))
    if not re.fullmatch('[0-9a-f]{40}', commit): raise ValueError('Tag did not resolve to a commit')
    return commit

def asset_url(package):
    return 'https://github.com/' + package['repository'] + '/releases/download/' + package['version'] + '/' + package['asset']

def npm_integrity(data):
    return 'sha512-' + base64.b64encode(hashlib.sha512(data).digest()).decode()

def check_web_packages(plan, web=WEB):
    """Offline: web/package.json installs each pinned package from its release asset, and the npm lockfile resolved that one."""
    manifest = json.loads((web / 'package.json').read_text())
    locked = json.loads((web / 'package-lock.json').read_text())
    for name, package in plan.get('web-packages', {}).items():
        url = asset_url(package)
        if manifest.get('dependencies', {}).get(package['package']) != url:
            raise ValueError('web/package.json does not install the locked release asset: ' + name)
        if locked.get('packages', {}).get('', {}).get('dependencies', {}).get(package['package']) != url:
            raise ValueError('web/package-lock.json does not record the locked release asset: ' + name)
        entry = locked.get('packages', {}).get('node_modules/' + package['package'], {})
        if entry.get('resolved') != url or entry.get('version') != package['version'][1:] or not str(entry.get('integrity', '')).startswith('sha512-'):
            raise ValueError('web/package-lock.json resolved another asset: ' + name)

def download_asset(url):
    req = urllib.request.Request(url, headers={'User-Agent': 'Desk-release-maintenance'})
    with urllib.request.urlopen(req, timeout=60) as reply:
        data = reply.read(ASSET_BYTES + 1)
    if len(data) > ASSET_BYTES: raise ValueError('Oversized release asset')
    return data

def verify_attestation(package, data):
    """The asset's build-provenance attestation, as `gh attestation verify` reads it: made by the
    repository's release workflow at the locked tag and commit, for an asset of this name and digest."""
    repository = package['repository']
    with tempfile.TemporaryDirectory(prefix='desk-web-package-') as directory:
        asset = Path(directory) / package['asset']
        asset.write_bytes(data)
        result = subprocess.run(['gh', 'attestation', 'verify', str(asset), '--repo', repository,
                                 '--signer-workflow', repository + '/.github/workflows/release.yml',
                                 '--source-ref', 'refs/tags/' + package['version'],
                                 '--source-digest', package['revision'], '--format', 'json'],
                                capture_output=True, text=True, timeout=120)
    if result.returncode != 0:
        raise ValueError('Web package attestation did not verify: ' + package['package'])
    try:
        verified = json.loads(result.stdout)
        subjects = [subject for entry in verified for subject in entry['verificationResult']['statement']['subject']]
    except (ValueError, KeyError, TypeError):
        raise ValueError('Web package attestation answered in a shape this script cannot read: ' + package['package'])
    if not any(subject.get('name') == package['asset'] and subject.get('digest', {}).get('sha256') == package['sha256'] for subject in subjects):
        raise ValueError('Web package attestation names another asset or digest: ' + package['package'])

def verify_web_packages(plan, web=WEB):
    """Online: the published release, its tag's commit, and the asset's digests, against the lock and the npm lockfile."""
    check_web_packages(plan, web)
    locked = json.loads((web / 'package-lock.json').read_text())
    for name, package in plan.get('web-packages', {}).items():
        release = get_release(package['repository'], package['version'])
        if not release or release.get('draft') or release.get('tag_name') != package['version'] or release.get('prerelease') is not False:
            raise ValueError('Missing or mismatched published release: ' + name)
        if tag_commit(package['repository'], package['version']) != package['revision']:
            raise ValueError('Web package tag disagrees with locked commit: ' + name)
        data = download_asset(asset_url(package))
        if hashlib.sha256(data).hexdigest() != package['sha256']:
            raise ValueError('Web package asset disagrees with the locked sha256: ' + name)
        if locked['packages']['node_modules/' + package['package']]['integrity'] != npm_integrity(data):
            raise ValueError('web/package-lock.json integrity disagrees with the release asset: ' + name)
        verify_attestation(package, data)
        print(name + ': published asset, digest, commit and attestation verified')

def verify_plan(plan):
    for name, component in plan['components'].items():
        if component['channel'] == 'development':
            print(name + ': development pin held; no published release asserted')
            continue
        release = get_release(component['repository'], component['version'])
        if not release or release.get('draft') or release.get('tag_name') != component['version']:
            raise ValueError('Missing or mismatched published release: ' + name)
        if component['channel'] == 'stable' and release.get('prerelease') is not False:
            raise ValueError('Stable pin requires a stable published release: ' + name)
        if tag_commit(component['repository'], component['version']) != component['revision']:
            raise ValueError('Component version tag disagrees with locked commit: ' + name)
        print(name + ': published version and commit verified')

def freshness(plan):
    """Read-only: compare each published pin with its repository's latest stable release.

    A lookup that fails, or returns anything but a release that says it is
    neither a draft nor a prerelease with a stable tag, is reported as failed,
    never as current. Development pins are not looked up.
    Only lock values and validated stable tags reach the report.
    """
    rows, attention = [], False
    for name, component in plan['components'].items():
        if component['channel'] == 'development':
            rows.append((name, component['version'], 'not checked', 'development pin held'))
            continue
        try:
            release = get_release(component['repository'])
        except Exception:
            release = None
        latest = release.get('tag_name') if isinstance(release, dict) else None
        if not isinstance(latest, str) or release.get('draft') is not False or release.get('prerelease') is not False or stable_version(latest) is None:
            rows.append((name, component['version'], 'unknown', 'lookup failed'))
            attention = True
        elif should_update(component, latest):
            rows.append((name, component['version'], latest, 'newer stable release not adopted'))
            attention = True
        else:
            rows.append((name, component['version'], latest, 'no newer stable release'))
    return rows, attention

def report_freshness(plan):
    rows, attention = freshness(plan)
    report = '## Component release freshness\n\n| Component | Locked | Latest stable | Status |\n| --- | --- | --- | --- |\n'
    report += ''.join('| ' + ' | '.join(row) + ' |\n' for row in rows)
    report += '\n' + ('Adopt newer releases through the component update PR, or run `python3 scripts/component-releases.py propose`. A failed lookup is not evidence that a pin is current.' if attention else 'Every published pin matches its latest stable release or is newer.') + '\nThis check does not change the lock.\n'
    print(report)
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as summary:
            summary.write(report)
    return 1 if attention else 0

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('check', 'verify', 'propose', 'ci', 'status'))
    args = parser.parse_args()
    plan = read_plan()
    if args.action == 'status':
        raise SystemExit(report_freshness(plan))
    if args.action == 'verify':
        verify_plan(plan)
        verify_web_packages(plan)
    elif args.action == 'ci':
        for name, component in plan['components'].items(): print(name + '=' + component['revision'])
    elif args.action == 'propose':
        changes = []
        for name, current in plan['components'].items():
            if current['channel'] == 'development':
                print(name + ': development pin held; publish and promote it explicitly')
                continue
            release = get_release(current['repository'])
            if not release or release.get('draft') or release.get('prerelease') or not should_update(current, release['tag_name']): continue
            current.update(version=release['tag_name'], revision=tag_commit(current['repository'], release['tag_name']), channel='stable')
            changes.append(name)
        if changes: LOCK.write_text(json.dumps(plan, indent=2) + '\n')
        print('Updated: ' + (', '.join(changes) or 'none'))
    else:
        check_web_packages(plan)
        print('Component lock valid')
if __name__ == '__main__': main()
