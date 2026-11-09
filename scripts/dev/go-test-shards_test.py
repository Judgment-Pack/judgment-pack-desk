#!/usr/bin/env python3
"""go-test-shards.py against handwritten test lists and logs; no Go program runs here."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

HELPER = Path(__file__).resolve().parent / 'go-test-shards.py'
NAMES = ['TestZeta', 'TestAlpha', 'TestAlphaTwo', 'TestBeta', 'ExampleGamma', 'FuzzDelta', 'TestEpsilon']
LISTING = '\n'.join(NAMES + ['BenchmarkNotRun']) + '\nok  \texample.invalid/internal/desk\t0.01s\n'
# Handwritten seconds. TestEpsilon is not recorded and counts as the default.
SECONDS = {'TestZeta': 100.0, 'TestAlpha': 40.0, 'TestAlphaTwo': 30.0, 'TestBeta': 30.0,
           'ExampleGamma': 0.0, 'FuzzDelta': 20.0}
DEFAULT = 10.0


class Shards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='go-test-shards-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.list = self.root / 'list'
        self.list.write_text(LISTING)
        self.durations = self.root / 'durations.json'
        self.durations.write_text(json.dumps({'version': 1, 'default': DEFAULT, 'tests': SECONDS}))
        self.env = {k: v for k, v in os.environ.items() if k != 'GITHUB_STEP_SUMMARY'}
        self.env['GO_TEST_SHARD_DURATIONS'] = str(self.durations)

    def run_helper(self, *args, env=None):
        return subprocess.run([sys.executable, '-I', str(HELPER), *args], text=True, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, stdin=subprocess.DEVNULL, timeout=30, env=env or self.env)

    def shard(self, k, n):
        result = self.run_helper('select', '%d/%d' % (k, n), str(self.list))
        self.assertEqual(result.returncode, 0, result.stderr)
        match = re.fullmatch(r'\^\((.*)\)\$\n', result.stdout)
        self.assertTrue(match, result.stdout)
        return match.group(1).split('|')

    def write_shard(self, k, n, log_names=None, verdict='PASS', listing=LISTING, name=None):
        directory = self.root / (name or 'shard-%d' % k)
        directory.mkdir()
        (directory / 'go-test-shard.json').write_text(json.dumps({'shard': '%d/%d' % (k, n), 'artifact': 'a-%d' % k}))
        (directory / 'go-test-desk.list').write_text(listing)
        names = self.shard(k, n) if log_names is None else log_names
        lines = []
        for test in names:
            lines += ['=== RUN   ' + test, '=== RUN   %s/sub' % test, '    --- %s: %s/sub (0.00s)' % (verdict, test),
                      '--- %s: %s (0.01s)' % (verdict, test)]
        (directory / 'go-test-desk.log').write_text('\n'.join(lines + ['PASS', 'ok  \tpkg\t1.0s']) + '\n')
        return directory

    def cost(self, test):
        return max(SECONDS.get(test, DEFAULT), 0.1)

    def test_shards_partition_the_list_in_run_order_balanced_by_time(self):
        for n in (1, 2, 3, 7):
            with self.subTest(shards=n):
                parts = [self.shard(k, n) for k in range(1, n + 1)]
                together = [test for part in parts for test in part]
                self.assertEqual(sorted(together), sorted(NAMES))
                self.assertEqual(len(together), len(set(together)))
                for part in parts:
                    self.assertEqual(part, [test for test in NAMES if test in part])
                # Longest first to the least loaded shard: no shard exceeds another by more
                # than its own longest test.
                loads = [sum(map(self.cost, part)) for part in parts]
                for part, load in zip(parts, loads):
                    self.assertLessEqual(load - min(loads), max(map(self.cost, part)) + 1e-9, (parts, loads))
        self.assertEqual(self.shard(1, 3), self.shard(1, 3))

    def test_the_split_follows_recorded_time_not_count(self):
        # Longest first: Zeta 100 | Alpha 40 + AlphaTwo 30 + Beta 30; FuzzDelta 20 breaks the
        # 100/100 tie to shard 1; Epsilon 10 (default) and Gamma (0 s, the 0.1 floor) to 2.
        self.assertEqual(self.shard(1, 2), ['TestZeta', 'FuzzDelta'])
        self.assertEqual(self.shard(2, 2), ['TestAlpha', 'TestAlphaTwo', 'TestBeta', 'ExampleGamma', 'TestEpsilon'])
        # Three shards: 100 | 40 + 20 + 10 | 30 + 30 + 0.1, where a count split is 3, 2, 2.
        self.assertEqual(self.shard(1, 3), ['TestZeta'])
        self.assertEqual(self.shard(2, 3), ['TestAlpha', 'FuzzDelta', 'TestEpsilon'])
        self.assertEqual(self.shard(3, 3), ['TestAlphaTwo', 'TestBeta', 'ExampleGamma'])

    def test_tests_reported_at_no_time_are_spread_over_the_shards(self):
        quick = ['TestQuick%02d' % i for i in range(10)]
        self.list.write_text('\n'.join(quick) + '\n')
        self.durations.write_text(json.dumps({'version': 1, 'default': DEFAULT, 'tests': {n: 0.0 for n in quick}}))
        parts = [self.shard(k, 2) for k in (1, 2)]
        self.assertEqual(sorted(parts[0] + parts[1]), quick)
        self.assertEqual((len(parts[0]), len(parts[1])), (5, 5))

    def test_an_unrecorded_test_counts_as_the_default(self):
        self.durations.write_text(json.dumps({'version': 1, 'default': 500.0, 'tests': SECONDS}))
        self.assertEqual(self.shard(1, 2), ['TestEpsilon'])

    def test_a_missing_or_malformed_durations_file_is_refused(self):
        for content in (None, 'not json', json.dumps({'version': 2, 'default': 1, 'tests': {}}),
                        json.dumps({'version': 1, 'default': 0, 'tests': {}}),
                        json.dumps({'version': 1, 'default': 1, 'tests': {'TestA': -1}}),
                        json.dumps({'version': 1, 'default': 1, 'tests': {'not a test': 1}})):
            with self.subTest(content=content):
                if content is None:
                    self.durations.unlink(missing_ok=True)
                else:
                    self.durations.write_text(content)
                result = self.run_helper('select', '1/2', str(self.list))
                self.assertEqual(result.returncode, 2, result.stdout)
                self.assertIn('durations', result.stderr)

    def test_durations_records_top_level_seconds_from_shard_logs(self):
        one = self.root / 'one'
        one.mkdir()
        (one / 'go-test-desk.log').write_text('\n'.join([
            '=== RUN   TestAlpha', '    --- PASS: TestAlpha/sub (2.00s)', '--- PASS: TestAlpha (2.50s)',
            '--- FAIL: TestBeta (1.25s)', '--- SKIP: TestGamma (0.00s)',
            'panic: test timed out after 30m0s', '\trunning tests:', '\t\tTestDelta (9m0s)']) + '\n')
        two = self.root / 'two'
        two.mkdir()
        (two / 'go-test-desk.log').write_text('--- PASS: TestAlpha (3.00s)\n--- PASS: TestEpsilon (0.75s)\n')
        result = self.run_helper('durations', str(one), str(two))
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['tests'], {'TestAlpha': 3.0, 'TestBeta': 1.25, 'TestGamma': 0.0, 'TestEpsilon': 0.75})
        self.assertEqual(data['version'], 1)
        self.assertEqual(data['default'], 1.2)
        self.assertEqual(self.run_helper('durations', str(self.root / 'nothing')).returncode, 2)

    def test_the_checked_in_durations_file_is_read(self):
        env = {k: v for k, v in self.env.items() if k != 'GO_TEST_SHARD_DURATIONS'}
        result = self.run_helper('select', '1/1', str(self.list), env=env)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_select_refuses_a_bad_shard_a_bad_list_and_an_empty_shard(self):
        for shard in ('0/3', '4/3', '1/0', 'x', '1/3/1'):
            with self.subTest(shard=shard):
                self.assertEqual(self.run_helper('select', shard, str(self.list)).returncode, 2)
        broken = self.root / 'broken'
        broken.write_text('TestAlpha\n# example.invalid/internal/desk\nfoo.go:1: undefined: x\nFAIL\n')
        result = self.run_helper('select', '1/1', str(broken))
        self.assertEqual(result.returncode, 2)
        self.assertIn('not a test list line', result.stderr)
        self.assertEqual(self.run_helper('select', '8/8', str(self.list)).returncode, 2)

    def test_complete_passes_when_every_test_reported_once(self):
        dirs = [str(self.write_shard(k, 3)) for k in (1, 2, 3)]
        summary = self.root / 'summary.md'
        result = self.run_helper('complete', '3', *dirs, env=dict(self.env, GITHUB_STEP_SUMMARY=str(summary)))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('7 listed tests, 7 reported, over 3 of 3 shards', result.stdout)
        self.assertIn('shard 2/3: PASS', summary.read_text())
        self.assertIn('`a-2`', summary.read_text())

    def test_complete_fails_on_a_test_missing_from_every_shard(self):
        first = self.shard(1, 3)
        dirs = [str(self.write_shard(1, 3, log_names=first[1:]))] + [str(self.write_shard(k, 3)) for k in (2, 3)]
        result = self.run_helper('complete', '3', *dirs)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn('1 listed tests ran in no shard: ' + first[0], result.stdout)

    def test_complete_fails_on_a_missing_duplicated_or_foreign_shard(self):
        two = [str(self.write_shard(k, 3)) for k in (1, 2)]
        result = self.run_helper('complete', '3', *two)
        self.assertEqual(result.returncode, 1)
        self.assertIn('no record of shard(s) 3 of 3', result.stdout)
        again = str(self.write_shard(2, 3, name='again'))
        result = self.run_helper('complete', '3', *two, again, str(self.write_shard(3, 3)))
        self.assertEqual(result.returncode, 1)
        self.assertIn('shard 2/3 appears twice', result.stdout)
        result = self.run_helper('complete', '3', *two, str(self.write_shard(3, 4, name='other')))
        self.assertEqual(result.returncode, 1)
        self.assertIn('is not one of 3 shards', result.stdout)
        self.assertEqual(self.run_helper('complete', '3', str(self.root / 'nothing')).returncode, 1)

    def test_complete_fails_when_shards_listed_different_tests_or_report_twice(self):
        other = LISTING.replace('TestBeta\n', 'TestBeta\nTestNew\n')
        dirs = [str(self.write_shard(1, 2)), str(self.write_shard(2, 2, listing=other))]
        result = self.run_helper('complete', '2', *dirs)
        self.assertEqual(result.returncode, 1)
        self.assertIn('listed other tests', result.stdout)
        dirs = [str(self.write_shard(1, 2, name='one', log_names=self.shard(1, 2) + ['TestBeta'])),
                str(self.write_shard(2, 2, name='two'))]
        result = self.run_helper('complete', '2', *dirs)
        self.assertEqual(result.returncode, 1)
        self.assertIn('reported more than once', result.stdout)

    def test_summary_names_failing_tests_and_a_timeout(self):
        directory = self.write_shard(1, 1, verdict='FAIL')
        with open(directory / 'go-test-desk.log', 'a') as log:
            log.write('panic: test timed out after 30m0s\n\trunning tests:\n\t\tTestZeta (2m0s)\n\ngoroutine 1 [x]:\n')
        result = self.run_helper('summary', '1/1', str(self.list), str(directory / 'go-test-desk.log'))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('shard 1/1: FAIL', result.stdout)
        self.assertIn('- FAIL `TestAlpha/sub`', result.stdout)
        self.assertIn('- FAIL `TestAlpha`', result.stdout)
        self.assertIn('still running: TestZeta (2m0s)', result.stdout)


if __name__ == '__main__':
    unittest.main()
