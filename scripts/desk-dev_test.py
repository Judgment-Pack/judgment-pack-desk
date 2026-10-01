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
            # An interrupted build is marked as the launcher's from the start.
            self.assertTrue((output.parent.parent / dev.LAUNCH_MARKER).is_file())
            output.write_text('desk binary')
        inherited = {'PATH': str(path_jpack.parent), 'JPACK_DESK_GATEWAY_MANIFEST_SHA256': 'a' * 64}
        which = lambda name: str(path_jpack) if name == 'jpack' else '/go'
        # Verification of the copy is dev-components' own test; here it copies.
        install = lambda source, destination: shutil.copytree(source, destination)
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

    def test_failed_desk_build_leaves_no_launch_and_the_cache_unchanged(self):
        root, bundle, path_jpack = self.checkout()
        def fail(args, **kwargs):
            Path(args[args.index('-o') + 1]).write_text('partial')
            raise subprocess.CalledProcessError(1, 'go')
        with self.assertRaises(subprocess.CalledProcessError):
            self.prepare(root, bundle, path_jpack, {'JPACK_DESK_JPACK': ''}, build=fail)
        self.assertEqual(list((root / 'bin/dev-launches').iterdir()), [])
        self.assertEqual(sorted(path.name for path in bundle.iterdir()), ['gateway-bundle.json', 'jpack', 'jpack-runner', 'jpack-source-worker'])
        self.assertEqual((bundle / 'jpack').read_text(), 'verified companion')

    def launches(self, root=None):
        launches = (root or self.state) / 'bin/dev-launches'
        made = {}
        for name in ('running', 'stopped', '.building-crashed'):
            made[name] = launches / name
            made[name].mkdir(parents=True)
            (made[name] / dev.LAUNCH_MARKER).touch()
        # Look like launches, but the launcher did not make them.
        for name in ('foreign', '.building-foreign'):
            made[name] = launches / name
            made[name].mkdir()
            (made[name] / dev.components.MANIFEST).write_text('{}')
        return launches, made

    def run_from(self, directory):
        """Start a real process from `directory`, and limit what pruning sees to
        it: the host's own process table is not this test's subject."""
        shutil.copy2(shutil.which('sleep'), directory / 'sleep')
        child = subprocess.Popen([str(directory / 'sleep'), '30'])
        self.children.append(child)
        proc = Path(tempfile.mkdtemp(dir=self.state)) / 'proc'
        proc.mkdir()
        self.entry = proc / str(child.pid)
        self.entry.symlink_to('/proc/' + str(child.pid))
        target = os.stat(directory / 'sleep')
        deadline = time.monotonic() + 5
        while (target.st_dev, target.st_ino) not in (dev.executables_in_use(proc) or ()) and time.monotonic() < deadline:
            time.sleep(0.02)
        scan = dev.executables_in_use
        self.scan = patch.object(dev, 'executables_in_use', side_effect=lambda **kwargs: scan(proc, **kwargs))
        self.scan.start()
        self.addCleanup(self.scan.stop)
        return child

    def test_prune_removes_only_marked_launches_no_process_executes_from(self):
        launches, made = self.launches()
        outside = self.state / 'outside'; outside.mkdir(); (outside / dev.LAUNCH_MARKER).touch()
        (launches / 'linked').symlink_to(outside)
        child = self.run_from(made['running'])
        dev.prune_launches(launches)
        self.assertTrue((made['running'] / 'sleep').exists())
        self.assertFalse(made['stopped'].exists())
        self.assertFalse(made['.building-crashed'].exists())
        self.assertTrue(made['foreign'].exists() and made['.building-foreign'].exists())
        self.assertTrue((outside / dev.LAUNCH_MARKER).exists() and (launches / 'linked').is_symlink())
        child.kill(); child.wait()
        self.entry.unlink()  # as the kernel's own entry goes once the child is reaped
        dev.prune_launches(launches)
        self.assertFalse(made['running'].exists())

    def test_a_launch_stays_in_use_after_it_is_renamed(self):
        launches, made = self.launches()
        child = self.run_from(made['running'])
        moved = launches / 'moved'
        made['running'].rename(moved)
        dev.prune_launches(launches)
        self.assertTrue((moved / 'sleep').exists())

    def test_prune_never_follows_a_linked_launch_root(self):
        for linked in ('bin', 'bin/dev-launches'):
            with self.subTest(linked=linked):
                target = Path(tempfile.mkdtemp(dir=self.state))
                elsewhere = target / 'dev-launches/valuable' if linked == 'bin' else target / 'valuable'
                elsewhere.mkdir(parents=True)
                (elsewhere / dev.LAUNCH_MARKER).touch()
                root = Path(tempfile.mkdtemp(dir=self.state))
                if linked == 'bin':
                    (root / 'bin').symlink_to(target)
                else:
                    (root / 'bin').mkdir(); (root / 'bin/dev-launches').symlink_to(target)
                with contextlib.redirect_stdout(io.StringIO()):
                    dev.prune_launches(root / 'bin/dev-launches')
                self.assertTrue((elsewhere / dev.LAUNCH_MARKER).exists())

    def test_replacing_the_launch_root_during_the_scan_cannot_redirect_deletion(self):
        for swapped in ('bin', 'bin/dev-launches'):
            with self.subTest(swapped=swapped):
                root = Path(tempfile.mkdtemp(dir=self.state))
                launches, made = self.launches(root)
                outside = Path(tempfile.mkdtemp(dir=self.state))
                valuable = (outside / 'dev-launches/stopped') if swapped == 'bin' else (outside / 'stopped')
                valuable.mkdir(parents=True)
                (valuable / dev.LAUNCH_MARKER).touch()
                def swap_then_scan(**kwargs):
                    (root / swapped).rename(root / (swapped + '-moved'))
                    (root / swapped).symlink_to(outside)
                    return set()
                with patch.object(dev, 'executables_in_use', side_effect=swap_then_scan):
                    dev.prune_launches(launches)
                self.assertTrue((valuable / dev.LAUNCH_MARKER).exists())

    def test_replacing_a_launch_after_it_is_checked_cannot_redirect_deletion(self):
        launches, made = self.launches()
        replacement = self.state / 'replacement'; replacement.mkdir()
        (replacement / 'valuable').write_text('must survive')
        original, checked = dev.file_ids, os.stat(made['stopped']).st_ino
        swapped = []
        def swap_then_scan(descriptor):
            # Once the marked launch has been opened and checked, put an
            # unmarked directory with something valuable at its name.
            if not swapped and os.fstat(descriptor).st_ino == checked:
                made['stopped'].rename(launches.parent / 'retired')
                replacement.rename(made['stopped'])
                swapped.append(True)
            return original(descriptor)
        with patch.object(dev, 'file_ids', side_effect=swap_then_scan):
            dev.prune_launches(launches)
        self.assertEqual(swapped, [True])
        self.assertEqual((made['stopped'] / 'valuable').read_text(), 'must survive')
        self.assertEqual(list((launches.parent / 'retired').iterdir()), [])

    def test_an_explicit_runtime_inside_an_old_launch_keeps_that_launch(self):
        launches, made = self.launches()
        runtime = made['stopped'] / 'jpack'; runtime.write_text('#!/bin/sh\n')
        dev.prune_launches(launches, keep=[runtime])
        self.assertTrue(runtime.exists())
        self.assertFalse(made['.building-crashed'].exists())
        dev.prune_launches(launches)
        self.assertFalse(made['stopped'].exists())

    def test_prepare_keeps_an_override_that_lives_in_an_old_launch(self):
        root, bundle, path_jpack = self.checkout()
        override = root / 'bin/dev-launches/previous/jpack'
        override.parent.mkdir(parents=True)
        override.write_text('#!/bin/sh\n'); override.chmod(0o755)
        (override.parent / dev.LAUNCH_MARKER).touch()
        commands, _ = self.prepare(root, bundle, path_jpack, {'JPACK_DESK_JPACK': str(override)})
        selected = Path(commands[0][1][commands[0][1].index('--jpack') + 1])
        self.assertEqual(selected, override.resolve())
        self.assertTrue(selected.exists())

    def test_prune_removes_nothing_when_a_process_cannot_be_inspected(self):
        launches, made = self.launches()
        with patch.object(dev, 'executables_in_use', return_value=None), contextlib.redirect_stdout(io.StringIO()) as out:
            dev.prune_launches(launches)
        self.assertIn('Older launch directories were kept', out.getvalue())
        self.assertTrue(all(directory.exists() for directory in made.values()))

    def test_the_host_scan_finds_a_real_process_or_names_what_blocks_it(self):
        directory = self.state / 'host'; directory.mkdir()
        shutil.copy2(shutil.which('sleep'), directory / 'sleep')
        child = subprocess.Popen([str(directory / 'sleep'), '30'])
        self.children.append(child)
        target = os.stat(directory / 'sleep')
        blocked = []
        found = dev.executables_in_use(blocked=blocked)
        if found is None:
            reason = dev.blocking(blocked)
            print('host process scan inconclusive: ' + reason, file=sys.stderr)
            self.assertIn('could not be inspected', reason) if blocked else None
            self.skipTest(reason)
        deadline = time.monotonic() + 5
        while (target.st_dev, target.st_ino) not in found and time.monotonic() < deadline:
            time.sleep(0.02)
            found = dev.executables_in_use() or found
        self.assertIn((target.st_dev, target.st_ino), found)

    def test_a_kept_launch_says_which_process_blocked_pruning(self):
        launches, made = self.launches()
        entry = self.state / 'proc/4242'; entry.mkdir(parents=True)
        (entry / 'comm').write_text('keyring-daemon\n')
        def blocked(blocked=None, **kwargs):
            blocked.append(entry)
            return None
        out = io.StringIO()
        with patch.object(dev, 'executables_in_use', side_effect=blocked), contextlib.redirect_stdout(out):
            dev.prune_launches(launches)
        self.assertIn('process 4242 (keyring-daemon) could not be inspected', out.getvalue())
        self.assertTrue(made['stopped'].exists())

    def test_a_process_vanishing_mid_scan_forces_another_pass(self):
        proc = self.state / 'proc'
        live = proc / '10'; (live / 'task/10').mkdir(parents=True)
        (live / 'stat').write_text('10 (x) S 1 1')
        executable = self.state / 'executable'; executable.write_text('')
        (live / 'exe').symlink_to(executable)
        info = os.stat(executable)
        gone = proc / '20'  # listed, then gone before it is inspected
        passes = []
        class Proc:
            def __init__(self, vanishing): self.vanishing = vanishing
            def iterdir(self):
                passes.append(1)
                return iter([live] + ([gone] if len(passes) <= self.vanishing else []))
        self.assertEqual(dev.executables_in_use(Proc(1)), {(info.st_dev, info.st_ino)})
        self.assertEqual(len(passes), 2)
        passes.clear()
        self.assertIsNone(dev.executables_in_use(Proc(99), attempts=3))
        self.assertEqual(len(passes), 3)

    def test_an_inconclusive_inspection_of_this_users_process_stops_pruning(self):
        if os.geteuid() == 0:
            self.skipTest('permissions do not bind root')
        for unreadable in ('stat', 'task'):
            with self.subTest(unreadable=unreadable):
                proc = Path(tempfile.mkdtemp(dir=self.state)) / 'proc'
                entry = proc / '30'; (entry / 'task/30').mkdir(parents=True)
                (entry / 'stat').write_text('30 (x) Z 1 1')
                (entry / unreadable).chmod(0)
                self.addCleanup((entry / unreadable).chmod, 0o700)
                self.assertIsNone(dev.executables_in_use(proc))

    def test_only_a_plain_zombie_or_another_user_may_hide_its_executable(self):
        proc = self.state / 'proc'
        def process(pid, state, threads, executable=None):
            entry = proc / str(pid)
            (entry / 'task').mkdir(parents=True)
            for thread in range(threads):
                (entry / 'task' / str(pid + thread)).mkdir()
            (entry / 'stat').write_text(f'{pid} (name with) spaces) {state} 1 1')
            if executable:
                (entry / 'exe').symlink_to(executable)
            return entry
        running = self.state / 'running-executable'; running.write_text('')
        process(10, 'S', 1, running)
        process(11, 'Z', 1)
        (proc / 'self').mkdir(parents=True)
        info = os.stat(running)
        self.assertEqual(dev.executables_in_use(proc), {(info.st_dev, info.st_ino)})
        for pid, state, threads in ((12, 'Z', 2), (13, 'S', 1)):
            with self.subTest(pid=pid):
                entry = process(pid, state, threads, self.state / 'missing')
                self.assertIsNone(dev.executables_in_use(proc))
                self.assertEqual(dev.executables_in_use(proc, uid=os.getuid() + 1), {(info.st_dev, info.st_ino)})
                shutil.rmtree(entry)

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
        for action in ('start', 'stop', 'restart'):
            task = next(task for task in tasks if task['label'] == f'desk: {action}')
            self.assertEqual(task['type'], 'process')
            self.assertEqual(task['args'], ['${workspaceFolder}/scripts/desk-dev.py', action])
        self.assertNotIn('${config:', json.dumps(tasks))


if __name__ == '__main__':
    unittest.main()
