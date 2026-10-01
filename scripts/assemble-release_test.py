import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('assemble', Path(__file__).with_name('assemble-release.py'))
a = importlib.util.module_from_spec(spec)
spec.loader.exec_module(a)


class AssemblyTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.output = self.root / 'out'
        for platform in a.PLATFORMS:
            directory = self.root / ('desk-release-' + platform)
            directory.mkdir()
            (directory / ('judgment-pack-desk_1.2.3_' + platform + '.tar.gz')).write_bytes(platform.encode())
            plan = a.components.read_plan()
            doc = dict(formatVersion=1, stateEpoch=plan['stateEpoch'], version='1.2.3', platform=platform.replace('_', '/'),
                       desk={'repository': 'https://github.com/Judgment-Pack/judgment-pack-desk', 'revision': 'a'*40}, **plan['components'])
            (directory / ('release-manifest_' + platform + '.json')).write_text(json.dumps(doc))
            self.checksums(directory)

    def checksums(self, directory):
        (directory / 'checksums.txt').write_text(''.join(a.digest(p)+'  '+p.name+'\n' for p in sorted(directory.iterdir()) if p.name != 'checksums.txt'))

    def test_complete_set_has_unique_manifests_combined_checksums_and_linux_alias(self):
        a.assemble(self.root, self.output, '1.2.3')
        self.assertEqual(len(list(self.output.glob('*.tar.gz'))), 3)
        self.assertEqual((self.output/'release-manifest.json').read_bytes(), (self.output/'release-manifest_linux_amd64.json').read_bytes())
        lines = (self.output/'checksums.txt').read_text().splitlines()
        self.assertEqual(len(lines), 7)
        for line in lines:
            digest, name = line.split()
            self.assertEqual(digest, a.digest(self.output/name))

    def test_missing_or_changed_artifact_prevents_assembly(self):
        archive = next((self.root/'desk-release-darwin_arm64').glob('*.tar.gz'))
        archive.write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'): a.assemble(self.root, self.output, '1.2.3')
        self.assertFalse(self.output.exists())
        archive.unlink()
        with self.assertRaises(ValueError): a.assemble(self.root, self.output, '1.2.3')
        self.assertFalse(self.output.exists())

    def test_platform_revision_or_component_disagreement_prevents_assembly(self):
        directory = self.root/'desk-release-darwin_amd64'
        manifest = directory/'release-manifest_darwin_amd64.json'
        original = json.loads(manifest.read_text())
        for field in ('platform', 'revision', 'component'):
            with self.subTest(field=field):
                doc = json.loads(json.dumps(original))
                if field == 'platform': doc['platform'] = 'darwin/arm64'
                elif field == 'revision': doc['desk']['revision'] = 'b'*40
                else: doc['runtime']['revision'] = 'b'*40
                manifest.write_text(json.dumps(doc)); self.checksums(directory)
                with self.assertRaises(ValueError): a.assemble(self.root, self.output, '1.2.3')
                self.assertFalse(self.output.exists())


if __name__ == '__main__': unittest.main()
