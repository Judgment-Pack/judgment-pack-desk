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


class Shards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='go-test-shards-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.list = self.root / 'list'
        self.list.write_text(LISTING)
        self.env = {k: v for k, v in os.environ.items() if k != 'GITHUB_STEP_SUMMARY'}

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

    def test_shards_partition_the_list_evenly_and_in_run_order(self):
        for n in (1, 2, 3, 7):
            with self.subTest(shards=n):
                parts = [self.shard(k, n) for k in range(1, n + 1)]
                together = [test for part in parts for test in part]
                self.assertEqual(sorted(together), sorted(NAMES))
                self.assertEqual(len(together), len(set(together)))
                self.assertLessEqual(max(map(len, parts)) - min(map(len, parts)), 1)
                for part in parts:
                    self.assertEqual(part, [test for test in NAMES if test in part])
        self.assertEqual(self.shard(1, 3), self.shard(1, 3))

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
