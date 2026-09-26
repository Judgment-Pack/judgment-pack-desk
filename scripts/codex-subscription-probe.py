#!/usr/bin/env python3
"""Probe Codex 0.157.1 with a scripted loopback model and an empty profile.

No real account, login, provider key, or subscription inference is used. Each
scenario starts a new process. Exit 0 means the local protocol checks passed;
it does not certify real authentication, entitlement, or a production engine.

The profile is Desk's: the same disabled features, and Desk's closed model
catalog supplied through `model_catalog_json`, because Codex reads a model's
tool mode from the catalog before the feature flags. Every model the process
lists is probed unless --model narrows it. The tool inventory counts every list
whose member name contains `tool`, wherever it appears in a model request, and
each request must advertise the host tool exactly once, and the host call is
scripted in the advertised form; a forged native call must be rejected as an
unknown tool, word for word, with no native request reaching the host.
--bundled-catalog leaves the release's own catalog in place and
--negative-control retains an environment (with the image-view feature left
on, so the environment has a tool to register): each is a control that must fail,
and exits 0 only when it failed for the right reason, by advertising a tool
beyond the host's after every scenario ran to completion.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import queue
import re
import signal
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

VERSION = 'codex-cli 0.157.1'
CATALOG = Path(__file__).resolve().parent.parent / 'internal' / 'codexbridge' / 'model-catalog.json'
SCENARIOS = ('host-tool', 'private-image', 'write', 'shell', 'web', 'subagent',
             'skills-list', 'skills-read')
# The host tool, advertised plainly or inside the `functions` namespace some
# models use. This release registers no other native tool without an
# environment, and none is allowed.
HOST_TOOLS = {'jps_probe', 'functions.jps_probe'}
# A forged native call must come back as an unknown tool, in these words; any
# other refusal would mean a registered tool declined its arguments.
REJECTIONS = {'private-image': 'unsupported call: view_image',
              'write': 'unsupported custom tool call: apply_patch',
              'shell': 'unsupported call: exec_command', 'web': 'unsupported call: web.run',
              'subagent': 'unsupported call: spawn_agent',
              'skills-list': 'unsupported call: skillslist', 'skills-read': 'unsupported call: skillsread'}
DISABLED_FEATURES = ('shell_tool', 'unified_exec', 'shell_snapshot', 'multi_agent',
    'multi_agent_v2', 'apps', 'hooks', 'plugins', 'remote_plugin', 'plugin_sharing',
    'memories', 'goals', 'browser_use', 'browser_use_external',
    'browser_use_full_cdp_access', 'computer_use', 'in_app_browser',
    'skill_mcp_dependency_install', 'skill_search', 'workspace_dependencies',
    'auth_elicitation', 'tool_suggest', 'image_generation', 'code_mode',
    'code_mode_host', 'enable_request_compression', 'view_image', 'token_budget')


def tool_inventory(request):
    """Return {qualified tool name: [request paths]} for every tool list in a request.

    Tools can arrive outside the top-level `tools` member, for example in an
    `additional_tools` input item, so every list whose member name contains
    `tool` is counted wherever it appears, and a tool's own members are
    searched as well. A tool without a name is recorded by its type, so it can
    never pass as the host tool.
    """
    found = {}
    def names(tools, prefix, path):
        for i, tool in enumerate(tools):
            here = '%s[%d]' % (path, i)
            if not isinstance(tool, dict):
                found.setdefault(prefix + repr(tool), []).append(here)
                continue
            name = tool.get('name') if isinstance(tool.get('name'), str) else None
            if tool.get('type') == 'namespace' and name and isinstance(tool.get('tools'), list):
                names(tool['tools'], prefix + name + '.', here + '.tools')
                # A namespace's other members are searched like any tool's.
                walk({k: v for k, v in tool.items() if k not in ('name', 'tools')}, here)
                continue
            found.setdefault(prefix + (name or 'type:' + str(tool.get('type'))), []).append(here)
            walk({k: v for k, v in tool.items() if k != 'name'}, here)
    def walk(node, path):
        if isinstance(node, dict):
            for key, value in node.items():
                here = path + '.' + key if path else key
                if isinstance(value, list) and 'tool' in key.lower():
                    names(value, '', here)
                else:
                    walk(value, here)
        elif isinstance(node, list):
            for i, value in enumerate(node):
                kind = ':' + str(value.get('type')) if isinstance(value, dict) and 'type' in value else ''
                walk(value, '%s[%d%s]' % (path, i, kind))
    walk(request, '')
    return found


def advertised(request):
    """Return the inventory of the request's definition channels only.

    A tool is offered to the model through the top-level `tools` member or an
    `additional_tools` input item. Only those count as an advertisement; the
    wider scan above may also see tool-shaped data inside a result, which can
    fail a scenario but never pass one.
    """
    channels = {'tools': request.get('tools', [])}
    for i, item in enumerate(request.get('input', [])):
        if isinstance(item, dict) and item.get('type') == 'additional_tools':
            channels['input[%d:additional_tools].tools' % i] = item.get('tools', [])
    return tool_inventory(channels)


class Probe:
    def __init__(self, binary, model, scenario, catalog, environment=False, sandbox_bin=None):
        self.binary, self.model, self.scenario = binary, model, scenario
        self.catalog = catalog
        self.environment = environment
        self.sandbox_bin = sandbox_bin
        self.requests = []
        self.messages = queue.Queue()
        self.next_id = 0
        self.server_requests = []
        self.process = None

    def tool_item(self, namespaced=False):
        """The scripted model's one call; the host call in the advertised form."""
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
        if self.scenario == 'host-tool' and namespaced:
            item['namespace'] = 'functions'
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
                namespaced = 'functions.jps_probe' in tool_inventory(body)
                item = probe.tool_item(namespaced) if sequence == 1 else {
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
        # The model listing needs no model; a scenario names the one it probes.
        # The environment control keeps the image-view feature on, so that a
        # retained environment has a tool to register; Desk's profile has it off.
        selected = 'model = %s\n' % json.dumps(self.model) if self.model else ''
        catalog = ''
        if self.catalog is not None:
            catalog = 'model_catalog_json = %s\n' % json.dumps(str(self.catalog_path))
        return '''%smodel_provider = "jps_probe"
%sapproval_policy = "never"
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
''' % (selected, catalog, json.dumps(str(work)), json.dumps(str(profile)),
        self.http.server_port, '\n'.join(k + ' = false' for k in DISABLED_FEATURES if not (self.environment and k == 'view_image')))

    def launch(self, base, summary, errors):
        """Prepare the empty private profile, start the process and shake hands."""
        home, work = base/'home', base/'work'
        profile = home/'.codex'
        for p in (home, profile, work):
            p.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.private_image = profile/'secret-fixture.png'
        self.private_image.write_bytes(base64.b64decode(
            'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1N8AAAAASUVORK5CYII='))
        self.canary = work/'patch-canary.txt'
        self.catalog_path = profile/'model-catalog.json'
        if self.catalog is not None:
            self.catalog_path.write_bytes(self.catalog)
            self.catalog_path.chmod(0o600)
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
        hello = self.rpc('initialize', {'clientInfo': {'name': 'jps_isolation_probe', 'version': '0.1.0'},
            'capabilities': {'experimentalApi': True}})
        summary['userAgent'] = hello.get('userAgent', '')
        self.write({'method': 'initialized'})
        effective = self.rpc('config/read', {'includeLayers': False})['config']
        summary['noMcpServers'] = not effective.get('mcp_servers')
        # The catalog is in effect when the process names Desk's file, or when
        # none was supplied and the process names none.
        applied = effective.get('model_catalog_json')
        summary['catalogApplied'] = applied == (str(self.catalog_path) if self.catalog is not None else None)
        return work

    def stop(self):
        # Reap the complete process group, including a failing probe's children.
        if self.process is None:
            return
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

    def models(self):
        """List every model the process offers, hidden ones included."""
        summary, errors = {}, []
        self.serve()
        try:
            with tempfile.TemporaryDirectory(prefix='jps-codex-proof-') as scratch:
                try:
                    self.launch(Path(scratch), summary, errors)
                    listed = self.rpc('model/list', {'limit': 100, 'includeHidden': True})
                    if listed.get('nextCursor'):
                        raise RuntimeError('model/list paginated')
                    summary['models'] = [row['model'] for row in listed['data']]
                except Exception as error:
                    raise RuntimeError('%s: %s' % (error, '; '.join(errors)))
                finally:
                    self.stop()
        finally:
            self.http.shutdown()
            self.http.server_close()
        if not summary.get('catalogApplied'):
            raise RuntimeError('the process did not apply the expected catalog: ' + '; '.join(errors))
        return summary['models']

    def run(self):
        summary = {'model': self.model, 'scenario': self.scenario, 'environmentEnabled': self.environment}
        errors = []
        self.serve()
        try:
            with tempfile.TemporaryDirectory(prefix='jps-codex-proof-') as scratch:
                try:
                    work = self.launch(Path(scratch), summary, errors)
                    account = self.rpc('account/read', {'refreshToken': False})
                    summary['accountAbsent'] = account.get('account') is None
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
                                # Desk refuses a namespaced callback, so record the
                                # namespace the host saw and fail on any.
                                summary['hostCallNamespace'] = params.get('namespace')
                                if params.get('namespace') is not None:
                                    raise RuntimeError('Host tool call carried a namespace: %r' % params.get('namespace'))
                                called = True
                                self.write({'id': message['id'], 'result': {'success': True,
                                    'contentItems': [{'type': 'inputText', 'text': 'JPS_FIXTURE_OK'}]}})
                            else:
                                self.write({'id': message['id'], 'error': {'code': -32601, 'message': 'Not permitted'}})
                        elif message.get('method') == 'turn/completed':
                            completed = message['params']['turn']['status'] == 'completed'
                            break
                    tools, channels, host_each = set(), {}, []
                    for request in self.requests:
                        inventory = tool_inventory(request)
                        # Each request must advertise the host tool exactly once,
                        # in exactly one of its two forms, through a definition
                        # channel; anything else found anywhere is unexpected.
                        host_each.append(sum(len(paths) for name, paths in advertised(request).items() if name in HOST_TOOLS) == 1)
                        for name, paths in inventory.items():
                            tools.add(name)
                            channels.setdefault(name, set()).update(
                                re.sub(r'\[\d+', '[', path) for path in paths)
                    # Only the scripted call's own result counts.
                    results = [x.get('output', '') for request in self.requests[1:]
                        for x in request.get('input', []) if isinstance(x, dict) and
                        x.get('type') in ('function_call_output', 'custom_tool_call_output') and
                        x.get('call_id') == 'call_probe']
                    observed = '\n'.join(str(x) for x in results)
                    if self.scenario == 'host-tool':
                        expected = called and 'JPS_FIXTURE_OK' in observed
                    else:
                        # The router's own unknown-tool refusal, and no native
                        # request of any kind reached the host.
                        expected = REJECTIONS[self.scenario] in results and not self.server_requests
                    summary.update({'toolCallback': called, 'turnCompleted': completed,
                        'modelRequests': len(self.requests), 'advertisedTools': sorted(tools),
                        'hostToolAdvertised': bool(host_each) and all(host_each),
                        'unexpectedTools': sorted(tools - HOST_TOOLS),
                        'toolChannels': {k: sorted(v) for k, v in sorted(channels.items())},
                        'expectedResult': bool(expected), 'privateImageReachedModel': any(
                            'data:image/' in json.dumps(r.get('input')) for r in self.requests),
                        'canaryWritten': self.canary.exists(),
                        'workspaceUnchanged': all(p.name == '.git' for p in work.iterdir()),
                        'serverRequests': self.server_requests, 'toolOutputs': results})
                finally:
                    self.stop()
        except Exception as error:
            summary['error'] = str(error) or type(error).__name__
        finally:
            self.http.shutdown()
            self.http.server_close()
        summary['passed'] = all(summary.get(k) for k in (
            'accountAbsent', 'noMcpServers', 'catalogApplied', 'hostToolAdvertised', 'turnCompleted',
            'expectedResult', 'workspaceUnchanged')) and not any(
            summary.get(k) for k in ('unexpectedTools', 'canaryWritten', 'privateImageReachedModel', 'error'))
        if not summary['passed']:
            summary['diagnostics'] = errors
        return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--codex', required=True, type=Path)
    parser.add_argument('--catalog', type=Path, default=CATALOG, help="Desk's closed model catalog")
    parser.add_argument('--bundled-catalog', action='store_true', help="Leave the release's own catalog in place; must fail")
    parser.add_argument('--model', action='append', help='Model metadata tested (repeatable); default every listed model. Inference stays on loopback')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--scenario', choices=SCENARIOS)
    parser.add_argument('--sandbox-bin', type=Path, help='Trusted bubblewrap executable for explicit sandbox diagnostics')
    parser.add_argument('--negative-control', action='store_true', help='Retain local environment; must fail tool isolation')
    args = parser.parse_args()
    binary = str(args.codex.resolve())
    catalog = None if args.bundled_catalog else args.catalog.read_bytes()
    models = Probe(binary, None, None, catalog, args.negative_control, args.sandbox_bin).models()
    report = {'binarySHA256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
              'catalogSHA256': hashlib.sha256(catalog).hexdigest() if catalog is not None else None,
              'models': models}
    if catalog is not None:
        expected = sorted(m['slug'] for m in json.loads(catalog)['models'])
        report['catalogListed'] = sorted(models) == expected
    for model in args.model or []:
        if model not in models:
            raise SystemExit('model %s is not listed by this process' % model)
    results = [Probe(binary, model, scenario, catalog, args.negative_control, args.sandbox_bin).run()
               for model in (args.model or models)
               for scenario in ([args.scenario] if args.scenario else SCENARIOS)]
    report['passed'] = all(r['passed'] for r in results) and report.get('catalogListed', True)
    control = args.bundled_catalog or args.negative_control
    if control:
        # A control holds only when every scenario ran to completion with its
        # own evidence intact, the catalog was applied and listed as expected,
        # and at least one scenario advertised a tool beyond the host's; any
        # other failure is the probe's, not the boundary's.
        def evidence(r):
            outputs = '\n'.join(str(o) for o in r.get('toolOutputs', []))
            if r['scenario'] == 'host-tool':
                return r.get('toolCallback') and 'JPS_FIXTURE_OK' in outputs
            if r['scenario'] == 'private-image' and args.sandbox_bin:
                # The diagnostic lets the image tool run so that the permission
                # profile, not the router, is what denies the private read.
                return 'Permission denied' in outputs and not r.get('privateImageReachedModel')
            return REJECTIONS[r['scenario']] in r.get('toolOutputs', [])
        intact = ('accountAbsent', 'noMcpServers', 'catalogApplied', 'turnCompleted', 'workspaceUnchanged')
        listed = report['catalogListed'] if catalog is not None else set(models) > set(m['slug'] for m in json.loads(args.catalog.read_bytes())['models'])
        report['controlHeld'] = bool(results) and listed and all(
            not r.get('error') and not r.get('canaryWritten') and all(r.get(k) for k in intact) and evidence(r)
            for r in results) and any(r.get('unexpectedTools') for r in results)
    report['results'] = results
    serialized = json.dumps(report, indent=2) + '\n'
    if args.output:
        args.output.write_text(serialized)
    print(serialized, end='')
    return 0 if (report['controlHeld'] if control else report['passed']) else 1


if __name__ == '__main__':
    raise SystemExit(main())
