#!/usr/bin/env python3
"""Finite Runtime for the recovery journey; all model responses are local fixtures."""
import hashlib
import json
import os
import sys

project, trace = sys.argv[1:3]
if sys.argv[3:] != ['mcp']:
    sys.exit(61)


def record(kind):
    fd = os.open(trace, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
    try:
        os.write(fd, (json.dumps({'kind': kind, 'role': 'runtime', 'pid': os.getpid(), 'tag': ''}) + '\n').encode())
    finally:
        os.close(fd)


def content(value):
    return {'content': [{'type': 'text', 'text': json.dumps(value)}], 'structuredContent': value, 'isError': False}


record('start')
for index, line in enumerate(sys.stdin):
    if index >= 256 or len(line) > 4 * 1024 * 1024:
        record('refused')
        sys.exit(65)
    request = json.loads(line)
    method, params = request.get('method'), request.get('params', {})
    if 'id' not in request and method in ['initialized', 'notifications/initialized']:
        continue
    if method == 'initialize':
        result = {'protocolVersion': '2025-06-18', 'capabilities': {'tools': {}, 'prompts': {}}, 'serverInfo': {'name': 'offline-recovery', 'version': '1'}}
    elif method == 'tools/list':
        result = {'tools': [{'name': name, 'description': name, 'inputSchema': {'type': 'object', 'properties': {}, 'additionalProperties': True}} for name in ['get_schema', 'get_pack', 'list_packs']]}
    elif method == 'prompts/list':
        result = {'prompts': [{'name': 'author_pack', 'arguments': []}]}
    elif method == 'prompts/get' and params == {'name': 'author_pack', 'arguments': {}}:
        result = {'messages': [{'role': 'user', 'content': {'type': 'text', 'text': 'Read the schema once. Preserve completed observations and await explicit recovery actions.'}}]}
    elif method == 'tools/call' and params.get('name') == 'get_schema' and params.get('arguments', {}) == {}:
        record('schema')
        result = content({'schema': 'recorded'})
    elif method == 'tools/call' and params.get('name') == 'list_packs' and params.get('arguments', {}) == {}:
        result = content({'packs': [{'id': 'one', 'packId': 'https://example.invalid/judgment-packs/minimal', 'packVersion': '0.1.0', 'path': 'one.json', 'description': 'A minimal pack'}]})
    elif method == 'tools/call' and params.get('name') == 'get_pack' and params.get('arguments') == {'pack_id': 'one'}:
        with open(os.path.join(project, 'one.json')) as pack:
            text = pack.read()
        value = json.loads(text)
        result = {'content': [{'type': 'text', 'text': text}], 'structuredContent': {'id': 'one', 'path': 'one.json', 'sha256': hashlib.sha256(text.encode()).hexdigest(), 'bytes': len(text.encode()), 'packVersion': value['version']}, 'isError': False}
    elif method == 'tools/call' and params.get('name') == 'describe_runtime' and params.get('arguments', {}) == {}:
        result = content({'name': 'offline-recovery'})
    else:
        record('refused')
        sys.exit(65)
    record('metadata')
    print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}), flush=True)
