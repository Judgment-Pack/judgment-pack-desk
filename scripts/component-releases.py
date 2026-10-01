#!/usr/bin/env python3
"""Validate published component pins, report or manually propose newer stable releases.

Development pins are deliberately held: a stable tag must not replace newer,
unreleased local work. Promotion requires a reviewed lock edit. `status` only
reports; it exits non-zero while a newer stable release is unadopted or a
lookup fails.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'internal/releaseplan/components.json'
REPOS = {name: 'Judgment-Pack/judgment-pack-' + name for name in ('runtime', 'runner', 'gateway')}
VERSION = r'v\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.+-]+)?'

def read_plan():
    plan = json.loads(LOCK.read_text())
    if plan.get('schemaVersion') != 1 or plan.get('stateEpoch') != 1 or set(plan.get('components', {})) != set(REPOS):
        raise ValueError('Unsupported component lock')
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
    if repo not in REPOS.values() or tag is not None and not re.fullmatch(VERSION, tag):
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
    if repo not in REPOS.values() or not re.fullmatch(VERSION, tag): raise ValueError('Invalid component tag')
    ref = 'refs/tags/' + tag
    result = subprocess.check_output(['git', 'ls-remote', 'https://github.com/' + repo + '.git', ref, ref + '^{}'], text=True, timeout=30)
    refs = dict(line.split()[::-1] for line in result.splitlines())
    commit = refs.get(ref + '^{}', refs.get(ref, ''))
    if not re.fullmatch('[0-9a-f]{40}', commit): raise ValueError('Tag did not resolve to a commit')
    return commit

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

    A lookup that fails or returns anything but a published stable release is
    reported as failed, never as current. Development pins are not looked up.
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
        if not isinstance(latest, str) or release.get('draft') or release.get('prerelease') is not False or stable_version(latest) is None:
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
    else: print('Component lock valid')
if __name__ == '__main__': main()
