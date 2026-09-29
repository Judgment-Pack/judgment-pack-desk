#!/usr/bin/env python3
"""Lifecycle safety checks without touching a developer's running Desk."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
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

    def test_tasks_use_structured_arguments_and_no_missing_settings(self):
        tasks = json.loads((dev.ROOT / '.vscode/tasks.json').read_text())['tasks']
        for action in ('start', 'stop', 'restart'):
            task = next(task for task in tasks if task['label'] == f'desk: {action}')
            self.assertEqual(task['type'], 'process')
            self.assertEqual(task['args'], ['${workspaceFolder}/scripts/desk-dev.py', action])
        self.assertNotIn('${config:', json.dumps(tasks))


if __name__ == '__main__':
    unittest.main()
