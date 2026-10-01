import gzip
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('published', Path(__file__).with_name('published-components.py'))
p = importlib.util.module_from_spec(spec); spec.loader.exec_module(p)
PLAN = json.loads((Path(__file__).resolve().parents[1] / 'internal/releaseplan/components.json').read_text())


def archive(path, entries):
    # Two downloads of one published asset return the same bytes, even when
    # the test crosses a wall-clock second or uses different temporary paths.
    with path.open('wb') as output, gzip.GzipFile(fileobj=output, filename='', mode='wb', mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as stream:
            for name, data in entries:
                info = tarfile.TarInfo(name) if isinstance(name, str) else name
                info.size = len(data)
                stream.addfile(info, io.BytesIO(data))


class PublishedComponentsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.calls = []
        self.reject = None

    def command(self, args, **kwargs):
        self.calls.append(args)
        if args[:3] == ['gh', 'release', 'download']:
            directory = Path(args[args.index('--dir') + 1])
            asset = args[args.index('--pattern') + 1]
            name = args[args.index('--repo') + 1].removeprefix('Judgment-Pack/judgment-pack-')
            entries = [(file, ('published ' + file).encode()) for file in p.PROGRAMS[name] + p.NOTICES[name]]
            archive(directory / asset, entries)
            (directory / 'checksums.txt').write_text(p.digest(directory / asset) + '  ' + asset + '\n')
        elif args[:3] == ['gh', 'attestation', 'verify']:
            if self.reject:
                raise subprocess.CalledProcessError(1, args)
        elif args[:2] == ['gh', 'api']:
            return subprocess.CompletedProcess(args, 0, stdout=json.dumps({'encoding': 'base64', 'size': 3, 'content': 'e30K'}))
        else:
            raise AssertionError(args)
        return subprocess.CompletedProcess(args, 0)

    def fetch(self, name='runtime', platform='linux/amd64'):
        return p.fetch(name, PLAN['components'][name], platform, self.root / name)

    def test_published_programs_and_notices_preserve_bytes_for_every_component_and_platform(self):
        for platform in ('linux/amd64', 'darwin/amd64', 'darwin/arm64'):
            with self.subTest(platform=platform), patch.object(p, 'run', side_effect=self.command):
                bundle = self.root / platform / 'bundle'; bundle.mkdir(parents=True)
                with patch('gzip.time.time', return_value=1000):
                    p.install(PLAN, platform, bundle, self.root / platform / 'downloads')
                for name in p.PROGRAMS:
                    for file in p.PROGRAMS[name]:
                        self.assertEqual((bundle / file).read_bytes(), ('published ' + file).encode())
                        self.assertEqual((bundle / file).stat().st_mode & 0o777, 0o755)
                    self.assertEqual((bundle / (name + '-licenses/THIRD_PARTY_NOTICES')).read_bytes(), b'published THIRD_PARTY_NOTICES')
                manifest = json.loads((bundle / 'gateway-bundle.json').read_text())
                self.assertEqual(manifest['revision'], PLAN['components']['gateway']['revision'])
                self.assertEqual(set(manifest['files']), set(p.PROGRAMS['gateway']))
                for file, digest in manifest['files'].items():
                    self.assertEqual(digest, p.digest(bundle / file))
                with patch('gzip.time.time', return_value=1001):
                    p.verify_bundle(PLAN, platform, bundle, self.root / platform / 'checked')
        for name, pin in PLAN['components'].items():
            verified = [call for call in self.calls if call[:3] == ['gh', 'attestation', 'verify'] and pin['repository'] in call]
            self.assertEqual(len(verified), 6)
            for call in verified:
                self.assertEqual(call[call.index('--signer-workflow') + 1], pin['repository'] + '/.github/workflows/release.yml')
                self.assertEqual(call[call.index('--source-ref') + 1], 'refs/tags/' + pin['version'])
                self.assertEqual(call[call.index('--source-digest') + 1], pin['revision'])
                self.assertIn('--deny-self-hosted-runners', call)

    def test_failed_attestation_never_extracts_or_installs(self):
        self.reject = True
        with patch.object(p, 'run', side_effect=self.command), patch.object(p, 'read_archive') as read:
            with self.assertRaises(subprocess.CalledProcessError):
                self.fetch()
            read.assert_not_called()

    def test_checksum_refuses_tampering_before_attestation(self):
        def command(args, **kwargs):
            result = self.command(args, **kwargs)
            if args[:3] == ['gh', 'release', 'download']:
                path = next((self.root / 'runtime').glob('*.tar.gz'))
                archive(path, [(file, b'replaced') for file in p.PROGRAMS['runtime'] + p.NOTICES['runtime']])
            return result
        with patch.object(p, 'run', side_effect=command):
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                self.fetch()
            self.assertFalse(any(call[:3] == ['gh', 'attestation', 'verify'] for call in self.calls))

    def test_checksum_requires_exact_unique_entry(self):
        file = self.root / 'checksums.txt'; name = 'component.tar.gz'; line = 'a' * 64 + '  ' + name
        for text in [line + '\n' + line, 'a' * 64 + '  another.tar.gz', 'invalid  ' + name, 'a' * 64 + ' *' + name]:
            with self.subTest(text=text):
                file.write_text(text)
                with self.assertRaises(ValueError): p.checksum(file, name)
        file.write_text(line + '\n')
        self.assertEqual(p.checksum(file, name), 'a' * 64)

    def test_archive_rejects_traversal_links_duplicates_missing_and_oversized_entries(self):
        link = tarfile.TarInfo('jpack'); link.type = tarfile.SYMTYPE; link.linkname = 'elsewhere'
        special = tarfile.TarInfo('device'); special.type = tarfile.CHRTYPE
        for entries in [[('../escape', b'a')], [('/absolute', b'a')], [(link, b'')], [(special, b'')], [('jpack', b'a'), ('jpack', b'b')], [('unrelated', b'a')], [('nested/../escape', b'a')]]:
            with self.subTest(entries=entries):
                path = self.root / 'archive.tar.gz'; archive(path, entries)
                with self.assertRaises(ValueError): p.read_archive(path, ('jpack',))
        archive(path, [('jpack', b'long')])
        with patch.object(p, 'MAX_EXPANDED', 3), self.assertRaisesRegex(ValueError, 'expansion limit'):
            p.read_archive(path, ('jpack',))
        with patch.object(p, 'MAX_ARCHIVE', 1), self.assertRaisesRegex(ValueError, 'Oversized'):
            p.read_archive(path, ('jpack',))
        with patch.object(p, 'MAX_EXPANDED', 4):
            self.assertEqual(p.read_archive(path, ('jpack',)), {'jpack': b'long'})

    def test_refuses_invalid_identity_before_download(self):
        for key, value in [('repository', 'untrusted/runtime'), ('version', 'v0.24.0-rc'), ('revision', 'main')]:
            pin = {**PLAN['components']['runtime'], key: value}
            with self.subTest(key=key), patch.object(p, 'run') as run, self.assertRaises(ValueError):
                p.fetch('runtime', pin, 'linux/amd64', self.root / 'download')
            run.assert_not_called()
        with patch.object(p, 'run') as run, self.assertRaises(ValueError): self.fetch(platform='windows/amd64')
        run.assert_not_called()

    def test_later_rebuild_or_provenance_rewrite_fails_independent_archive_check(self):
        with patch.object(p, 'run', side_effect=self.command):
            bundle = self.root / 'bundle'; bundle.mkdir()
            p.install(PLAN, 'linux/amd64', bundle, self.root / 'downloads')
            (bundle / 'jpack').write_bytes(b'same version, rebuilt')
            with self.assertRaisesRegex(ValueError, 'differs from published'):
                p.verify_bundle(PLAN, 'linux/amd64', bundle, self.root / 'replaced-check')
            (bundle / 'jpack').write_bytes(b'published jpack')
            path = bundle / 'component-artifacts.json'; original = path.read_text(); record = json.loads(original)
            record['runtime']['revision'] = '0' * 40; path.write_text(json.dumps(record))
            with self.assertRaisesRegex(ValueError, 'provenance disagrees'):
                p.verify_bundle(PLAN, 'linux/amd64', bundle, self.root / 'provenance-check')
            record.pop('runtime'); path.write_text(json.dumps(record))
            with self.assertRaisesRegex(ValueError, 'Incomplete'):
                p.verify_bundle(PLAN, 'linux/amd64', bundle, self.root / 'missing-check')

    def test_publisher_registration_refuses_identity_without_echo(self):
        for response in [{'encoding':'base64','size':100,'content':'private'}, {'encoding':'none','size':0}, {'encoding':'base64','size':2,'content':'bnVsbA=='}]:
            with self.subTest(response=response), patch.object(p, 'run', return_value=subprocess.CompletedProcess([],0,stdout=json.dumps(response))):
                with self.assertRaisesRegex(ValueError, '^Public Desk bundles require an unconfigured Google registration') as error:
                    p.require_unregistered_gateway(PLAN['components']['gateway'])
                self.assertNotIn('private', str(error.exception))

if __name__ == '__main__': unittest.main()
