#!/usr/bin/env python3
"""Validate the release lock, or propose newer stable component releases.

Development pins are deliberately held: a stable tag must not replace newer,
unreleased local work. Promotion requires a reviewed lock edit.
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

def read_plan():
    plan = json.loads(LOCK.read_text())
    if plan.get('schemaVersion') != 1 or plan.get('stateEpoch') != 1 or set(plan.get('components', {})) != set(REPOS):
        raise ValueError('Unsupported component lock')
    for name, component in plan['components'].items():
        if component.get('repository') != REPOS[name] or not re.fullmatch('[0-9a-f]{40}', component.get('revision', '')):
            raise ValueError('Invalid component pin: ' + name)
        if not re.fullmatch(r'v\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.+-]+)?', component.get('version', '')) or component.get('channel') not in ('stable', 'preview', 'development'):
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

def get_release(repo):
    req = urllib.request.Request('https://api.github.com/repos/' + repo + '/releases/latest', headers={'Accept': 'application/vnd.github+json', 'User-Agent': 'Desk-release-maintenance', **({'Authorization': 'Bearer ' + os.environ['GH_TOKEN']} if os.environ.get('GH_TOKEN') else {})})
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
    if stable_version(tag) is None: raise ValueError('Expected stable tag')
    ref = 'refs/tags/' + tag
    result = subprocess.check_output(['git', 'ls-remote', 'https://github.com/' + repo + '.git', ref, ref + '^{}'], text=True)
    refs = dict(line.split()[::-1] for line in result.splitlines())
    commit = refs.get(ref + '^{}', refs.get(ref, ''))
    if not re.fullmatch('[0-9a-f]{40}', commit): raise ValueError('Tag did not resolve to a commit')
    return commit

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('check', 'propose', 'ci'))
    args = parser.parse_args()
    plan = read_plan()
    if args.action == 'ci':
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
