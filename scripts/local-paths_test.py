import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('local_paths', Path(__file__).with_name('local-paths.py'))
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)


class LocalPathsTest(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        self.file = 'fixture.txt'

    def scan(self, text, exclusions=None):
        (self.root / self.file).write_text(text)
        return p.check(self.root, {self.file}, exclusions or {})

    def allowance(self, literal):
        return {self.file: {'reason': 'Synthetic refusal fixture', 'paths': [literal]}}

    def test_new_paths_fail_for_each_root_including_old_global_names(self):
        for directory in ('home', 'Users', 'tmp'):
            for name in ('someone', 'new-machine'):
                literal = '/' + directory + '/' + name + '/new-secret'
                with self.subTest(literal=literal):
                    self.assertTrue(any('machine-local path ' + literal in error for error in self.scan(literal)))

    def test_exact_literal_fixture_and_new_suffix_in_same_file(self):
        literal = '/' + 'home/someone/fixture'
        allowed = self.allowance(literal)
        self.assertEqual(self.scan(literal, allowed), [])
        for addition in (literal + '/secret', literal + '@secret', literal + '秘密', '/' + 'Users/owner/secret', '/' + 'tmp/new-secret'):
            with self.subTest(addition=addition):
                errors = self.scan(literal + '\n' + addition, allowed)
                self.assertEqual(len(errors), 1)
                self.assertIn('machine-local path ' + addition, errors[0])

    def test_allowance_is_scoped_to_file(self):
        literal = '/' + 'tmp/fixture'
        (self.root / 'other.txt').write_text(literal)
        (self.root / self.file).write_text(literal)
        errors = p.check(self.root, {self.file, 'other.txt'}, self.allowance(literal))
        self.assertEqual(len(errors), 1)
        self.assertIn('other.txt:1:', errors[0])

    def test_literal_star_does_not_allow_a_pattern(self):
        literal = '/' + 'tmp/fixture-*.png'
        allowed = self.allowance(literal)
        self.assertEqual(self.scan(literal, allowed), [])
        errors = self.scan(literal + '\n' + '/' + 'tmp/fixture-new.png', allowed)
        self.assertEqual(len(errors), 1)
        self.assertIn('machine-local path', errors[0])

    def test_stale_unknown_and_invalid_exclusions_fail(self):
        literal = '/' + 'tmp/fixture'
        self.assertIn('stale exclusion', self.scan('removed', self.allowance(literal))[0])
        errors = self.scan('safe', {'missing.txt': {'reason': 'Fixture', 'paths': [literal]}})
        self.assertTrue(any('unknown exclusion file' in error for error in errors))
        for entry in ({'paths': [literal]}, {'reason': '', 'paths': [literal]},
                      {'reason': 'Fixture', 'paths': ['/' + 'tmp/*']},
                      {'reason': 'Fixture', 'paths': []}):
            with self.subTest(entry=entry):
                self.assertTrue(self.scan('safe', {self.file: entry}))

    def test_binary_and_root_names_without_path_are_not_leaks(self):
        self.assertEqual(self.scan('relative/tmp/example /tmp /home /Users'), [])
        (self.root / self.file).write_bytes(b'\0' + b'/' + b'tmp/binary')
        self.assertEqual(p.check(self.root, {self.file}, {}), [])

    def test_cli_fails_closed_for_git_missing_file_and_bad_inventory(self):
        (self.root / 'scripts').mkdir()
        script = self.root / 'scripts' / 'local-paths.py'
        script.write_bytes(Path(p.__file__).read_bytes())
        inventory = self.root / p.EXCLUSIONS
        inventory.write_text('{}')
        fakebin = self.root / 'bin'
        fakebin.mkdir()
        git = fakebin / 'git'
        git.write_text('#!' + sys.executable + '\nimport os,sys\nsys.stdout.write(os.environ.get("TRACKED", ""))\nsys.exit(int(os.environ.get("GIT_EXIT", "0")))\n')
        git.chmod(0o755)
        env = dict(os.environ, PATH=str(fakebin), TRACKED='missing.txt')
        for overrides in ({'GIT_EXIT': '2'}, {}, {'TRACKED': ''}):
            with self.subTest(overrides=overrides):
                result = subprocess.run([sys.executable, str(script)], env=dict(env, **overrides), capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b'cannot complete scan', result.stderr)
        env['TRACKED'] = p.EXCLUSIONS
        inventory.write_text('[]')
        result = subprocess.run([sys.executable, str(script)], env=env, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'exclusions must be a file mapping', result.stderr)

    def test_manifest_wires_guard_without_swallowing_failures(self):
        manifest = json.loads(Path(__file__).resolve().parent.parent.joinpath('dev.json').read_text())
        command = next(c['run'] for c in manifest['checks'] if c['name'] == 'local-paths')
        with patch.object(p.subprocess, 'run', side_effect=subprocess.CalledProcessError(2, 'git')):
            self.assertEqual(p.main(), 1)
        self.assertEqual(command, 'python3 scripts/local-paths.py')


if __name__ == '__main__':
    unittest.main()
