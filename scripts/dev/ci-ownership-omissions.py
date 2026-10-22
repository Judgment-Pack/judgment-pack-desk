#!/usr/bin/env python3
"""Prove the focused tests detect semantic coverage and aggregation omissions."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[2]
source = (root / 'scripts/dev/ci-ownership.py').read_text()
mutations = {
    'unassigned-check-accepted': (
        'require(set(assigned) == set(names),', 'require(True,', 'test_new_check_unassigned'),
    'failed-job-accepted': (
        "require(needs[owner].get('result') == want,", 'require(True,',
        'test_unexpected_skip_failed_canceled_missing_and_malformed'),
    'stale-evidence-accepted': (
        "require(data == {'owner': owner, 'variant': variant, 'sha': sha, 'sources': sources},",
        'require(True,', 'test_stale_revision_and_source_and_malformed_evidence'),
}
with tempfile.TemporaryDirectory(prefix='desk-ci-omissions-') as temporary:
    for name, (needle, replacement, test) in mutations.items():
        if source.count(needle) != 1:
            sys.exit('ci-ownership omissions: mutation needs review: ' + name)
        module = Path(temporary) / (name + '.py')
        module.write_text(source.replace(needle, replacement))
        result = subprocess.run(
            [sys.executable, str(root / 'scripts/dev/ci-ownership_test.py'), 'OwnershipTests.' + test],
            env=dict(os.environ, CI_OWNERSHIP_MODULE=str(module)),
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        if result.returncode == 0 or 'AssertionError: ValueError not raised' not in result.stdout:
            print(result.stdout)
            sys.exit('ci-ownership omissions: not caught by a behavioral assertion: ' + name)
        print('ci-ownership omissions: caught ' + name)
