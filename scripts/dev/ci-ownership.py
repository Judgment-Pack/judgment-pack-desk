#!/usr/bin/env python3
"""Run manifest-backed CI owners and verify same-candidate completion evidence.

This is CI wiring, not a replacement for the unchanged local full check runner.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
CONFIG = 'scripts/dev/ci-ownership.json'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def load(root):
    return (json.loads((root / 'dev.json').read_text()),
            json.loads((root / CONFIG).read_text()))


def validate(manifest, config, root=ROOT):
    require(config.get('version') == 1, 'unsupported ownership version')
    require('parity' not in manifest, 'parity needs an explicit CI owner')
    checks = manifest['checks']
    setup = manifest['setup']
    names = [c['name'] for c in checks]
    setup_names = [c['name'] for c in setup]
    require(len(names) == len(set(names)), 'duplicate manifest check')
    require(len(setup_names) == len(set(setup_names)), 'duplicate manifest setup')
    require(not set(names) & set(setup_names), 'ambiguous check/setup name')
    owners = config['owners']
    require(set(owners) == {'component-policy', 'go', 'web', 'integration',
                            'local-gateway', 'development-bundle'}, 'unknown or missing owner')
    assigned = list(config['external'])
    used_setup = set()
    by_name = {c['name']: c for c in checks + setup}
    dependencies = config['dependencies']
    require(set(dependencies) <= set(names), 'dependencies name an unknown check')
    for name, deps in dependencies.items():
        require(len(deps) == len(set(deps)) and name not in deps, 'duplicate/self dependency: ' + name)
        require(set(deps) <= set(by_name), 'unknown dependency: ' + name)
    for owner, spec in owners.items():
        require(spec['checks'], 'empty owner: ' + owner)
        require(spec['variants'] and len(spec['variants']) == len(set(spec['variants'])),
                'missing/duplicate variants: ' + owner)
        require(all(re.fullmatch(r'[a-zA-Z0-9_-]+', v) for v in spec['variants']), 'invalid variant')
        require(set(spec['setup']) <= set(setup_names), 'unknown setup: ' + owner)
        require(set(spec['prepare']) <= set(names), 'unknown preparation: ' + owner)
        require(not set(spec['prepare']) & set(spec['checks']), 'duplicate preparation: ' + owner)
        assigned.extend(spec['checks'])
        used_setup.update(spec['setup'])
        # Actual execution always follows manifest order, including prerequisites.
        available = set(spec['setup'])
        for phase in ('prepare', 'checks'):
            selected = set(spec[phase])
            for c in checks:
                name = c['name']
                if name not in selected:
                    continue
                needed = set(dependencies.get(name, []))
                if c['toolchain'] == 'node':
                    needed.add('npm-ci')
                require(needed <= available, 'missing setup/dependency for ' + owner + ': ' + name)
                available.add(name)
    require(len(assigned) == len(set(assigned)), 'duplicate owner assignment')
    require(set(assigned) == set(names), 'unassigned/removed/unknown checks: ' +
            ', '.join(sorted(set(assigned) ^ set(names))))
    require(used_setup == set(setup_names), 'unassigned setup step')
    # Only these explicitly reviewed environment-dependent reports live outside candidate CI.
    expected_external = {'component-freshness': 'DESK_CHECK_FRESHNESS',
                         'mutation-rows': 'DESK_MUTATION_BASE'}
    require(set(config['external']) == set(expected_external), 'unknown external ownership policy')
    for name, env in expected_external.items():
        spec = config['external'][name]
        require(spec['requires_env'] == env and spec['reason'], 'missing external policy: ' + name)
        require(by_name[name].get('requires_env') == env and by_name[name].get('optional') is True,
                'changed external check policy: ' + name)
        workflow = root / spec['workflow']
        require(workflow.is_file() and '--only ' + name in workflow.read_text(),
                'missing external workflow: ' + name)
    return owners


def fingerprint(root):
    return {name: hashlib.sha256((root / name).read_bytes()).hexdigest()
            for name in ('dev.json', CONFIG, 'scripts/dev/ci-ownership.py', 'scripts/dev/check.sh',
                         '.github/workflows/ci.yml', '.github/workflows/release-build.yml')}


def receipt(root, owner, variant, output):
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    require(sha == os.environ.get('GITHUB_SHA'), 'checkout differs from candidate revision')
    require(re.fullmatch('[0-9a-f]{40}', sha), 'invalid candidate revision')
    data = {'owner': owner, 'variant': variant, 'sha': sha, 'sources': fingerprint(root)}
    output.mkdir(parents=True, exist_ok=True)
    (output / (owner + '--' + variant + '.json')).write_text(json.dumps(data, sort_keys=True) + '\n')


def run_owner(root, manifest, owners, owner, variant, output):
    require(owner in owners, 'unknown owner: ' + owner)
    spec = owners[owner]
    require(variant in spec['variants'], 'unknown variant: ' + variant)
    by_name = {c['name']: c for c in manifest['checks'] + manifest['setup']}
    # Optional manifest environment skips cannot satisfy an owner's success receipt.
    for name in spec['setup'] + spec['prepare'] + spec['checks']:
        env = by_name[name].get('requires_env')
        require(not env or os.environ.get(env), 'missing required environment: ' + str(env))
    for phase in ('setup', 'prepare', 'checks'):
        if not spec[phase]:
            continue
        command = ['bash', 'scripts/dev/check.sh', '--all', '--only', ','.join(spec[phase])]
        if phase == 'setup':
            command.append('--setup')
        subprocess.run(command, cwd=root, check=True)
    receipt(root, owner, variant, output)


def aggregate(root, manifest, config, needs, evidence, sha, event):
    owners = validate(manifest, config, root)
    require(isinstance(sha, str) and re.fullmatch('[0-9a-f]{40}', sha), 'invalid candidate revision')
    require(isinstance(event, str) and bool(event), 'missing event policy')
    require(isinstance(needs, dict) and set(needs) == set(owners) | {'release-archives'},
            'missing/unknown dependency jobs')
    plan = json.loads((root / 'internal/releaseplan/components.json').read_text())
    development = any(c['channel'] == 'development' for c in plan['components'].values())
    active = set(owners) - {'development-bundle'}
    if development:
        active.add('development-bundle')
    if event == 'pull_request' and not development:
        active.add('release-archives')
    expected = {}
    for owner in needs:
        require(isinstance(needs[owner], dict), 'malformed dependency: ' + owner)
        want = 'success' if owner in active else 'skipped'
        require(needs[owner].get('result') == want,
                owner + ': expected ' + want + ', got ' + str(needs[owner].get('result')))
        if owner in active:
            variants = owners[owner]['variants'] if owner in owners else ['ubuntu-latest']
            for variant in variants:
                expected[owner + '--' + variant + '.json'] = (owner, variant)
    files = list(evidence.rglob('*.json')) if evidence.is_dir() else []
    require(len(files) == len(expected) and {p.name for p in files} == set(expected),
            'missing/duplicate/unexpected completion evidence')
    sources = fingerprint(root)
    for path in files:
        data = json.loads(path.read_text())
        owner, variant = expected[path.name]
        require(data == {'owner': owner, 'variant': variant, 'sha': sha, 'sources': sources},
                'stale/malformed completion evidence: ' + path.name)
    print('ci-ownership: all required jobs and same-candidate variants passed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['validate', 'run', 'aggregate', 'release-receipt'])
    parser.add_argument('--owner')
    parser.add_argument('--variant', default='ubuntu-latest')
    parser.add_argument('--evidence', type=Path)
    parser.add_argument('--needs', type=Path)
    parser.add_argument('--sha', default=os.environ.get('GITHUB_SHA'))
    parser.add_argument('--event', default=os.environ.get('GITHUB_EVENT_NAME'))
    args = parser.parse_args()
    manifest, config = load(ROOT)
    owners = validate(manifest, config)
    if args.command == 'validate':
        print('ci-ownership: every manifest check and setup has an explicit owner')
    elif args.command == 'run':
        run_owner(ROOT, manifest, owners, args.owner, args.variant, args.evidence)
    elif args.command == 'release-receipt':
        receipt(ROOT, 'release-archives', 'ubuntu-latest', args.evidence)
    else:
        aggregate(ROOT, manifest, config, json.loads(args.needs.read_text()),
                  args.evidence, args.sha, args.event)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.CalledProcessError) as exc:
        sys.exit('ci-ownership: ' + str(exc))
