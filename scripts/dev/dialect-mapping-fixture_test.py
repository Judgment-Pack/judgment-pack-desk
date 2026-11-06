"""The new fixture setup cannot enable legacy Runtime tests or hide bad input."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('dialect-mapping-fixture.sh')
ROOT = SCRIPT.parent.parent.parent


class MappingRuntimeFixtureTests(unittest.TestCase):
    def run_setup(self, root, fixture, dedicated=None, legacy=None, owner_status=0):
        env = dict(os.environ)
        env.pop('DESK_DIALECT_MAPPING_RUNTIME', None)
        env.pop('JPACK_BIN', None)
        if dedicated is not None:
            env['DESK_DIALECT_MAPPING_RUNTIME'] = dedicated
        if legacy is not None:
            env['JPACK_BIN'] = legacy
        env['FIXTURE_PATH'] = str(fixture)
        env['OWNER_STATUS'] = str(owner_status)
        return subprocess.run([
            'bash', '-c', '. "$1"; root=$2; prepare_dialect_mapping_fixture || exit $?; '
            'printf "%s\\n%s\\n" "$DESK_DIALECT_MAPPING_RUNTIME" "${JPACK_BIN-UNSET}"',
            'fixture', str(SCRIPT), str(root),
        ], env=env, capture_output=True, text=True, timeout=5)

    def test_existing_owner_and_legacy_environment_are_separate(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            owner = root / 'scripts/dev/jobs-companion.sh'
            owner.parent.mkdir(parents=True)
            owner.write_text('#!/bin/sh\nprintf "%s\\n" "$1" >> "$(dirname "$0")/calls"\n'
                             'test "$OWNER_STATUS" = 0 || exit "$OWNER_STATUS"\n'
                             'printf "%s\\n" "$FIXTURE_PATH"\n')
            owner.chmod(0o700)
            fixture = root / 'owned runtime'
            fixture.write_text('#!/bin/sh\nexit 0\n')
            fixture.chmod(0o700)
            for legacy in (None, '/unchanged-legacy-fixture'):
                result = self.run_setup(root, fixture, legacy=legacy)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.splitlines(), [str(fixture), legacy or 'UNSET'])
            self.assertEqual(owner.with_name('calls').read_text().splitlines(),
                             ['dialect-mapping-runtime', 'dialect-mapping-runtime'])

    def test_explicit_fixture_never_uses_owner_or_operator_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture = root / 'owned-runtime'
            fixture.write_text('#!/bin/sh\nexit 0\n')
            fixture.chmod(0o700)
            # No owner exists. A valid explicit fixture needs no installer.
            result = self.run_setup(root, fixture, dedicated=str(fixture), legacy='/old-fixture')
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.splitlines(), [str(fixture), '/old-fixture'])
            for invalid in ('', 'relative-runtime', str(root / 'missing')):
                result = self.run_setup(root, fixture, dedicated=invalid, legacy=str(fixture))
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, '')
            fixture.chmod(0o600)
            result = self.run_setup(root, fixture, dedicated=str(fixture), legacy='/old-fixture')
            self.assertNotEqual(result.returncode, 0)

    def test_owner_refusal_does_not_fall_back(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            owner = root / 'scripts/dev/jobs-companion.sh'
            owner.parent.mkdir(parents=True)
            owner.write_text('#!/bin/sh\nexit 73\n')
            owner.chmod(0o700)
            result = self.run_setup(root, root / 'unused', legacy='/old-fixture', owner_status=73)
            self.assertEqual(result.returncode, 73)
            self.assertEqual(result.stdout, '')

    def test_both_go_mutation_paths_keep_the_dedicated_setup(self):
        for path in (ROOT / 'scripts/dev/mutation-rows.sh',
                     ROOT / '.github/workflows/mutation-nightly.yml'):
            text = path.read_text()
            self.assertIn('prepare_dialect_mapping_fixture', text)
            self.assertNotIn('export JPACK_BIN', text)
        for name in ('mutation-rows.yml', 'mutation-nightly.yml'):
            self.assertIn('GH_TOKEN: ${{ github.token }}',
                          (ROOT / '.github/workflows' / name).read_text())
        text = (ROOT / 'scripts/dev/jobs-companion.sh').read_text()
        self.assertIn('dialect-mapping-runtime)\n    install_official > /dev/null', text)


if __name__ == '__main__':
    unittest.main()
