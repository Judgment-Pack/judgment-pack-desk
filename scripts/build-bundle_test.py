import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('build_bundle', Path(__file__).with_name('build-bundle.py'))
bundle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bundle)


class PublisherRegistrationTest(unittest.TestCase):
    def parse(self, raw):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'client.json'
            path.write_bytes(raw.encode())
            return bundle.publisher_registration(path)

    def test_only_desktop_client_identity_enters_the_bundle(self):
        raw = json.dumps({'installed': {'client_id': 'test.apps.googleusercontent.com',
            'client_secret': 'test-secret', 'token_uri': 'https://wrong.example/token',
            'redirect_uris': ['https://wrong.example/callback']}, 'unrelated': 'private'})
        result = json.loads(self.parse(raw))
        self.assertEqual(result, {'installed': {'client_id': 'test.apps.googleusercontent.com', 'client_secret': 'test-secret'}})

    def test_secret_is_optional_for_desktop_clients(self):
        self.assertEqual(json.loads(self.parse('{"installed":{"client_id":"test.apps.googleusercontent.com"}}'))['installed']['client_secret'], '')

    def test_rejects_wrong_client_types_duplicates_and_limits_without_echoing_input(self):
        valid = {'client_id': 'test.apps.googleusercontent.com', 'client_secret': 'do-not-echo'}
        values = ['null', '[]', '{}', json.dumps({'web': valid}), json.dumps({'type': 'service_account', 'private_key': 'do-not-echo'}),
            json.dumps({'installed': valid, 'web': {}}), json.dumps({'installed': valid, 'type': 'service_account'}),
            '{"installed":{"client_id":"test.apps.googleusercontent.com","client_id":"other.apps.googleusercontent.com"}}',
            '{"installed":{"client_id":"test.apps.googleusercontent.com","client_secret":"\\ud800"}}',
            json.dumps({'installed': {**valid, 'client_id': 'wrong'}}),
            json.dumps({'installed': {**valid, 'client_secret': 42}}),
            json.dumps({'installed': {**valid, 'client_secret': 'line\nbreak'}}),
            json.dumps({'installed': {**valid, 'client_secret': 'a' * 4097}}),
            json.dumps({'installed': valid, 'extra': 'a' * 16_384})]
        for raw in values:
            with self.subTest(case=values.index(raw)), self.assertRaisesRegex(ValueError, '^Choose a valid Google Desktop app registration JSON') as error:
                self.parse(raw)
            self.assertNotIn('do-not-echo', str(error.exception))


if __name__ == '__main__':
    unittest.main()
