"""Exercise development-build branches with only test-provided command stand-ins."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class DevelopmentBuildTest(unittest.TestCase):
    def run_branch(self, development, packaging_exit=0):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts/dev').mkdir(parents=True)
            (root / 'internal/releaseplan').mkdir(parents=True)
            (root / 'internal/releaseplan/components.json').write_text(json.dumps({
                'components': {'runtime': {'channel': 'development' if development else 'stable'}}}))
            for filename in ('development-build.sh', 'lib.sh'):
                shutil.copyfile(ROOT / 'scripts/dev' / filename, root / 'scripts/dev' / filename)
            fakebin = root / 'bin'
            fakebin.mkdir()
            commands = '''import json, os, pathlib, sys
root = pathlib.Path(os.environ['JPDEV_ROOT'])
with (root / 'commands').open('a') as log:
    log.write(pathlib.Path(sys.argv[0]).name + ' ' + ' '.join(sys.argv[1:]) + '\\n')
if pathlib.Path(sys.argv[0]).name == 'python3':
    if sys.argv[1] == 'scripts/package-release_test.py':
        sys.exit(int(os.environ['PACKAGING_EXIT']))
    if sys.argv[1] == '-c':
        plan = json.loads((root / 'internal/releaseplan/components.json').read_text())
        print(str(any(c['channel'] == 'development' for c in plan['components'].values())).lower())
    else:
        companions = root / 'companions'
        companions.mkdir()
        runner = companions / 'jpack-runner'
        runner.write_text('#!/bin/sh\\nexit 0\\n')
        runner.chmod(0o755)
        print(companions)
if pathlib.Path(sys.argv[0]).name == 'go':
    output = pathlib.Path(sys.argv[sys.argv.index('-o') + 1])
    output.write_text('#!/bin/sh\\nexit 0\\n')
    output.chmod(0o755)
'''
            for name in ('python3', 'npm', 'go'):
                program = fakebin / name
                program.write_text('#!' + sys.executable + '\n' + commands)
                program.chmod(0o755)
            # Only shell utilities used by the script are available, never installed companions.
            for name in ('dirname', 'mkdir', 'tee', 'tail', 'rm', 'cp'):
                (fakebin / name).symlink_to(shutil.which(name))
            env = dict(os.environ, PATH=str(fakebin), JPDEV_ROOT=str(root),
                       RUNNER_TEMP=str(root / 'scratch'), GH_TOKEN='test-token',
                       PACKAGING_EXIT=str(packaging_exit))
            result = subprocess.run(['/bin/bash', str(root / 'scripts/dev/development-build.sh')],
                                    env=env, capture_output=True, text=True)
            return result, (root / 'commands').read_text().splitlines()

    def test_packaging_runs_before_stable_exit(self):
        result, commands = self.run_branch(False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(commands[0], 'python3 scripts/package-release_test.py')
        self.assertEqual(len(commands), 2)
        self.assertIn('no development pin', result.stdout)

    def test_packaging_runs_on_development_path(self):
        result, commands = self.run_branch(True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(commands[0], 'python3 scripts/package-release_test.py')
        self.assertIn('python3 scripts/dev-components.py', commands)
        self.assertTrue(any(c.startswith('go build ') for c in commands))

    def test_packaging_failure_stops_each_branch(self):
        for development in (False, True):
            with self.subTest(development=development):
                result, commands = self.run_branch(development, 7)
                self.assertEqual(result.returncode, 7)
                self.assertEqual(commands, ['python3 scripts/package-release_test.py'])

    def test_stable_archive_matrix_runs_packaging_contracts(self):
        workflow = (ROOT / '.github/workflows/release-build.yml').read_text()
        self.assertIn('run: python3 scripts/package-release_test.py', workflow)
        self.assertLess(workflow.index('run: python3 scripts/package-release_test.py'),
                        workflow.index('run: python3 scripts/package-release.py'))
        for platform in ('linux_amd64', 'darwin_arm64', 'darwin_amd64'):
            self.assertIn('platform: ' + platform, workflow)


if __name__ == '__main__':
    unittest.main()
