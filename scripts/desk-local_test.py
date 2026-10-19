import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
spec = importlib.util.spec_from_file_location('local', Path(__file__).with_name('desk-local.py'))
u = importlib.util.module_from_spec(spec); spec.loader.exec_module(u)

class LocalLaunchTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(); self.addCleanup(temp.cleanup)
        self.base = Path(temp.name)
        self.root = self.base / 'install'; self.root.mkdir(mode=0o700)
        self.managed = b'{"schemaVersion":1,"current":{"version":"0.12.0"},"autoInstall":true}'
        (self.root / 'installation.json').write_bytes(self.managed)
        self.source = self.base / 'source'; self.source.mkdir(mode=0o700)
        for name in u.REQUIRED - {'gateway-bundle.json'}:
            file = self.source / name
            file.write_text('#!/bin/sh\nexit 0\n'); file.chmod(0o700)
        (self.source / 'jpack-desk').write_text('#!/bin/sh\n[ "$1" = --desk-home-layout-version ] && { echo 1; exit 0; }\nprintf "%s\\n" "$@"\n')
        (self.source / 'jpack-runner').write_text('#!/bin/sh\necho \'{"version":1}\'\n')
        (self.source / 'gateway-bundle.json').write_text(json.dumps({'files': {'gateway': u.digest(self.source / 'gateway')}}))
        self.manifest()
    def manifest(self):
        doc = {'schemaVersion':1, 'kind':'local-development', 'stateEpoch':1,
               'files':{p.name:u.digest(p) for p in self.source.iterdir() if p.name != 'local-manifest.json'}}
        (self.source / 'local-manifest.json').write_text(json.dumps(doc))
    def test_install_and_relocate_preserves_managed_release_and_selection(self):
        identifier = u.install(self.root, self.source)
        self.assertEqual((self.root / 'installation.json').read_bytes(), self.managed)
        self.assertEqual(u.check(self.root), self.root / 'local-builds' / identifier)
        moved = self.base / 'moved'; self.root.rename(moved)
        self.assertEqual(u.check(moved), moved / 'local-builds' / identifier)
        # Actual installed script remains self-contained after its source is gone.
        shutil.rmtree(self.source)
        self.assertEqual(subprocess.check_output(['python3', str(moved/'desk-local.py'), '--root', str(moved), 'check']).strip(), b'1')
    def test_tamper_and_unlisted_files_are_refused_without_old_release_fallback(self):
        u.install(self.root, self.source)
        bundle = u.selected(self.root)
        (bundle / 'jpack').write_text('changed')
        with self.assertRaisesRegex(ValueError, 'changed'): u.check(self.root)
        (self.source / 'extra').write_text('unlisted')
        with self.assertRaisesRegex(ValueError, 'unlisted'): u.verify(self.source, u.digest(self.source / 'local-manifest.json'))
    def test_selection_traversal_and_changed_helper_are_refused(self):
        u.install(self.root, self.source)
        marker = json.loads((self.root / 'local-launch.json').read_text()); marker['manifestDigest'] = '../escape'
        (self.root / 'local-launch.json').write_text(json.dumps(marker))
        with self.assertRaisesRegex(ValueError, 'selection'): u.selected(self.root)
        u.install(self.root, self.source)
        (self.root / 'desk-local.py').write_text('changed')
        with self.assertRaisesRegex(ValueError, 'launcher changed'): u.selected(self.root)
    def test_links_are_refused(self):
        file = self.source / 'jpack'; content = file.read_bytes(); file.unlink()
        outside = self.base / 'outside'; outside.write_bytes(content); file.symlink_to(outside)
        with self.assertRaisesRegex(ValueError, 'unsafe'): u.verify(self.source, u.digest(self.source / 'local-manifest.json'))
    def test_unsupported_bundle_does_not_publish_selection(self):
        (self.source / 'jpack-desk').write_text('#!/bin/sh\nexit 2\n')
        self.manifest()
        with self.assertRaises(subprocess.CalledProcessError): u.install(self.root, self.source)
        self.assertFalse((self.root / 'local-launch.json').exists())
        self.assertEqual((self.root / 'installation.json').read_bytes(), self.managed)
    def test_busy_install_preserves_selection(self):
        with u.running_lock(self.root):
            with self.assertRaisesRegex(ValueError, 'running'): u.install(self.root, self.source)
        self.assertFalse((self.root / 'local-launch.json').exists())
    def test_launch_pins_components_and_discards_foreign_gateway_overrides(self):
        u.install(self.root, self.source); bundle = u.selected(self.root)
        with patch.object(u.subprocess, 'Popen') as start, patch.object(u, 'check', return_value=bundle), patch.dict(os.environ, {'JPACK_DESK_GATEWAY_BUNDLE':'/other','JPACK_DESK_GATEWAY_MANIFEST_SHA256':'a'*64}):
            start.return_value.wait.return_value = 0
            self.assertEqual(u.launch(self.root, ['--', '--port', '5173']), 0)
            self.assertEqual(start.call_args.args[0], [str(bundle/'jpack-desk'), '--jpack', str(bundle/'jpack'), '--runner', str(bundle/'jpack-runner'), '--port', '5173'])
            self.assertNotIn('JPACK_DESK_GATEWAY_BUNDLE', start.call_args.kwargs['env'])
            self.assertNotIn('JPACK_DESK_GATEWAY_MANIFEST_SHA256', start.call_args.kwargs['env'])
        for arg in ['--runner=/other', '-jpack', '--dev-token=x']:
            with self.assertRaisesRegex(ValueError, 'saved components'): u.launch(self.root, [arg])

if __name__ == '__main__': unittest.main()
