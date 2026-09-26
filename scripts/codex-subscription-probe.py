#!/usr/bin/env python3
"""Probe Codex 0.145.0 with a scripted loopback model and an empty profile.

No real account, login, provider key, or subscription inference is used. Each
scenario starts a new process. Exit 0 means the local protocol checks passed;
it does not certify real authentication, entitlement, or a production engine.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

VERSION = 'codex-cli 0.145.0'
SCENARIOS = ('host-tool', 'private-image', 'write', 'shell', 'web', 'subagent',
             'skills-list', 'skills-read')
# Native non-executing utilities remain registered in this exact release.
# Each skills handler can only address the disabled orchestrator catalog.
UTILITY_TOOLS = {'update_plan', 'skills.list', 'skills.read'}
DISABLED_FEATURES = ('shell_tool', 'unified_exec', 'shell_snapshot', 'multi_agent',
    'multi_agent_v2', 'apps', 'hooks', 'plugins', 'remote_plugin', 'plugin_sharing',
    'memories', 'goals', 'browser_use', 'browser_use_external',
    'browser_use_full_cdp_access', 'computer_use', 'in_app_browser',
    'skill_mcp_dependency_install', 'skill_search', 'workspace_dependencies',
    'auth_elicitation', 'tool_suggest', 'image_generation', 'code_mode',
    'code_mode_host', 'enable_request_compression')


class Probe:
    def __init__(self, binary, model, scenario, environment=False, sandbox_bin=None):
        self.binary, self.model, self.scenario = binary, model, scenario
        self.environment = environment
        self.sandbox_bin = sandbox_bin
        self.requests = []
        self.messages = queue.Queue()
        self.next_id = 0
        self.server_requests = []

    def tool_item(self):
        if self.scenario == 'write':
            return {'type': 'custom_tool_call', 'id': 'fc_probe', 'call_id': 'call_probe',
                'name': 'apply_patch', 'input': '*** Begin Patch\n*** Add File: ' +
                str(self.canary) + '\n+UNAUTHORIZED_WRITE\n*** End Patch'}
        name, args = {
            'host-tool': ('jps_probe', {'input': 'probe'}),
            'private-image': ('view_image', {'path': str(self.private_image)}),
            'shell': ('exec_command', {'cmd': 'touch ' + str(self.canary)}),
            'web': ('web.run', {'search_query': [{'q': 'JPS_SYNTHETIC_PROBE'}]}),
            'subagent': ('spawn_agent', {'message': 'JPS_SYNTHETIC_PROBE'}),
            'skills-list': ('list', {'authority': {'kind': 'orchestrator'}}),
            'skills-read': ('read', {'authority': {'kind': 'orchestrator'},
                'package': str(self.private_image), 'resource': str(self.private_image)})
        }[self.scenario]
        item = {'type': 'function_call', 'id': 'fc_probe', 'call_id': 'call_probe',
                'name': name, 'arguments': json.dumps(args)}
        if self.scenario.startswith('skills-'):
            item['namespace'] = 'skills'
        return item

    def serve(self):
        probe = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                if self.path != '/v1/responses' or self.headers.get('Content-Encoding'):
                    self.send_error(400)
                    return
                size = int(self.headers.get('Content-Length', '0'))
                if not 0 < size <= 4 * 1024 * 1024:
                    self.send_error(413)
                    return
                body = json.loads(self.rfile.read(size))
                probe.requests.append(body)
                sequence = len(probe.requests)
                self.send_response(200)
                self.send_header('Content-Type', 'text/event-stream')
                self.end_headers()
                item = probe.tool_item() if sequence == 1 else {
                    'type': 'message', 'id': 'msg_probe', 'role': 'assistant',
                    'status': 'completed', 'content': [
                        {'type': 'output_text', 'text': 'Probe completed.', 'annotations': []}]}
                response = {'id': 'resp_' + str(sequence), 'object': 'response', 'status': 'completed',
                    'output': [item], 'usage': {'input_tokens': 1, 'output_tokens': 1, 'total_tokens': 2}}
                events = [
                    {'type': 'response.created', 'response': {'id': response['id'], 'status': 'in_progress'}},
                    {'type': 'response.output_item.added', 'output_index': 0, 'item': item},
                    {'type': 'response.output_item.done', 'output_index': 0, 'item': item},
                    {'type': 'response.completed', 'response': response}]
                for event in events:
                    self.wfile.write(('event: ' + event['type'] + '\ndata: ' + json.dumps(event) + '\n\n').encode())
                self.wfile.flush()

        self.http = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        threading.Thread(target=self.http.serve_forever, daemon=True).start()

    def write(self, value):
        self.process.stdin.write(json.dumps(value) + '\n')
        self.process.stdin.flush()

    def read(self, deadline):
        value = self.messages.get(timeout=max(0.01, deadline - time.monotonic()))
        if value is None:
            raise RuntimeError('Codex exited before completing the protocol')
        if isinstance(value, Exception):
            raise value
        return value

    def rpc(self, method, params):
        self.next_id += 1
        identifier = self.next_id
        self.write({'id': identifier, 'method': method, 'params': params})
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            value = self.read(deadline)
            if value.get('id') == identifier and 'method' not in value:
                if 'error' in value:
                    raise RuntimeError('RPC error: ' + json.dumps(value['error']))
                return value['result']
            if 'method' in value and 'id' in value:
                raise RuntimeError('Unexpected server request during ' + method)
        raise RuntimeError('RPC deadline exceeded')

    def config(self, work, profile):
        return '''model = %s
model_provider = "jps_probe"
approval_policy = "never"
forced_login_method = "chatgpt"
cli_auth_credentials_store = "file"
web_search = "disabled"
project_doc_max_bytes = 0
default_permissions = "jps"
[permissions.jps.filesystem]
":minimal" = "read"
%s = "read"
%s = "deny"
[permissions.jps.network]
enabled = false
[model_providers.jps_probe]
name = "JPS loopback fixture"
base_url = "http://127.0.0.1:%s/v1"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
[skills]
include_instructions = false
[skills.bundled]
enabled = false
[analytics]
enabled = false
[history]
persistence = "none"
[tools.experimental_request_user_input]
enabled = false
[features]
%s
''' % (json.dumps(self.model), json.dumps(str(work)), json.dumps(str(profile)),
        self.http.server_port, '\n'.join(k + ' = false' for k in DISABLED_FEATURES))

    def run(self):
        summary = {'scenario': self.scenario, 'environmentEnabled': self.environment}
        errors = []
        self.serve()
        try:
            with tempfile.TemporaryDirectory(prefix='jps-codex-proof-') as scratch:
                base = Path(scratch)
                home, work = base/'home', base/'work'
                profile = home/'.codex'
                for p in (home, profile, work):
                    p.mkdir(mode=0o700, parents=True, exist_ok=True)
                self.private_image = profile/'secret-fixture.png'
                self.private_image.write_bytes(base64.b64decode(
                    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1N8AAAAASUVORK5CYII='))
                self.canary = work/'patch-canary.txt'
                config = profile/'config.toml'
                config.write_text(self.config(work, profile))
                config.chmod(0o600)
                # Only the child gets these documented settings. Never inherit the
                # user's credentials, proxy settings, terminal profile or project.
                env = {'PATH': '/usr/bin:/bin', 'HOME': str(home), 'CODEX_HOME': str(profile),
                       'LANG': 'C.UTF-8', 'TMPDIR': str(base), 'RUST_LOG': 'off'}
                if self.sandbox_bin:
                    env['PATH'] = str(self.sandbox_bin.parent) + ':' + env['PATH']
                version = subprocess.run([self.binary, '--version'], env=env, cwd=str(work),
                    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, universal_newlines=True,
                    timeout=5, check=True).stdout.strip()
                summary['version'] = version
                if version != VERSION:
                    raise RuntimeError('This probe requires ' + VERSION)
                # Stop discovery at this private root, even if /tmp/.codex exists.
                subprocess.run(['/usr/bin/git', 'init', '-q', str(work)], env=env, check=True,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
                self.process = subprocess.Popen([self.binary, 'app-server', '--stdio', '--strict-config'],
                    cwd=str(work), env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE, universal_newlines=True, bufsize=1, start_new_session=True)
                def consume():
                    try:
                        for line in self.process.stdout:
                            if len(line) > 4 * 1024 * 1024:
                                raise RuntimeError('Protocol frame too large')
                            self.messages.put(json.loads(line))
                    except Exception as error:
                        self.messages.put(error)
                    finally:
                        self.messages.put(None)
                def stderr():
                    for line in self.process.stderr:
                        if len(errors) < 8:
                            errors.append(line.strip())
                threading.Thread(target=consume, daemon=True).start()
                threading.Thread(target=stderr, daemon=True).start()
                try:
                    hello = self.rpc('initialize', {'clientInfo': {'name': 'jps_isolation_probe', 'version': '0.1.0'},
                        'capabilities': {'experimentalApi': True}})
                    summary['userAgent'] = hello.get('userAgent', '')
                    self.write({'method': 'initialized'})
                    account = self.rpc('account/read', {'refreshToken': False})
                    summary['accountAbsent'] = account.get('account') is None
                    effective = self.rpc('config/read', {'includeLayers': False})['config']
                    summary['noMcpServers'] = not effective.get('mcp_servers')
                    started = self.rpc('thread/start', {'model': self.model,
                        'modelProvider': 'jps_probe', 'allowProviderModelFallback': False,
                        'cwd': str(work), 'approvalPolicy': 'never', 'permissions': 'jps',
                        'environments': None if self.environment else [],
                        'selectedCapabilityRoots': [], 'ephemeral': True,
                        'experimentalRawEvents': False, 'dynamicTools': [{'type': 'function',
                            'name': 'jps_probe', 'description': 'A harmless Desk tool fixture.',
                            'inputSchema': {'type': 'object', 'properties': {'input': {'type': 'string'}},
                                'required': ['input'], 'additionalProperties': False}}]})
                    self.rpc('turn/start', {'threadId': started['thread']['id'], 'permissions': 'jps',
                        'environments': None if self.environment else [],
                        'input': [{'type': 'text', 'text': 'Run the probe, then report completion.'}]})
                    deadline = time.monotonic() + 20
                    called, completed = False, False
                    while time.monotonic() < deadline:
                        message = self.read(deadline)
                        if 'id' in message and 'method' in message:
                            self.server_requests.append(message['method'])
                            if message['method'] == 'item/tool/call':
                                params = message['params']
                                if params.get('tool') != 'jps_probe' or params.get('arguments') != {'input': 'probe'}:
                                    raise RuntimeError('Unexpected host tool arguments')
                                called = True
                                self.write({'id': message['id'], 'result': {'success': True,
                                    'contentItems': [{'type': 'inputText', 'text': 'JPS_FIXTURE_OK'}]}})
                            else:
                                self.write({'id': message['id'], 'error': {'code': -32601, 'message': 'Not permitted'}})
                        elif message.get('method') == 'turn/completed':
                            completed = message['params']['turn']['status'] == 'completed'
                            break
                    tools = set()
                    for request in self.requests:
                        for tool in request.get('tools', []):
                            if tool['type'] == 'namespace':
                                tools.update(tool['name'] + '.' + t['name'] for t in tool['tools'])
                            else:
                                tools.add(tool.get('name', tool['type']))
                    results = [x.get('output', '') for request in self.requests[1:]
                        for x in request.get('input', []) if isinstance(x, dict) and
                        x.get('type') in ('function_call_output', 'custom_tool_call_output')]
                    observed = '\n'.join(str(x) for x in results)
                    if self.scenario == 'host-tool':
                        expected = called and 'JPS_FIXTURE_OK' in observed
                    elif self.scenario == 'skills-list':
                        expected = any(json.loads(x) == {'skills': [], 'warnings': []} for x in results)
                    elif self.scenario == 'skills-read':
                        expected = 'skill package is not available from the requested authority' in observed
                    else:
                        expected = 'unsupported' in observed
                    summary.update({'toolCallback': called, 'turnCompleted': completed,
                        'modelRequests': len(self.requests), 'advertisedTools': sorted(tools),
                        'unexpectedTools': sorted(tools - UTILITY_TOOLS - {'jps_probe'}),
                        'expectedResult': bool(expected), 'privateImageReachedModel': any(
                            'data:image/' in json.dumps(r.get('input')) for r in self.requests),
                        'canaryWritten': self.canary.exists(),
                        'workspaceUnchanged': all(p.name == '.git' for p in work.iterdir()),
                        'serverRequests': self.server_requests, 'toolOutputs': results})
                finally:
                    # Reap the complete process group, including a failing probe's children.
                    try:
                        os.killpg(self.process.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                    try:
                        self.process.wait(timeout=3)
                    except subprocess.TimeoutExpired:
                        os.killpg(self.process.pid, signal.SIGKILL)
                        self.process.wait(timeout=3)
                    for stream in (self.process.stdin, self.process.stdout, self.process.stderr):
                        stream.close()
        except Exception as error:
            summary['error'] = str(error) or type(error).__name__
        finally:
            self.http.shutdown()
            self.http.server_close()
        summary['passed'] = all(summary.get(k) for k in (
            'accountAbsent', 'noMcpServers', 'turnCompleted', 'expectedResult', 'workspaceUnchanged')) and not any(
            summary.get(k) for k in ('unexpectedTools', 'canaryWritten', 'privateImageReachedModel', 'error'))
        if not summary['passed']:
            summary['diagnostics'] = errors
        return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--codex', required=True, type=Path)
    parser.add_argument('--model', default='gpt-5.5', help='Model metadata tested; inference stays on loopback')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--scenario', choices=SCENARIOS)
    parser.add_argument('--sandbox-bin', type=Path, help='Trusted bubblewrap executable for explicit sandbox diagnostics')
    parser.add_argument('--negative-control', action='store_true', help='Retain local environment; must fail tool isolation')
    args = parser.parse_args()
    binary = str(args.codex.resolve())
    results = [Probe(binary, args.model, scenario, args.negative_control, args.sandbox_bin).run()
               for scenario in ([args.scenario] if args.scenario else SCENARIOS)]
    report = {'model': args.model, 'binarySHA256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
              'passed': all(r['passed'] for r in results), 'results': results}
    serialized = json.dumps(report, indent=2) + '\n'
    if args.output:
        args.output.write_text(serialized)
    print(serialized, end='')
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
