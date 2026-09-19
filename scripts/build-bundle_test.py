import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('build_bundle', Path(__file__).with_name('build-bundle.py'))
bundle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bundle)


class PublicBundleRegistrationTest(unittest.TestCase):
    def check(self, raw):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory)
            path = source / 'adapters/cmd/gateway-connections/publisher-google.json'
            path.parent.mkdir(parents=True)
            if raw is not None:
                path.write_bytes(raw)
            bundle.require_unregistered_gateway(source)

    def test_empty_upstream_registration_is_allowed(self):
        self.check(b'{}\n')

    def test_refuses_embedded_identity_or_an_unrecognized_archive_without_echo(self):
        for raw in [None, b'', b'null', b'{"installed":{"client_id":"do-not-echo.apps.googleusercontent.com"}}',
                    b'{}{}', b'{}' + b' ' * 70 + b'private', b'{"client_secret":"do-not-echo"}', b' ' * 65 + b'{}', b'{}\n' + b'x' * 1024]:
            with self.subTest(case=raw), self.assertRaisesRegex(ValueError, '^Public Desk bundles require an unconfigured Google registration') as error:
                self.check(raw)
            self.assertNotIn('do-not-echo', str(error.exception))

    def test_publisher_embedding_flags_are_rejected_before_writing_a_bundle(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'bundle'
            result = subprocess.run([sys.executable, str(Path(__file__).with_name('build-bundle.py')),
                '--google-oauth-client', str(Path(directory) / 'registration.json'), '--require-google-oauth',
                '--output', str(output)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn('unrecognized arguments', result.stderr)
            self.assertFalse(output.exists())


if __name__ == '__main__':
    unittest.main()
