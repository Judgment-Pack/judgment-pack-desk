#!/usr/bin/env python3
"""Lifecycle safety checks without touching a developer's running Desk."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('desk_dev', Path(__file__).with_name('desk-dev.py'))
dev = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dev)


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.state = Path(self.temp.name)
        self.patch = patch.object(dev, 'STATE', self.state)
        self.patch.start()
        self.addCleanup(self.patch.stop)
        self.children = []
        self.addCleanup(self.cleanup_children)
        self.old_handler = signal.getsignal(signal.SIGTERM)
        self.addCleanup(signal.signal, signal.SIGTERM, self.old_handler)

    def spawn(self, code='import time; time.sleep(60)', session=True):
        child = subprocess.Popen([sys.executable, '-c', code], start_new_session=session,
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.children.append(child)
        return child

    def cleanup_children(self):
        for child in self.children:
            if child.poll() is None:
                child.kill()
            child.wait()

    def main(self, action):
        with patch.object(sys, 'argv', ['desk-dev.py', action]), contextlib.redirect_stdout(io.StringIO()):
            dev.main()

    def test_stop_preserves_untracked_process(self):
        tracked = self.spawn()
        unrelated = self.spawn()
        dev.stop({'backend': dev.identity(tracked.pid)})
        tracked.wait(timeout=3)
        self.assertIsNone(unrelated.poll())
        self.assertEqual(dev.read_state(), {})

    def test_stop_ignores_reused_pid_and_other_boot(self):
        child = self.spawn()
        for field in ('start', 'boot'):
            record = dev.identity(child.pid)
            record[field] = 'not-the-original-process'
            dev.stop({'backend': record})
            self.assertIsNone(child.poll())

    def test_stop_refuses_process_that_is_not_group_leader(self):
        child = self.spawn(session=False)
        dev.stop({'backend': dev.identity(child.pid)})
        self.assertIsNone(child.poll())

    def test_stop_cleans_up_child_after_group_leader_exits(self):
        child_pid_file = self.state / 'child.pid'
        ready_file = self.state / 'child.ready'
        stubborn_code = (
            'import signal,time; from pathlib import Path; '
            'signal.signal(signal.SIGTERM, signal.SIG_IGN); '
            f'Path({str(ready_file)!r}).touch(); time.sleep(60)'
        )
        code = (
            'import subprocess,sys,time; from pathlib import Path; '
            f'p=subprocess.Popen([sys.executable,"-c",{stubborn_code!r}]); '
            f'Path({str(child_pid_file)!r}).write_text(str(p.pid)); time.sleep(60)'
        )
        leader = self.spawn(code)
        deadline = time.monotonic() + 5
        while not ready_file.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.assertTrue(ready_file.exists())
        child_pid = int(child_pid_file.read_text())
        self.addCleanup(lambda: dev.os.kill(child_pid, signal.SIGKILL) if dev.identity(child_pid) else None)
        dev.stop({'backend': dev.identity(leader.pid)})
        leader.wait(timeout=3)
        self.assertIsNone(dev.identity(child_pid))

    def test_start_is_idempotent(self):
        processes = {name: dev.identity(self.spawn().pid) for name in ('backend', 'frontend')}
        dev.write_state(processes)
        with patch.object(dev, 'prepare') as prepare, patch.object(dev, 'launch') as launch:
            self.main('start')
        prepare.assert_not_called()
        launch.assert_not_called()
        self.assertEqual(dev.read_state(), processes)

    def test_failed_build_does_not_stop_running_servers(self):
        processes = {name: dev.identity(self.spawn().pid) for name in ('backend', 'frontend')}
        dev.write_state(processes)
        with patch.object(dev, 'prepare', side_effect=subprocess.CalledProcessError(1, 'go')):
            with self.assertRaises(subprocess.CalledProcessError):
                self.main('restart')
        self.assertTrue(all(dev.alive(record) for record in processes.values()))
        self.assertEqual(dev.read_state(), processes)

    def test_failed_start_cleans_up_other_started_server(self):
        commands = [
            ('backend', [sys.executable, '-c', 'import time; time.sleep(60)'], self.state, None, 'unused'),
            ('frontend', [sys.executable, '-c', 'raise SystemExit(1)'], self.state, None, 'unused'),
        ]
        records = []
        save = dev.write_state
        def record_state(processes):
            records.extend(processes.values())
            save(processes)
        with patch.object(dev, 'ready', return_value=False), patch.object(dev, 'write_state', side_effect=record_state):
            with self.assertRaises(RuntimeError):
                dev.launch(commands)
        self.assertFalse(any(dev.alive(record) for record in records))
        self.assertEqual(dev.read_state(), {})

    def test_occupied_port_refused_without_signalling(self):
        fake_socket = unittest.mock.MagicMock()
        fake_socket.__enter__.return_value.bind.side_effect = OSError(98, 'Address already in use')
        with patch.object(dev.socket, 'socket', return_value=fake_socket), patch.object(dev.os, 'killpg') as kill:
            with self.assertRaisesRegex(RuntimeError, 'untracked process'):
                dev.require_free_ports()
        kill.assert_not_called()

    def test_state_from_another_checkout_is_refused(self):
        (self.state / 'processes.json').write_text(json.dumps({'version': 1, 'root': '/elsewhere', 'processes': {}}))
        with self.assertRaisesRegex(RuntimeError, 'no processes were stopped'):
            self.main('stop')

    def checkout(self):
        root = self.state / 'checkout'
        vite = root / 'web/node_modules/vite/bin/vite.js'
        vite.parent.mkdir(parents=True); vite.touch()
        bundle = root / 'bin/dev-components/key-verified'
        bundle.mkdir(parents=True)
        for name in ('jpack', 'jpack-runner', 'jpack-source-worker', 'gateway-bundle.json'):
            (bundle / name).write_text('verified companion')
        # An executable jpack on PATH that the launcher must not pick up.
        path_jpack = self.state / 'path/jpack'
        path_jpack.parent.mkdir(); path_jpack.write_text('#!/bin/sh\n'); path_jpack.chmod(0o755)
        return root, bundle, path_jpack

    def prepare(self, root, bundle, path_jpack, environ, build=None):
        def compile_desk(args, **kwargs):
            output = Path(args[args.index('-o') + 1])
            # A build that fails here leaves a staging directory prune can remove.
            self.assertTrue((output.parent / dev.LAUNCH_MARKER).is_file())
            output.write_text('desk binary')
        inherited = {'PATH': str(path_jpack.parent), 'JPACK_DESK_GATEWAY_MANIFEST_SHA256': 'a' * 64}
        which = lambda name: str(path_jpack) if name == 'jpack' else '/go'
        # Verification of the copy is dev-components' own test; here it copies.
        install = lambda source, destination: shutil.copytree(source, destination, dirs_exist_ok=True)
        with patch.object(dev, 'ROOT', root), patch.object(dev, 'node_environment', return_value=('/node', inherited)), \
             patch.object(dev.shutil, 'which', side_effect=which), patch.object(dev.components, 'synchronize', return_value=bundle), \
             patch.object(dev.components, 'install', side_effect=install), \
             patch.dict(dev.os.environ, environ), patch.object(dev.subprocess, 'run', side_effect=build or compile_desk), \
             contextlib.redirect_stdout(io.StringIO()) as out:
            return dev.prepare(), out.getvalue()

    def test_prepare_uses_isolated_locked_binaries_instead_of_path(self):
        root, bundle, path_jpack = self.checkout()
        commands, _ = self.prepare(root, bundle, path_jpack, {'JPACK_DESK_JPACK': ''})
        backend = commands[0][1]
        installed = Path(backend[0]).parent
        self.assertEqual(installed.parent, root / 'bin/dev-launches')
        self.assertEqual(backend[backend.index('--jpack') + 1], str(installed / 'jpack'))
        self.assertNotIn(str(path_jpack), backend)
        self.assertEqual(backend[backend.index('--runner') + 1], str(installed / 'jpack-runner'))
        self.assertEqual((installed / 'jpack-source-worker').read_text(), 'verified companion')
        self.assertTrue((installed / dev.LAUNCH_MARKER).is_file())
        self.assertNotEqual((installed / 'jpack').stat().st_ino, (bundle / 'jpack').stat().st_ino)
        self.assertFalse((bundle / 'jpack-desk').exists())
        self.assertNotIn('JPACK_DESK_GATEWAY_MANIFEST_SHA256', commands[0][3])
        self.assertEqual(commands[0][3]['JPACK_DESK_LOCAL_ACCESS'], '1')

    def test_runtime_override_is_used_and_reported_as_outside_the_lock(self):
        root, bundle, path_jpack = self.checkout()
        override = self.state / 'override-jpack'
        override.write_text('#!/bin/sh\n'); override.chmod(0o755)
        commands, out = self.prepare(root, bundle, path_jpack, {'JPACK_DESK_JPACK': str(override)})
        backend = commands[0][1]
        self.assertEqual(backend[backend.index('--jpack') + 1], str(override.resolve()))
        self.assertIn('Runtime override enabled', out)
        override.chmod(0o644)
        with self.assertRaisesRegex(RuntimeError, 'JPACK_DESK_JPACK must name an executable'):
            self.prepare(root, bundle, path_jpack, {'JPACK_DESK_JPACK': str(override)})

    def test_a_failed_build_deletes_nothing_and_leaves_its_staging_marked(self):
        root, bundle, path_jpack = self.checkout()
        def fail(args, **kwargs):
            Path(args[args.index('-o') + 1]).write_text('partial')
            raise subprocess.CalledProcessError(1, 'go')
        with self.assertRaises(subprocess.CalledProcessError):
            self.prepare(root, bundle, path_jpack, {'JPACK_DESK_JPACK': ''}, build=fail)
        staged = list((root / 'bin/dev-launches').iterdir())
        self.assertEqual(len(staged), 1)
        self.assertTrue(staged[0].name.startswith('.building-'))
        self.assertEqual((staged[0] / 'jpack-desk').read_text(), 'partial')
        self.assertTrue((staged[0] / dev.LAUNCH_MARKER).is_file())
        self.assertEqual(sorted(path.name for path in bundle.iterdir()), ['gateway-bundle.json', 'jpack', 'jpack-runner', 'jpack-source-worker'])
        self.assertEqual((bundle / 'jpack').read_text(), 'verified companion')
        # A copy that fails verification is marked for prune as well.
        with patch.object(dev.components, 'install', side_effect=RuntimeError('Copied companions failed verification')), \
             patch.object(dev, 'ROOT', root), patch.object(dev, 'node_environment', return_value=('/node', {})), \
             patch.object(dev.shutil, 'which', return_value='/go'), patch.object(dev.components, 'synchronize', return_value=bundle), \
             patch.dict(dev.os.environ, {'JPACK_DESK_JPACK': ''}), contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(RuntimeError, 'failed verification'):
                dev.prepare()
        staged = [path for path in (root / 'bin/dev-launches').iterdir()]
        self.assertEqual(len(staged), 2)
        self.assertTrue(all((path / dev.LAUNCH_MARKER).is_file() for path in staged))

    def test_starts_never_delete_old_launches(self):
        root, bundle, path_jpack = self.checkout()
        old = root / 'bin/dev-launches/old'; old.mkdir(parents=True)
        (old / dev.LAUNCH_MARKER).touch()
        override = old / 'jpack'; override.write_text('#!/bin/sh\n'); override.chmod(0o755)
        for environ in ({'JPACK_DESK_JPACK': ''}, {'JPACK_DESK_JPACK': str(override)}):
            self.prepare(root, bundle, path_jpack, environ)
        self.assertTrue(override.exists())
        self.assertEqual(len([path for path in old.parent.iterdir() if path.is_dir()]), 3)

    def test_a_start_or_restart_records_the_launch_it_used(self):
        for action in ('start', 'restart'):
            with self.subTest(action=action):
                (self.state / 'processes.json').unlink(missing_ok=True)  # state names one checkout
                root = Path(tempfile.mkdtemp(dir=self.state))
                used = root / 'bin/dev-launches/fresh'; used.mkdir(parents=True)
                commands = [('backend', [str(used / 'jpack-desk')], root, {}, 'unused')]
                with patch.object(dev, 'ROOT', root), patch.object(dev, 'prepare', return_value=commands), \
                     patch.object(dev, 'require_free_ports'), patch.object(dev, 'launch', return_value={}):
                    self.main(action)
                self.assertEqual((used.parent / dev.LAST_LAUNCH).read_text(), 'fresh\n')
                failed = [('backend', [str(root / 'bin/dev-launches/broken/jpack-desk')], root, {}, 'unused')]
                with patch.object(dev, 'ROOT', root), patch.object(dev, 'prepare', return_value=failed), \
                     patch.object(dev, 'require_free_ports'), patch.object(dev, 'launch', side_effect=RuntimeError('did not start')):
                    with self.assertRaises(RuntimeError):
                        self.main(action)
                self.assertEqual((used.parent / dev.LAST_LAUNCH).read_text(), 'fresh\n')

    def test_the_record_names_one_directory_or_nothing(self):
        launches = self.state / 'records'; launches.mkdir()
        descriptor = os.open(str(launches), os.O_RDONLY | os.O_DIRECTORY)
        self.addCleanup(os.close, descriptor)
        record = launches / dev.LAST_LAUNCH
        self.assertIsNone(dev.last_launch(descriptor))
        for content, expected in (('fresh\n', 'fresh'), ('', None), ('\n', None), ('.', None), ('..', None),
                                  ('../fresh', None), ('a/b', None), ('a\0b', None)):
            with self.subTest(content=content):
                record.write_text(content)
                self.assertEqual(dev.last_launch(descriptor), expected)
        record.unlink()
        (self.state / 'elsewhere').write_text('fresh\n')
        record.symlink_to(self.state / 'elsewhere')
        self.assertIsNone(dev.last_launch(descriptor))

    def tree(self, path):
        return sorted((str(item.relative_to(path)), item.is_symlink(), item.read_bytes() if item.is_file() and not item.is_symlink() else None)
                      for item in path.rglob('*'))

    def launches(self):
        root = Path(tempfile.mkdtemp(dir=self.state))
        launches = root / 'bin/dev-launches'
        made = {}
        for name in ('old', 'older', '.building-crashed', 'current'):
            made[name] = launches / name
            (made[name] / 'licenses').mkdir(parents=True)
            (made[name] / 'licenses/LICENSE').write_text('text')
            (made[name] / 'jpack').write_text('companion')
            (made[name] / dev.LAUNCH_MARKER).touch()
        made['foreign'] = launches / 'foreign'
        made['foreign'].mkdir()
        (made['foreign'] / dev.components.MANIFEST).write_text('{}')  # looks like one, not marked
        (launches / 'notes.txt').write_text('a file, not a launch')
        (launches / dev.LAST_LAUNCH).write_text('current\n')
        return root, launches, made

    def prune(self, root):
        out = io.StringIO()
        with patch.object(dev, 'ROOT', root), patch.object(sys, 'argv', ['desk-dev.py', 'prune']), contextlib.redirect_stdout(out):
            dev.main()
        return out.getvalue()

    def test_prune_removes_only_marked_launches_and_keeps_the_last_one_used(self):
        root, launches, made = self.launches()
        out = self.prune(root)
        self.assertEqual(sorted(path.name for path in launches.iterdir()), sorted([dev.LAST_LAUNCH, 'current', 'foreign', 'notes.txt']))
        self.assertTrue((made['current'] / 'licenses/LICENSE').exists())
        self.assertIn('Removed 3 old launch directories; kept current', out)
        self.assertIn('Removed 0 old launch directories; kept current', self.prune(root))

    def test_prune_refuses_while_any_tracked_server_runs_and_changes_nothing(self):
        for tracked in (('backend',), ('frontend',), ('backend', 'frontend'), ('another',)):
            with self.subTest(tracked=tracked):
                root, launches, made = self.launches()
                before = self.tree(launches)
                children = {name: self.spawn() for name in tracked}
                with patch.object(dev, 'ROOT', root):
                    dev.write_state({name: dev.identity(child.pid) for name, child in children.items()})
                with self.assertRaisesRegex(RuntimeError, 'running.*Nothing was removed'):
                    self.prune(root)
                self.assertEqual(self.tree(launches), before)
                for child in children.values():
                    child.kill(); child.wait()
                self.assertIn('Removed 3', self.prune(root))

    def test_prune_waits_for_the_lock_and_then_reads_the_state(self):
        root, launches, made = self.launches()
        before = self.tree(launches)
        script = (
            'import importlib.util, sys\n'
            'from pathlib import Path\n'
            'spec = importlib.util.spec_from_file_location("dev", sys.argv[1]); dev = importlib.util.module_from_spec(spec); spec.loader.exec_module(dev)\n'
            'dev.ROOT, dev.STATE = Path(sys.argv[2]), Path(sys.argv[3]); sys.argv = ["desk-dev.py", "prune"]\n'
            'try:\n    dev.main()\n'
            'except RuntimeError as error:\n    print(error); sys.exit(3)\n')
        with (self.state / 'lock').open('a') as lock:
            dev.fcntl.flock(lock, dev.fcntl.LOCK_EX)
            pruner = subprocess.Popen([sys.executable, '-c', script, str(Path(dev.__file__)), str(root), str(self.state)],
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            self.children.append(pruner)
            time.sleep(1)
            self.assertIsNone(pruner.poll())  # waiting for the lock
            self.assertEqual(self.tree(launches), before)
            # A start that holds the lock records its servers before releasing it.
            child = self.spawn()
            with patch.object(dev, 'ROOT', root):
                dev.write_state({'backend': dev.identity(child.pid)})
        out, err = pruner.communicate(timeout=30)
        self.assertEqual(pruner.returncode, 3, err)
        self.assertIn('Nothing was removed', out)
        self.assertEqual(self.tree(launches), before)

    def test_prune_ignores_a_malformed_or_linked_record(self):
        for record in ('../current', '', 'a/b', 'linked'):
            with self.subTest(record=record):
                root, launches, made = self.launches()
                (launches / dev.LAST_LAUNCH).unlink()
                if record == 'linked':
                    (self.state / 'record').write_text('current\n')
                    (launches / dev.LAST_LAUNCH).symlink_to(self.state / 'record')
                else:
                    (launches / dev.LAST_LAUNCH).write_text(record)
                self.prune(root)
                self.assertFalse(made['current'].exists())

    def test_prune_never_follows_a_link(self):
        root, launches, made = self.launches()
        outside = self.state / 'outside'; (outside / 'inner').mkdir(parents=True)
        (outside / dev.LAUNCH_MARKER).touch(); (outside / 'inner/keep').write_text('not ours')
        (launches / 'linked').symlink_to(outside)                       # a linked launch
        (made['old'] / 'escape').symlink_to(outside)                    # a link inside a launch
        unmarked = launches / 'link-marked'; unmarked.mkdir()
        (unmarked / dev.LAUNCH_MARKER).symlink_to(outside / dev.LAUNCH_MARKER)  # a linked marker
        self.prune(root)
        self.assertTrue((outside / 'inner/keep').exists() and (outside / dev.LAUNCH_MARKER).exists())
        self.assertTrue((launches / 'linked').is_symlink() and unmarked.exists())
        self.assertFalse(made['old'].exists())
        for linked in ('bin', 'bin/dev-launches'):
            with self.subTest(linked=linked):
                target = Path(tempfile.mkdtemp(dir=self.state))
                elsewhere = (target / 'dev-launches/valuable') if linked == 'bin' else (target / 'valuable')
                elsewhere.mkdir(parents=True)
                (elsewhere / dev.LAUNCH_MARKER).touch()
                other = Path(tempfile.mkdtemp(dir=self.state))
                if linked == 'bin':
                    (other / 'bin').symlink_to(target)
                else:
                    (other / 'bin').mkdir(); (other / 'bin/dev-launches').symlink_to(target)
                with self.assertRaisesRegex(RuntimeError, 'is a link'):
                    self.prune(other)
                self.assertTrue((elsewhere / dev.LAUNCH_MARKER).exists())

    def test_replacing_the_launch_root_midway_cannot_redirect_deletion(self):
        for swapped in ('bin', 'bin/dev-launches'):
            with self.subTest(swapped=swapped):
                root, launches, made = self.launches()
                outside = Path(tempfile.mkdtemp(dir=self.state))
                valuable = (outside / 'dev-launches/older') if swapped == 'bin' else (outside / 'older')
                valuable.mkdir(parents=True)
                (valuable / dev.LAUNCH_MARKER).touch(); (valuable / 'keep').write_text('not ours')
                original, done = dev.clear, []
                def swap_then_clear(descriptor):
                    if not done:
                        (root / swapped).rename(root / (swapped + '-moved'))
                        (root / swapped).symlink_to(outside)
                        done.append(True)
                    return original(descriptor)
                with patch.object(dev, 'clear', side_effect=swap_then_clear):
                    self.prune(root)
                self.assertEqual(done, [True])
                self.assertTrue((valuable / 'keep').exists())

    def test_status_names_the_launched_set_and_a_changed_lock(self):
        pins = {name: {'version': 'v1.0.0', 'revision': name[0] * 40} for name in ('runtime', 'runner', 'gateway')}
        changed = dict(pins, gateway={'version': 'v1.1.0', 'revision': 'f' * 40})
        directory = self.state / 'launch'; directory.mkdir()
        (directory / dev.components.MANIFEST).write_text(json.dumps({'identity': {'components': pins}}))
        with patch.object(dev.components.release.components, 'read_plan', return_value={'components': changed}):
            lines = dev.component_lines(directory, ['jpack-desk', '--jpack', str(directory / 'jpack')])
            override = dev.component_lines(directory, ['jpack-desk', '--jpack', '/elsewhere/jpack'])
        self.assertIn('runtime: v1.0.0 (locked)', lines)
        self.assertIn('runner: v1.0.0 (locked)', lines)
        self.assertIn('gateway: v1.0.0 (restart required: component lock changed)', lines)
        self.assertIn('runtime: explicit override (outside the component lock)', override)
        self.assertNotIn('runtime: v1.0.0 (locked)', override)

    def test_status_of_a_backend_without_a_launch_record_is_not_called_locked(self):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            dev.show_components(os.getpid())
        self.assertIn('not synchronized by this launcher', out.getvalue())
        self.assertNotIn('(locked)', out.getvalue())

    def test_tasks_use_structured_arguments_and_no_missing_settings(self):
        tasks = json.loads((dev.ROOT / '.vscode/tasks.json').read_text())['tasks']
        for action in ('start', 'stop', 'restart', 'prune'):
            task = next(task for task in tasks if task['label'] == f'desk: {action}')
            self.assertEqual(task['type'], 'process')
            self.assertEqual(task['args'], ['${workspaceFolder}/scripts/desk-dev.py', action])
        self.assertNotIn('${config:', json.dumps(tasks))


if __name__ == '__main__':
    unittest.main()
