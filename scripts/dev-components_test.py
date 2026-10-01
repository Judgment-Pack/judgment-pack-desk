#!/usr/bin/env python3
"""Companion drift, cache verification and failed-update isolation."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('companions', Path(__file__).with_name('dev-components.py'))
c = importlib.util.module_from_spec(spec)
spec.loader.exec_module(c)
REAL_IDENTITY = c.identity


class CompanionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.plan = copy.deepcopy(c.release.components.read_plan())
        self.identity = {'components': self.plan['components'], 'platform': 'test'}
        for name, value in [('ROOT', self.root), ('identity', lambda plan: {'components': plan['components'], 'platform': 'test'})]:
            item = patch.object(c, name, value)
            item.start(); self.addCleanup(item.stop)
        item = patch.object(c.release.components, 'read_plan', side_effect=lambda: self.plan)
        item.start(); self.addCleanup(item.stop)

    def fixture_build(self, plan, output):
        for name in c.EXECUTABLES:
            file = output / name
            file.write_text(name + plan['components']['runner']['revision'])
            file.chmod(0o700)
        gateway = {'revision': plan['components']['gateway']['revision'],
                   'files': {name: c.digest(output / name) for name in ('gateway', 'adapter-document', 'gateway-connections')}}
        (output / 'gateway-bundle.json').write_text(json.dumps(gateway))

    def synchronize(self):
        with patch.object(c, 'build', side_effect=self.fixture_build):
            return c.synchronize()

    def test_matching_cache_needs_no_fetch_or_build(self):
        installed = self.synchronize()
        with patch.object(c, 'build') as build:
            self.assertEqual(c.synchronize(), installed)
        build.assert_not_called()

    def test_source_worker_tampering_is_repaired_in_new_directory(self):
        original = self.synchronize()
        (original / 'jpack-source-worker').write_text('old worker')
        replacement = self.synchronize()
        self.assertNotEqual(replacement, original)
        self.assertEqual((original / 'jpack-source-worker').read_text(), 'old worker')
        self.assertTrue(c.verify(replacement, self.identity))

    def test_missing_worker_nonexecutable_and_corrupt_manifest_are_not_current(self):
        for problem in ('missing', 'nonexecutable', 'manifest'):
            with self.subTest(problem=problem):
                installed = self.synchronize()
                if problem == 'missing': (installed / 'jpack-source-worker').unlink()
                elif problem == 'nonexecutable': (installed / 'jpack').chmod(0o600)
                else: (installed / c.MANIFEST).write_text('{}')
                self.assertFalse(c.verify(installed, self.identity))
                self.assertNotEqual(self.synchronize(), installed)

    def test_lock_change_rebuilds_even_when_display_version_is_unchanged(self):
        old = self.synchronize()
        self.plan['components']['runner']['revision'] = 'f' * 40
        new = self.synchronize()
        self.assertNotEqual(new, old)
        self.assertIn('f' * 40, (new / 'jpack-runner').read_text())
        self.assertNotIn('f' * 40, (old / 'jpack-runner').read_text())

    def test_failed_build_preserves_old_bundle_and_removes_partial_output(self):
        old = self.synchronize()
        before = (old / 'jpack-runner').read_bytes()
        self.plan['components']['runner']['revision'] = 'f' * 40
        def fail(plan, output):
            (output / 'jpack-runner').write_text('partial')
            raise RuntimeError('build failed')
        with patch.object(c, 'build', side_effect=fail), self.assertRaisesRegex(RuntimeError, 'build failed'):
            c.synchronize()
        self.assertEqual((old / 'jpack-runner').read_bytes(), before)
        self.assertEqual(list((self.root / 'bin/dev-components').iterdir()), [old])

    def test_rejects_incomplete_build_before_publishing_it(self):
        with patch.object(c, 'build'), self.assertRaisesRegex(RuntimeError, 'verification failed'):
            c.synchronize()
        self.assertEqual(list((self.root / 'bin/dev-components').iterdir()), [])

    def test_manifest_cannot_read_outside_bundle_or_follow_symlinks(self):
        installed = self.synchronize()
        external = self.root / 'external'
        external.write_text('private')
        manifest = json.loads((installed / c.MANIFEST).read_text())
        for path in ('../external', str(external)):
            changed = copy.deepcopy(manifest)
            changed['files'][path] = c.digest(external)
            (installed / c.MANIFEST).write_text(json.dumps(changed))
            self.assertFalse(c.verify(installed, self.identity))
        (installed / c.MANIFEST).write_text(json.dumps(manifest))
        (installed / 'jpack').unlink()
        (installed / 'jpack').symlink_to(external)
        self.assertFalse(c.verify(installed, self.identity))

    def test_unlisted_files_are_not_copied_into_a_launch(self):
        installed = self.synchronize()
        (installed / 'unexpected').write_text('not verified')
        self.assertFalse(c.verify(installed, self.identity))

    def test_wrong_gateway_revision_cannot_pass_with_matching_checksums(self):
        def wrong_gateway(plan, output):
            self.fixture_build(plan, output)
            path = output / 'gateway-bundle.json'
            value = json.loads(path.read_text()); value['revision'] = 'f' * 40
            path.write_text(json.dumps(value))
        with patch.object(c, 'build', side_effect=wrong_gateway), self.assertRaisesRegex(RuntimeError, 'verification failed'):
            c.synchronize()


def go_version(revision, modified):
    """`go version -m` output in Go's own layout."""
    lines = ['/out/binary: go1.26.5', '\tpath\texample/cmd', '\tbuild\t-trimpath=true', '\tbuild\tvcs=git']
    if revision is not None:
        lines.append('\tbuild\tvcs.revision=' + revision)
    return '\n'.join(lines + ['\tbuild\tvcs.modified=' + modified]) + '\n'


class BuildTests(unittest.TestCase):
    """The recipe itself: exact locked sources, a fixed Go environment, and a
    refusal of any companion whose embedded VCS stamp is not the locked commit."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name) / 'out'
        self.output.mkdir()
        self.plan = copy.deepcopy(c.release.components.read_plan())
        self.runs, self.sources = [], []

    def build(self, stamp):
        def source(component, destination):
            self.sources.append(dict(component))
            destination.mkdir(parents=True)
        def run(args, **kwargs):
            self.runs.append((args, kwargs))
        def check_output(args, **kwargs):
            if args[:2] == ['go', 'env']:
                return 'linux\namd64\n'
            binary = Path(args[-1]).name
            component = 'runtime' if binary == 'jpack' else 'runner'
            return stamp(self.plan['components'][component]['revision'])
        with patch.object(c.release, 'source', side_effect=source), patch.object(c.release, 'licenses'), \
             patch.object(c.subprocess, 'run', side_effect=run), patch.object(c.subprocess, 'check_output', side_effect=check_output), \
             patch.dict(c.os.environ, {'GOFLAGS': '-overlay=/elsewhere.json', 'GOWORK': '/elsewhere/go.work', 'GOOS': 'windows'}):
            c.build(self.plan, self.output)

    def test_builds_each_component_from_its_locked_source_with_a_fixed_environment(self):
        self.build(lambda revision: go_version(revision, 'false'))
        self.assertEqual(self.sources, list(self.plan['components'].values()))
        self.assertEqual(len(self.runs), 4)
        for args, kwargs in self.runs:
            env = kwargs['env']
            self.assertEqual((env['GOFLAGS'], env['GOWORK'], env['CGO_ENABLED'], env['GOOS'], env['GOARCH']),
                             ('-mod=readonly', 'off', '0', 'linux', 'amd64'))
        self.assertIn('--gateway-only', self.runs[0][0])
        built = {args[args.index('-o') + 1]: args for args, _ in self.runs[1:]}
        self.assertEqual(sorted(Path(name).name for name in built), ['jpack', 'jpack-runner', 'jpack-source-worker'])
        for args in built.values():
            self.assertIn('-buildvcs=true', args)

    def test_refuses_a_companion_stamped_with_another_commit_or_local_changes(self):
        for stamp in (lambda revision: go_version('f' * 40, 'false'),
                      lambda revision: go_version(revision, 'true'),
                      lambda revision: go_version(revision + 'f', 'false'),
                      lambda revision: go_version(None, 'false'),
                      lambda revision: go_version(revision, 'false').replace('\tbuild\tvcs.modified', '\tbuild\tvcs.modified.note')):
            with self.subTest(stamp=stamp), self.assertRaisesRegex(RuntimeError, 'does not match its locked source'):
                self.build(stamp)


class IdentityTests(unittest.TestCase):
    def test_lock_platform_and_every_recipe_script_key_the_cache(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'scripts').mkdir()
            names = ('dev-components.py', 'build-bundle.py', 'package-release.py', 'component-releases.py')
            for name in names:
                (root / 'scripts' / name).write_text(name)
            plan = c.release.components.read_plan()
            with patch.object(c, 'ROOT', root):
                original = REAL_IDENTITY(plan)
                self.assertEqual(original['components'], plan['components'])
                self.assertEqual(original['platform'], c.platform.system() + '/' + c.platform.machine())
                for name in names:
                    (root / 'scripts' / name).write_text(name + ' changed')
                    self.assertNotEqual(REAL_IDENTITY(plan), original, name)
                    (root / 'scripts' / name).write_text(name)


if __name__ == '__main__':
    unittest.main()
