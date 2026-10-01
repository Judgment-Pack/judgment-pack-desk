#!/usr/bin/env python3
"""Start, stop, or restart the local Desk + Vite development servers (Linux/WSL)."""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
KEY = hashlib.sha256(str(ROOT).encode()).hexdigest()[:12]
STATE = Path(tempfile.gettempdir()) / f'jpack-desk-dev-{os.getuid()}-{KEY}'
BOOT = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
URL = 'http://localhost:5173/'
spec = importlib.util.spec_from_file_location('dev_components', ROOT / 'scripts/dev-components.py')
components = importlib.util.module_from_spec(spec)
spec.loader.exec_module(components)


def identity(pid):
    """PID plus kernel birth time and boot ID; a reused PID is not ours."""
    try:
        fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
        if fields[0] == 'Z':
            return None
        return {'pid': pid, 'start': fields[19], 'boot': BOOT, 'group': int(fields[2])}
    except (OSError, IndexError, ValueError):
        return None


def alive(record):
    return bool(record) and identity(record['pid']) == record


def read_state():
    path = STATE / 'processes.json'
    if not path.exists():
        return {}
    data = json.loads(path.read_text())
    if data.get('root') != str(ROOT) or data.get('version') != 1:
        raise RuntimeError(f'Unrecognized process state: {path}; no processes were stopped.')
    return data['processes']


def write_state(processes):
    path = STATE / 'processes.tmp'
    path.write_text(json.dumps({'version': 1, 'root': str(ROOT), 'processes': processes}))
    path.replace(STATE / 'processes.json')


def stop(processes):
    # Snapshot group members before signalling; children may outlive their leader.
    # Escalation addresses only those exact processes, never a port or a name.
    members = {}
    for record in processes.values():
        if not alive(record) or record['group'] != record['pid']:
            continue
        for path in Path('/proc').iterdir():
            if path.name.isdigit():
                member = identity(int(path.name))
                if member and member['group'] == record['pid']:
                    members[member['pid']] = member
        if alive(record):
            try:
                os.killpg(record['pid'], signal.SIGTERM)
            except ProcessLookupError:
                pass
    deadline = time.monotonic() + 10
    while any(alive(member) for member in members.values()) and time.monotonic() < deadline:
        time.sleep(0.1)
    for member in members.values():
        if alive(member):
            try:
                os.kill(member['pid'], signal.SIGKILL)
            except ProcessLookupError:
                pass
    deadline = time.monotonic() + 3
    while any(alive(member) for member in members.values()) and time.monotonic() < deadline:
        time.sleep(0.05)
    if any(alive(member) for member in members.values()):
        raise RuntimeError('A tracked server could not be stopped; process state was retained.')
    write_state({})


def require_free_ports():
    # Vite can bind IPv6 localhost, so check both loopback families.
    for port in (8790, 5173):
        for family, host in ((socket.AF_INET, '127.0.0.1'), (socket.AF_INET6, '::1')):
            try:
                with socket.socket(family) as listener:
                    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                    listener.bind((host, port))
            except OSError as error:
                if error.errno in (97, 99):  # IPv6 unavailable on this host.
                    continue
                raise RuntimeError(f'Port {port} is already in use. Stop its existing server first; this task will not kill an untracked process.') from error


def node_environment():
    env = os.environ.copy()
    node = shutil.which('node')
    if node:
        version = subprocess.check_output([node, '--version'], text=True).strip()
        if int(version.lstrip('v').split('.')[0]) >= 22:
            return node, env
    # VS Code may not inherit an interactive shell's nvm selection.
    nvm = Path(os.environ.get('NVM_DIR', str(Path.home() / '.nvm')))
    candidates = []
    for node in (nvm / 'versions/node').glob('v*/bin/node'):
        try:
            version = tuple(int(part) for part in node.parents[1].name.lstrip('v').split('.'))
            if version[0] >= 22:
                candidates.append((version[0] == 22, version, node))
        except ValueError:
            continue
    if not candidates:
        raise RuntimeError('Node 22 or newer is required. Run nvm install, then npm --prefix web ci.')
    node = max(candidates)[2]
    env['PATH'] = str(node.parent) + os.pathsep + env.get('PATH', '')
    return str(node), env


# Written into every launch directory this launcher creates; pruning removes
# nothing without it.
LAUNCH_MARKER = '.desk-dev-launch'
DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


def scan_executables(proc, uid, blocked):
    """One pass over `proc`: (found, settled).

    Not settled when a listed process disappeared before it was inspected (it
    may have handed off to a child this pass did not list), or when a live
    process of this user could not be inspected; that entry is added to
    `blocked`. A process in the middle of exiting looks like the latter for a
    moment, so the caller tries again.
    """
    found, settled = set(), True
    for entry in [entry for entry in proc.iterdir() if entry.name.isdigit()]:
        try:
            executable = os.stat(entry / 'exe')
            found.add((executable.st_dev, executable.st_ino))
            continue
        except PermissionError:
            # The kernel refuses another user's process, or one that is not
            # dumpable (systemd's "(sd-pam)", for one). Executing an ordinary
            # readable file, as every launch file is, makes a process dumpable,
            # and no companion gives that up, so this one runs no launch file.
            continue
        except OSError:
            pass  # gone, a zombie, a kernel thread, exiting, or a dead leader
        try:
            owner = entry.stat().st_uid
        except FileNotFoundError:
            settled = False
            continue
        except OSError:
            blocked.append(entry)
            settled = False
            continue
        if owner != uid:
            continue
        try:
            state = (entry / 'stat').read_text().rsplit(')', 1)[1].split()[0]
            threads = len(os.listdir(entry / 'task'))
        except FileNotFoundError:
            settled = False
            continue
        except (OSError, IndexError):
            blocked.append(entry)
            settled = False
            continue
        if state != 'Z' or threads > 1:
            blocked.append(entry)
            settled = False
    return found, settled


def executables_in_use(proc=Path('/proc'), uid=None, attempts=5, blocked=None):
    """(device, inode) of every running process's executable, or None when the
    scan does not settle.

    Identity by inode survives renames and links. A process of this user whose
    executable cannot be read keeps the scan unsettled unless it is a plain
    zombie: a leader that exited while other threads run is still live. After
    `attempts` unsettled passes, a short pause apart, the answer is None, and
    `blocked` names what could not be inspected in the last pass.
    """
    uid = os.getuid() if uid is None else uid
    blocked = [] if blocked is None else blocked
    for attempt in range(attempts):
        if attempt:
            time.sleep(0.05)
        del blocked[:]
        found, settled = scan_executables(proc, uid, blocked)
        if settled:
            return found
    return None


def blocking(blocked):
    """Names the process that stopped pruning, from its world-readable name."""
    if not blocked:
        return 'running processes kept changing during the check'
    try:
        name = (blocked[-1] / 'comm').read_text().strip()
    except OSError:
        name = 'unknown'
    return 'process ' + blocked[-1].name + ' (' + name + ') could not be inspected'


def file_ids(descriptor):
    """(device, inode) of every regular file below an open directory, never following a link."""
    ids = set()
    for entry in os.listdir(descriptor):
        info = os.stat(entry, dir_fd=descriptor, follow_symlinks=False)
        if stat.S_ISDIR(info.st_mode):
            child = os.open(entry, DIRECTORY, dir_fd=descriptor)
            try:
                ids |= file_ids(child)
            finally:
                os.close(child)
        elif stat.S_ISREG(info.st_mode):
            ids.add((info.st_dev, info.st_ino))
    return ids


def clear(descriptor):
    """Empty an open directory, never following a link out of it."""
    for entry in os.listdir(descriptor):
        if stat.S_ISDIR(os.stat(entry, dir_fd=descriptor, follow_symlinks=False).st_mode):
            child = os.open(entry, DIRECTORY, dir_fd=descriptor)
            try:
                clear(child)
            finally:
                os.close(child)
            os.rmdir(entry, dir_fd=descriptor)
        else:
            os.unlink(entry, dir_fd=descriptor)


def prune_launches(launches, keep=()):
    """Remove launch directories this launcher made that no process executes from.

    Every start gets a new launch directory, so old ones would accumulate a
    copy of the companions per restart. `bin` and `bin/dev-launches` are opened
    without following links before anything is inspected, and each candidate
    is opened once: its marker, its files and its removal all go through that
    one descriptor, so replacing any of these paths cannot redirect a deletion.
    Only directories carrying the launcher's marker are removed, never one
    holding a running executable or a file named in `keep`, and nothing when a
    live process of this user cannot be inspected. A process started by hand
    from an old launch directory while a start is running is outside this.
    """
    try:
        parent = os.open(str(launches.parent), DIRECTORY)
    except OSError:
        print('Launch directories are not pruned: bin is a link or unreadable.', flush=True)
        return
    try:
        try:
            root = os.open(launches.name, DIRECTORY, dir_fd=parent)
        except OSError:
            print('Launch directories are not pruned: bin/dev-launches is a link or unreadable.', flush=True)
            return
        try:
            blocked = []
            in_use = executables_in_use(blocked=blocked)
            if in_use is None:
                print('Older launch directories were kept: ' + blocking(blocked) + '. Remove unused ones under bin/dev-launches while Desk is stopped.', flush=True)
                return
            for path in keep:
                try:
                    info = os.stat(path)
                    in_use.add((info.st_dev, info.st_ino))
                except OSError:
                    pass
            for name in os.listdir(root):
                try:
                    candidate = os.open(name, DIRECTORY, dir_fd=root)
                except OSError:
                    continue  # a link or not a directory: keep it
                try:
                    if not stat.S_ISREG(os.stat(LAUNCH_MARKER, dir_fd=candidate, follow_symlinks=False).st_mode):
                        continue
                    if file_ids(candidate) & in_use:
                        continue
                    clear(candidate)
                except OSError:
                    continue  # changed meanwhile: keep what is left
                finally:
                    os.close(candidate)
                try:
                    os.rmdir(name, dir_fd=root)  # only ever removes an empty directory
                except OSError:
                    pass
        finally:
            os.close(root)
    finally:
        os.close(parent)


def prepare():
    node, env = node_environment()
    vite = ROOT / 'web/node_modules/vite/bin/vite.js'
    if not vite.is_file():
        raise RuntimeError('Install frontend dependencies first: npm --prefix web ci')
    go = shutil.which('go')
    if not go:
        raise RuntimeError('Go must be on PATH to build Desk.')
    runtime = os.environ.get('JPACK_DESK_JPACK')
    if runtime:
        runtime = str(Path(runtime).expanduser().resolve())
        if not Path(runtime).is_file() or not os.access(runtime, os.X_OK):
            raise RuntimeError('JPACK_DESK_JPACK must name an executable file.')
        print('Runtime override enabled; this launch may differ from the tested component lock.', flush=True)
    bundle = components.synchronize()
    print('Building Desk…', flush=True)
    launches = ROOT / 'bin/dev-launches'
    launches.mkdir(parents=True, exist_ok=True)
    # An explicit Runtime override inside an older launch keeps that launch.
    prune_launches(launches, keep=[runtime] if runtime else [])
    with tempfile.TemporaryDirectory(prefix='.building-', dir=launches) as temp:
        # Marked first, so a build interrupted here is still recognisably ours.
        (Path(temp) / LAUNCH_MARKER).touch()
        staged = Path(temp) / 'bundle'
        # Companions stay beside Desk, as a verified copy that shares no file
        # with the cache.
        components.install(bundle, staged)
        (staged / LAUNCH_MARKER).touch()
        subprocess.run([go, 'build', '-trimpath', '-o', str(staged / 'jpack-desk'), '.'], cwd=ROOT, check=True)
        installed = launches / Path(temp).name[len('.building-'):]
        staged.rename(installed)
    runtime = runtime or str(installed / 'jpack')
    backend = [str(installed / 'jpack-desk'), '--dev-token', 'dev', '--port', '8790',
               '--jpack', runtime, '--runner', str(installed / 'jpack-runner')]
    project = os.environ.get('JPACK_DESK_PROJECT')
    if project:
        backend.append(str(Path(project).expanduser().resolve()))
    # Omitting a project uses Desk's saved default; no user configuration is rewritten.
    backend_env = dict(env, JPACK_DESK_LOCAL_ACCESS='1')
    backend_env.pop('JPACK_DESK_GATEWAY_MANIFEST_SHA256', None)
    frontend_env = dict(env, JPACK_DESK_CHASSIS='http://127.0.0.1:8790')
    return [
        ('backend', backend, ROOT, backend_env, 'http://127.0.0.1:8790/'),
        ('frontend', [node, str(vite), '--host', '127.0.0.1', '--port', '5173', '--strictPort'], ROOT / 'web', frontend_env, 'http://127.0.0.1:5173/'),
    ]


def ready(url):
    try:
        # Never send localhost readiness probes through a configured HTTP proxy.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(url, timeout=0.5) as response:
            return response.status == 200
    except (OSError, urllib.error.URLError):
        return False


def launch(commands):
    processes = {}
    children = []
    try:
        for name, command, cwd, env, url in commands:
            with (STATE / f'{name}.log').open('wb') as log:
                child = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                         stdout=log, stderr=log, start_new_session=True)
            children.append(child)
            record = identity(child.pid)
            if not record:
                raise RuntimeError(f'{name} exited during startup. See {STATE / (name + ".log")}')
            processes[name] = record
            write_state(processes)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            if any(child.poll() is not None for child in children):
                raise RuntimeError(f'A development server exited. See logs in {STATE}')
            if all(ready(command[4]) for command in commands):
                return processes
            time.sleep(0.2)
        raise RuntimeError(f'Desk did not become ready within 45 seconds. See logs in {STATE}')
    except BaseException:
        stop(processes)
        for child in children:
            child.wait(timeout=5)
        raise


def show(processes):
    for name in ('backend', 'frontend'):
        record = processes.get(name)
        print(f'{name}: ' + (f'running (PID {record["pid"]})' if alive(record) else 'stopped'))
    if all(alive(processes.get(name)) for name in ('backend', 'frontend')):
        print(f'Desk: {URL}')
        show_components(processes['backend']['pid'])
    print(f'Logs: {STATE}')


def component_lines(directory, arguments):
    """The companion set a backend was launched with, against the current lock."""
    record = json.loads((directory / components.MANIFEST).read_text())
    expected = components.release.components.read_plan()['components']
    runtime = arguments[arguments.index('--jpack') + 1] if '--jpack' in arguments else ''
    lines = []
    for name, item in record['identity']['components'].items():
        if name == 'runtime' and runtime != str(directory / 'jpack'):
            lines.append('runtime: explicit override (outside the component lock)')
        else:
            lines.append(name + ': ' + item['version'] + (' (locked)' if item == expected.get(name) else ' (restart required: component lock changed)'))
    return lines + ['Companion directory: ' + str(directory)]


def show_components(pid):
    try:
        directory = Path(os.readlink(f'/proc/{pid}/exe')).parent
        arguments = Path(f'/proc/{pid}/cmdline').read_bytes().decode().split('\0')
        print('\n'.join(component_lines(directory, arguments)))
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        print('Companions: not synchronized by this launcher; restart to apply the component lock.')


def open_browser():
    opener = shutil.which('wslview') or shutil.which('xdg-open')
    if opener:
        subprocess.Popen([opener, URL], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                         stderr=subprocess.DEVNULL, start_new_session=True)
    else:
        print(f'Open {URL} in your browser.')


def interrupted(_signum, _frame):
    raise KeyboardInterrupt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('start', 'stop', 'restart', 'status'))
    parser.add_argument('--open', action='store_true', help='Open the browser after starting.')
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, interrupted)
    STATE.mkdir(mode=0o700, exist_ok=True)
    if STATE.is_symlink() or STATE.stat().st_uid != os.getuid():
        raise RuntimeError(f'Unsafe process state directory: {STATE}')
    STATE.chmod(0o700)
    # VS Code tasks and terminal invocations share a lock; repeated clicks cannot
    # create duplicate servers or race a restart against a stop.
    with (STATE / 'lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        processes = read_state()
        if args.action == 'status':
            show(processes)
            return
        if args.action == 'stop':
            stop(processes)
            print('Desk development servers stopped. Saved workspace files are unchanged.')
            return
        running = all(alive(processes.get(name)) for name in ('backend', 'frontend'))
        if args.action == 'start' and running:
            print('Desk is already running.')
        else:
            # Compile before stopping a working session: a build failure leaves it up.
            commands = prepare()
            stop(processes)
            require_free_ports()
            processes = launch(commands)
        show(processes)
        if args.open:
            open_browser()


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
        print(f'Desk: {error}', file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print('Desk task cancelled.', file=sys.stderr)
        sys.exit(130)
