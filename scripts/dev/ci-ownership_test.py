#!/usr/bin/env python3
"""Hand-authored CI ownership and fail-closed aggregation cases; no suites or network."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

MODULE = Path(os.environ.get('CI_OWNERSHIP_MODULE', Path(__file__).with_name('ci-ownership.py')))
spec = importlib.util.spec_from_file_location('ci_ownership', MODULE)
ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci)
SHA = '1' * 40


class OwnershipTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.evidence = self.root / 'evidence'
        self.evidence.mkdir()
        self.manifest = {'setup': [{'name': 'npm-ci', 'toolchain': 'node', 'run': 'npm ci'}], 'checks': [
            {'name': 'policy', 'toolchain': 'python'},
            {'name': 'unit-go', 'toolchain': 'go'},
            {'name': 'vite-build', 'toolchain': 'node'},
            {'name': 'unit-web', 'toolchain': 'node'},
            {'name': 'companion', 'toolchain': 'go'},
            {'name': 'gateway', 'toolchain': 'go'},
            {'name': 'development', 'toolchain': 'go'},
            {'name': 'component-freshness', 'toolchain': 'python', 'optional': True,
             'requires_env': 'DESK_CHECK_FRESHNESS'},
            {'name': 'mutation-rows', 'toolchain': 'go', 'optional': True,
             'requires_env': 'DESK_MUTATION_BASE'},
        ]}
        self.config = {'version': 1, 'owners': {
            'component-policy': {'checks': ['policy'], 'setup': [], 'prepare': [], 'variants': ['ubuntu-latest']},
            'go': {'checks': ['unit-go'], 'setup': [], 'prepare': [], 'variants': ['ubuntu-latest']},
            'web': {'checks': ['vite-build', 'unit-web'], 'setup': ['npm-ci'], 'prepare': [], 'variants': ['ubuntu-latest']},
            'integration': {'checks': ['companion'], 'setup': ['npm-ci'], 'prepare': ['vite-build'], 'variants': ['ubuntu-latest']},
            'local-gateway': {'checks': ['gateway'], 'setup': [], 'prepare': [], 'variants': ['ubuntu-latest', 'macos-latest']},
            'development-bundle': {'checks': ['development'], 'setup': [], 'prepare': [], 'variants': ['ubuntu-latest', 'macos-15', 'macos-15-intel']},
        }, 'dependencies': {'unit-web': ['npm-ci', 'vite-build'], 'companion': ['npm-ci', 'vite-build']},
            'external': {
                'component-freshness': {'workflow': 'freshness.yml', 'requires_env': 'DESK_CHECK_FRESHNESS', 'reason': 'scheduled report'},
                'mutation-rows': {'workflow': 'mutation.yml', 'requires_env': 'DESK_MUTATION_BASE', 'reason': 'changed PR rows'},
            }}
        (self.root / 'freshness.yml').write_text('run: scripts/dev/check.sh --only component-freshness\n')
        (self.root / 'mutation.yml').write_text('run: scripts/dev/check.sh --only mutation-rows\n')
        for name in ('dev.json', ci.CONFIG, 'scripts/dev/ci-ownership.py', 'scripts/dev/check.sh',
                     '.github/workflows/ci.yml', '.github/workflows/release-build.yml'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('test source\n')
        self.set_development(False)
        self.needs = {name: {'result': 'success'} for name in self.config['owners']}
        self.needs['development-bundle']['result'] = 'skipped'
        self.needs['release-archives'] = {'result': 'success'}
        self.write_evidence(['component-policy', 'go', 'web', 'integration', 'local-gateway', 'release-archives'])

    def set_development(self, development):
        path = self.root / 'internal/releaseplan/components.json'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({'components': {'runtime': {'channel': 'development' if development else 'stable'}}}))

    def write_evidence(self, owners):
        for name in owners:
            variants = self.config['owners'][name]['variants'] if name in self.config['owners'] else ['ubuntu-latest']
            for variant in variants:
                (self.evidence / (name + '--' + variant + '.json')).write_text(json.dumps({
                    'owner': name, 'variant': variant, 'sha': SHA, 'sources': ci.fingerprint(self.root)}))

    def validate(self):
        return ci.validate(self.manifest, self.config, self.root)

    def aggregate(self, event='pull_request'):
        ci.aggregate(self.root, self.manifest, self.config, self.needs, self.evidence, SHA, event)

    def test_complete_coverage(self):
        self.validate()

    def test_new_check_unassigned(self):
        self.manifest['checks'].append({'name': 'new-check', 'toolchain': 'go'})
        with self.assertRaisesRegex(ValueError, 'unassigned'):
            self.validate()

    def test_removed_check(self):
        self.manifest['checks'].pop(0)
        with self.assertRaisesRegex(ValueError, 'unassigned'):
            self.validate()

    def test_duplicate_owner(self):
        self.config['owners']['web']['checks'].append('unit-go')
        with self.assertRaisesRegex(ValueError, 'duplicate owner'):
            self.validate()

    def test_unknown_owner(self):
        self.config['owners']['typo'] = self.config['owners'].pop('go')
        with self.assertRaisesRegex(ValueError, 'owner'):
            self.validate()

    def test_duplicate_manifest_check(self):
        self.manifest['checks'].append(copy.deepcopy(self.manifest['checks'][0]))
        with self.assertRaisesRegex(ValueError, 'duplicate manifest'):
            self.validate()

    def test_missing_setup(self):
        self.config['owners']['web']['setup'] = []
        with self.assertRaisesRegex(ValueError, 'missing setup'):
            self.validate()

    def test_missing_build_dependency(self):
        self.config['owners']['integration']['prepare'] = []
        with self.assertRaisesRegex(ValueError, 'missing setup/dependency'):
            self.validate()

    def test_unassigned_new_setup(self):
        self.manifest['setup'].append({'name': 'new-setup', 'toolchain': 'shell'})
        with self.assertRaisesRegex(ValueError, 'unassigned setup'):
            self.validate()

    def test_unknown_setup(self):
        self.config['owners']['web']['setup'] = ['typo']
        with self.assertRaisesRegex(ValueError, 'unknown setup'):
            self.validate()

    def test_external_not_blanket_optional(self):
        self.config['external']['unit-go'] = {'reason': 'optional'}
        self.config['owners']['go']['checks'] = ['policy']
        with self.assertRaises(ValueError):
            self.validate()

    def test_missing_external_workflow(self):
        (self.root / 'freshness.yml').unlink()
        with self.assertRaisesRegex(ValueError, 'missing external workflow'):
            self.validate()

    def test_stable_pr_pass(self):
        self.aggregate()

    def test_stable_push_skip_archive(self):
        self.needs['release-archives']['result'] = 'skipped'
        (self.evidence / 'release-archives--ubuntu-latest.json').unlink()
        self.aggregate('push')

    def test_development_pr_and_push(self):
        self.set_development(True)
        self.needs['release-archives']['result'] = 'skipped'
        self.needs['development-bundle']['result'] = 'success'
        (self.evidence / 'release-archives--ubuntu-latest.json').unlink()
        self.write_evidence(['development-bundle'])
        self.aggregate()
        self.aggregate('push')

    def test_unexpected_skip_failed_canceled_missing_and_malformed(self):
        original = copy.deepcopy(self.needs)
        for result in ('failure', 'cancelled', 'skipped', 'neutral', '', None):
            with self.subTest(result=result):
                self.needs['go']['result'] = result
                with self.assertRaisesRegex(ValueError, 'expected success'):
                    self.aggregate()
        self.needs = copy.deepcopy(original)
        del self.needs['go']
        with self.assertRaisesRegex(ValueError, 'dependency jobs'):
            self.aggregate()
        self.needs = copy.deepcopy(original)
        self.needs['go'] = {}
        with self.assertRaisesRegex(ValueError, 'expected success'):
            self.aggregate()
        with self.assertRaisesRegex(ValueError, 'dependency jobs'):
            ci.aggregate(self.root, self.manifest, self.config, [], self.evidence, SHA, 'pull_request')

    def test_stable_pr_cannot_skip_archives(self):
        self.needs['release-archives']['result'] = 'skipped'
        with self.assertRaisesRegex(ValueError, 'release-archives: expected success'):
            self.aggregate()

    def test_development_cannot_skip_bundle(self):
        self.set_development(True)
        self.needs['release-archives']['result'] = 'skipped'
        with self.assertRaisesRegex(ValueError, 'development-bundle: expected success'):
            self.aggregate()

    def test_nonapplicable_job_must_be_skipped(self):
        self.needs['development-bundle']['result'] = 'success'
        with self.assertRaisesRegex(ValueError, 'expected skipped'):
            self.aggregate()

    def test_missing_partial_variant_evidence(self):
        (self.evidence / 'local-gateway--macos-latest.json').unlink()
        with self.assertRaisesRegex(ValueError, 'completion evidence'):
            self.aggregate()

    def test_duplicate_evidence(self):
        duplicate = self.evidence / 'duplicate'
        duplicate.mkdir()
        source = self.evidence / 'go--ubuntu-latest.json'
        (duplicate / source.name).write_text(source.read_text())
        with self.assertRaisesRegex(ValueError, 'completion evidence'):
            self.aggregate()

    def test_stale_revision_and_source_and_malformed_evidence(self):
        path = self.evidence / 'go--ubuntu-latest.json'
        original = path.read_text()
        for field, value in [('sha', '2' * 40), ('sources', {}), ('owner', 'web'), ('variant', 'macos-latest')]:
            data = json.loads(original)
            data[field] = value
            path.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, 'stale/malformed'):
                self.aggregate()
        path.write_text('{bad')
        with self.assertRaises(ValueError):
            self.aggregate()

    def test_missing_evidence_directory(self):
        self.evidence = self.root / 'absent'
        with self.assertRaisesRegex(ValueError, 'completion evidence'):
            self.aggregate()

    def test_matrix_cancellation_cannot_use_partial_evidence(self):
        self.needs['local-gateway']['result'] = 'cancelled'
        with self.assertRaisesRegex(ValueError, 'local-gateway: expected success'):
            self.aggregate()

    def test_unknown_dependency_job(self):
        self.needs['extra-job'] = {'result': 'success'}
        with self.assertRaisesRegex(ValueError, 'dependency jobs'):
            self.aggregate()

    def test_absent_event_or_revision_is_not_a_skip_policy(self):
        for sha, event in [(None, 'pull_request'), ('bad', 'pull_request'), (SHA, None), (SHA, '')]:
            with self.subTest(sha=sha, event=event), self.assertRaises(ValueError):
                ci.aggregate(self.root, self.manifest, self.config, self.needs, self.evidence, sha, event)

    def test_optional_manifest_skip_cannot_satisfy_owned_check(self):
        self.manifest['checks'][1]['requires_env'] = 'DESK_CI_TEST_MISSING_ENV'
        self.manifest['checks'][1]['optional'] = True
        with patch.dict(os.environ, {}, clear=True), patch.object(ci.subprocess, 'run') as run, patch.object(ci, 'receipt') as receipt:
            with self.assertRaisesRegex(ValueError, 'missing required environment'):
                ci.run_owner(self.root, self.manifest, self.config['owners'], 'go', 'ubuntu-latest', self.evidence)
        run.assert_not_called()
        receipt.assert_not_called()

    def test_run_consumes_map_setup_build_and_checks(self):
        with patch.object(ci.subprocess, 'run') as run, patch.object(ci, 'receipt') as receipt:
            ci.run_owner(self.root, self.manifest, self.config['owners'], 'integration', 'ubuntu-latest', self.evidence)
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual(commands, [
            ['bash', 'scripts/dev/check.sh', '--all', '--only', 'npm-ci', '--setup'],
            ['bash', 'scripts/dev/check.sh', '--all', '--only', 'vite-build'],
            ['bash', 'scripts/dev/check.sh', '--all', '--only', 'companion']])
        receipt.assert_called_once()

    def test_failed_command_never_emits_receipt(self):
        with patch.object(ci.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'check')), patch.object(ci, 'receipt') as receipt:
            with self.assertRaises(subprocess.CalledProcessError):
                ci.run_owner(self.root, self.manifest, self.config['owners'], 'go', 'ubuntu-latest', self.evidence)
        receipt.assert_not_called()

    def test_changed_checkout_never_emits_receipt(self):
        with patch.object(ci.subprocess, 'check_output', return_value='2' * 40), patch.dict(os.environ, {'GITHUB_SHA': SHA}):
            with self.assertRaisesRegex(ValueError, 'checkout differs'):
                ci.receipt(self.root, 'go', 'ubuntu-latest', self.evidence)


class RepositoryWiringTests(unittest.TestCase):
    def test_real_manifest_and_workflow(self):
        root = Path(__file__).resolve().parents[2]
        manifest, config = ci.load(root)
        owners = ci.validate(manifest, config, root)
        workflow = (root / '.github/workflows/ci.yml').read_text()
        self.assertNotIn('scripts/dev/check.sh --all', workflow)
        self.assertIn('    name: Go\n', workflow)
        self.assertIn('    name: Web\n', workflow)
        self.assertIn('    name: check\n', workflow)
        final = workflow[workflow.index('\n  check:'):]
        self.assertIn('    if: always()\n', final)
        self.assertIn('        if: always()\n', final)
        expected = set(owners) | {'release-archives'}
        needs = final.split('needs: [', 1)[1].split(']', 1)[0]
        self.assertEqual(set(needs.split(', ')), expected)
        for owner in owners:
            self.assertEqual(workflow.count(' run --owner ' + owner + ' '), 1)
        self.assertEqual(config['owners']['local-gateway']['variants'], ['ubuntu-latest', 'macos-latest'])
        self.assertEqual(config['owners']['development-bundle']['variants'], ['ubuntu-latest', 'macos-15', 'macos-15-intel'])
        # Full Go/Web suites belong only to their lanes; integration may rebuild its page.
        self.assertEqual(config['owners']['go']['checks'], ['gofmt', 'python-tests', 'needle-check', 'component-identities', 'go-vet', 'go-test'])
        self.assertEqual(config['owners']['web']['checks'], ['i18n-check', 'ui-message-audit', 'typecheck', 'vite-build', 'web-tests', 'dialect-read'])
        self.assertEqual(config['owners']['integration']['prepare'], ['vite-build'])
        self.assertIn('ci-receipt: true', workflow)
        release = (root / '.github/workflows/release-build.yml').read_text()
        self.assertIn('      ci-receipt:\n        type: boolean\n        default: false', release)


if __name__ == '__main__':
    unittest.main()
