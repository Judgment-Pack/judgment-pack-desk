#!/usr/bin/env python3
"""Handwritten Devkit selection/runner fixtures; no Desk programs run here."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SUBPROCESS_TIMEOUT = 30
DOC_CHECKS = ['diff-check', 'local-paths', 'agent-docs', 'python-tests', 'needle-check']


class Selection(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='desk-check-selection-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = dict(os.environ, JPDEV_ROOT=str(self.root))
        for key in ('JPDEV_WRAP', 'JPDEV_LOGDIR', 'GIT_DIR', 'GIT_WORK_TREE', 'GIT_INDEX_FILE'):
            self.env.pop(key, None)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        for name in ('check.sh', 'changed_checks.py'):
            dest = self.root / 'scripts/dev' / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / 'scripts/dev' / name, dest)
        self.write('dev.json', json.dumps({'version': 1, 'checks': [
            {'name': 'first', 'run': 'printf "first\\n" >> ran'},
            {'name': 'second', 'run': 'printf "second\\n" >> ran'},
            {'name': 'third', 'run': 'printf "third\\n" >> ran'},
        ]}))
        self.mapping([{'paths': ['README.md', 'SECURITY.md'], 'checks': ['second']}])
        for name in ('README.md', 'SECURITY.md', 'source.go', '.github/workflows/ci.yml'):
            self.write(name, 'base\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture baseline')
        self.git('branch', 'baseline')

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, env=self.env, stderr=subprocess.STDOUT, text=True,
                                       stdin=subprocess.DEVNULL, timeout=SUBPROCESS_TIMEOUT)

    def mapping(self, rules):
        self.write('jpdev.json', json.dumps({'version': 1, 'changed': rules}))

    def run_command(self, *args):
        return subprocess.run(args, cwd=self.root, env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              stdin=subprocess.DEVNULL, timeout=SUBPROCESS_TIMEOUT)

    def select(self, base='baseline'):
        return self.run_command(sys.executable, str(self.root / 'scripts/dev/changed_checks.py'),
                                '--root', str(self.root), '--manifest', str(self.root / 'dev.json'), '--base', base)

    def assert_selection(self, expected):
        result = self.select()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines()[0], expected, result.stdout)
        return result

    def runner(self, *args):
        return self.run_command('bash', str(self.root / 'scripts/dev/check.sh'), *args)

    def test_mapped_document(self):
        self.write('README.md', 'edited\n')
        self.assert_selection('only second')

    def test_overlapping_union_in_manifest_order(self):
        self.mapping([{'paths': ['README.md'], 'checks': ['third', 'second']},
                      {'paths': ['*.md'], 'checks': ['first', 'second']}])
        self.git('add', 'jpdev.json')
        self.git('commit', '-qm', 'fixture mapping')
        self.git('branch', '-f', 'baseline')
        self.write('README.md', 'edited\n')
        self.assert_selection('only first,second,third')

    def test_source_workflow_untracked_and_no_change_fallback(self):
        self.assert_selection('whole')
        for name in ('source.go', '.github/workflows/ci.yml', 'new-file'):
            with self.subTest(name=name):
                self.write(name, 'edited\n')
                result = self.assert_selection('whole')
                self.assertIn(name, result.stdout)
                if name == 'new-file':
                    self.assertIn('untracked path new-file is not mapped', result.stdout)
                    (self.root / name).unlink()
                else:
                    self.git('restore', name)

    def test_selection_inputs_force_full_even_when_mapped(self):
        names = ['jpdev.json', 'dev.json', 'scripts/dev/check.sh', 'scripts/dev/changed_checks.py']
        self.mapping([{'paths': ['**'], 'checks': ['second']}])
        self.git('add', 'jpdev.json')
        self.git('commit', '-qm', 'fixture broad mapping')
        self.git('branch', '-f', 'baseline')
        for name in names:
            with self.subTest(name=name):
                path = self.root / name
                path.write_text(path.read_text() + '\n')
                result = self.assert_selection('whole')
                self.assertIn('selection input changed: ' + name, result.stdout)
                self.git('restore', name)

    def test_committed_staged_worktree_deletion_and_both_rename_paths(self):
        self.write('README.md', 'committed\n')
        self.git('add', 'README.md')
        self.git('commit', '-qm', 'fixture change')
        self.assert_selection('only second')
        self.git('reset', '--hard', 'baseline')
        self.write('README.md', 'staged\n')
        self.git('add', 'README.md')
        self.assert_selection('only second')
        self.git('reset', '--hard', 'baseline')
        (self.root / 'README.md').unlink()
        self.assert_selection('only second')
        self.git('restore', 'README.md')
        self.git('mv', 'README.md', 'SECURITY.md.renamed')
        result = self.assert_selection('whole')
        self.assertIn('SECURITY.md.renamed', result.stdout)
        self.git('reset', '--hard', 'baseline')
        self.git('mv', '-f', 'source.go', 'SECURITY.md')
        result = self.assert_selection('whole')
        self.assertIn('source.go', result.stdout)

    def test_missing_base_and_missing_merge_base_refused(self):
        result = self.select('absent-ref')
        self.assertEqual(result.returncode, 2)
        self.assertIn('refused:', result.stderr)
        tree = self.git('write-tree').strip()
        commit = self.git('commit-tree', tree, '-m', 'unrelated root').strip()
        result = self.select(commit)
        self.assertEqual(result.returncode, 2)
        self.assertIn('no merge base', result.stderr)

    def test_invalid_mapping_refused_before_runner_commands(self):
        self.mapping([{'paths': ['README.md'], 'checks': ['absent-check']}])
        result = self.runner('--changed', 'baseline')
        self.assertEqual(result.returncode, 2)
        self.assertIn('absent-check', result.stderr)
        self.assertFalse((self.root / 'ran').exists())

    def test_changed_only_refused_before_commands(self):
        result = self.runner('--changed', 'baseline', '--only', 'first')
        self.assertEqual(result.returncode, 2)
        self.assertIn('--changed and --only both choose the steps', result.stderr)
        self.assertFalse((self.root / 'ran').exists())

    def test_runner_selected_failure_propagates(self):
        manifest = json.loads((self.root / 'dev.json').read_text())
        manifest['checks'][1]['run'] = 'printf "selected failure\\n"; exit 7'
        self.write('dev.json', json.dumps(manifest))
        self.git('add', 'dev.json')
        self.git('commit', '-qm', 'fixture failing command')
        self.git('branch', '-f', 'baseline')
        self.write('README.md', 'edited\n')
        result = self.runner('--all', '--changed', 'baseline')
        self.assertEqual(result.returncode, 1, result.stderr + result.stdout)
        self.assertIn('exit 7', result.stdout)
        self.assertIn('selected failure', result.stdout)
        self.assertFalse((self.root / 'ran').exists())

    def test_no_mapping_defaults_to_full(self):
        (self.root / 'jpdev.json').unlink()
        self.write('README.md', 'edited\n')
        self.assert_selection('whole')

    def test_workflow_executes_scoped_only_for_pull_request(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        checks = workflow.split('      - name: Checks\n', 1)[1]
        body = checks.split('        run: |\n', 1)[1].split('      # The full output', 1)[0]
        script = '\n'.join(line[10:] for line in body.splitlines())
        self.write('scripts/dev/check.sh', '#!/usr/bin/env bash\nprintf "%s\\n" "$*" > invoked\n')
        (self.root / 'scripts/dev/check.sh').chmod(0o755)
        self.git('update-ref', 'refs/remotes/origin/baseline', 'HEAD')
        for event in ('pull_request', 'push', 'workflow_call', 'release', 'workflow_dispatch'):
            with self.subTest(event=event):
                result = subprocess.run(['bash', '-e', '-c', script], cwd=self.root,
                                        env=dict(self.env, CHECK_EVENT=event, CHECK_BASE_REF='baseline'),
                                        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        stdin=subprocess.DEVNULL, timeout=SUBPROCESS_TIMEOUT)
                self.assertEqual(result.returncode, 0, result.stderr)
                # go-test runs in the Go jobs (internal/desk in shards), not a second time here.
                expected = '--all --skip go-test --changed origin/baseline' if event == 'pull_request' else '--all --skip go-test'
                self.assertEqual((self.root / 'invoked').read_text().strip(), expected)
                (self.root / 'invoked').unlink()
        for base in ('', '../invalid', 'missing-branch'):
            with self.subTest(base=base):
                result = subprocess.run(['bash', '-e', '-c', script], cwd=self.root,
                                        env=dict(self.env, CHECK_EVENT='pull_request', CHECK_BASE_REF=base),
                                        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        stdin=subprocess.DEVNULL, timeout=SUBPROCESS_TIMEOUT)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertFalse((self.root / 'invoked').exists())

    def test_runner_defaults_to_full(self):
        self.write('README.md', 'edited\n')
        result = self.runner()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertEqual((self.root / 'ran').read_text(), 'first\nsecond\nthird\n')


class DeskPolicy(unittest.TestCase):
    def test_conservative_mapping(self):
        mapping = json.loads((ROOT / 'jpdev.json').read_text())
        self.assertEqual(mapping, {'version': 1, 'changed': [
            {'paths': ['README.md', 'SECURITY.md'], 'checks': DOC_CHECKS},
            {'paths': ['AGENTS.md', 'CLAUDE.md'], 'checks': DOC_CHECKS},
        ]})
        names = [step['name'] for step in json.loads((ROOT / 'dev.json').read_text())['checks']]
        self.assertTrue(set(DOC_CHECKS) <= set(names))

    def test_workflow_full_history_and_event_guard(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        aggregate = workflow.split('  check:\n', 1)[1].split('  release-archives:\n', 1)[0]
        self.assertIn('fetch-depth: 0', aggregate)
        self.assertIn('CHECK_EVENT: ${{ github.event_name }}', aggregate)
        self.assertIn('CHECK_BASE_REF: ${{ github.base_ref }}', aggregate)
        self.assertIn('if [ "$CHECK_EVENT" = pull_request ]; then', aggregate)
        self.assertIn('if [ -z "$CHECK_BASE_REF" ]; then', aggregate)
        self.assertIn('git check-ref-format "refs/heads/$CHECK_BASE_REF" || exit 2', aggregate)
        self.assertIn('git rev-parse --verify "refs/remotes/origin/$CHECK_BASE_REF^{commit}" >/dev/null || exit 2', aggregate)
        self.assertIn('scripts/dev/check.sh --all --skip go-test --changed "origin/$CHECK_BASE_REF"\n          else\n            scripts/dev/check.sh --all --skip go-test\n          fi', aggregate)
        # The go-test the check job skips runs in the Go jobs: the other packages in Go checks,
        # internal/desk in the shard matrix, which Go requires.
        self.assertIn('GO_TEST_SHARD: others', workflow)
        self.assertIn('GO_TEST_SHARD: ${{ matrix.shard }}/${{ needs.go-plan.outputs.count }}', workflow)
        self.assertIn('needs: [go-plan, go-checks, go-test-desk]', workflow)
        # The storage line's lane (ADR-0015) runs the same shards with the line on and blocks
        # nothing: continue-on-error, not a need of Go, and artifacts the Go job never reads.
        lane = workflow.split('\n  go-test-desk-storage-line:\n', 1)[1].split('\n  go:\n', 1)[0]
        self.assertIn('continue-on-error: true', lane)
        self.assertIn("JPACK_DESK_STORAGE_LINE: 'on'", lane)
        self.assertIn('GO_TEST_SHARD: ${{ matrix.shard }}/${{ needs.go-plan.outputs.count }}', lane)
        self.assertNotIn('name: go-test-desk-shard-', lane)
        self.assertNotIn('JPACK_DESK_STORAGE_LINE', workflow.split('\n  go-test-desk-storage-line:\n', 1)[0])
        self.assertEqual(workflow.count('--changed'), 1)
        self.assertIn('  push:\n    branches: [main]\n  pull_request:\n  workflow_call:', workflow)
        self.assertIn('name: Go\n', workflow)
        self.assertIn('name: Web\n', workflow)
        self.assertIn('scripts/dev/check.sh --only gofmt,component-policy,python-tests,needle-check,component-identities,go-vet,go-test', workflow)
        self.assertIn('scripts/dev/check.sh --only i18n-check,ui-message-audit,typecheck,vite-build,web-tests,dialect-read', workflow)
        self.assertIn('uses: ./.github/workflows/release-build.yml', workflow)


if __name__ == '__main__':
    unittest.main()
