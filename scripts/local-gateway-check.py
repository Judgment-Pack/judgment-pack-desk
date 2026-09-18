#!/usr/bin/env python3
"""Exercise an installed bundle without user settings, chats, models or network services.
Usage: python3 scripts/local-gateway-check.py /path/to/bundle /path/to/jpack
"""
import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.error
import uuid

TOKEN = 'local-gateway-isolated-test'

def request(url, body=None, method=None, headers=None):
    encoded = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, encoded, method=method, headers={
        'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json', **(headers or {})})
    with urllib.request.urlopen(req, timeout=20) as response:
        data = response.read()
        return json.loads(data) if data else None

def port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]

def wait_for(test, timeout=15):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            value = test()
            if value: return value
        except (urllib.error.URLError, ConnectionError, TimeoutError): pass
        time.sleep(.05)
    raise AssertionError('timed out')

def closed(url):
    try: urllib.request.urlopen(url + '/publickey', timeout=.2); return False
    except (urllib.error.URLError, ConnectionError, TimeoutError): return True

def sample_pdf():
    stream = b'BT /F1 12 Tf 72 720 Td (Local PDF processing works.) Tj ET'
    objects = [b'<< /Type /Catalog /Pages 2 0 R >>', b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
        b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>',
        b'<< /Length ' + str(len(stream)).encode() + b' >>\nstream\n' + stream + b'\nendstream',
        b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
    pdf = b'%PDF-1.4\n'
    offsets = [0]
    for number, obj in enumerate(objects, 1):
        offsets.append(len(pdf))
        pdf += str(number).encode() + b' 0 obj\n' + obj + b'\nendobj\n'
    xref = len(pdf)
    pdf += b'xref\n0 6\n0000000000 65535 f \n'
    for offset in offsets[1:]: pdf += ('%010d 00000 n \n' % offset).encode()
    return pdf + b'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n' + str(xref).encode() + b'\n%%EOF\n'

def main():
    bundle = Path(sys.argv[1]).resolve()
    runtime = str(Path(sys.argv[2]).resolve())
    processes = []
    with tempfile.TemporaryDirectory(prefix='desk-local-gateway-') as temp:
        root = Path(temp).resolve()
        project, config, data = [root / part for part in ('project', 'config', 'data')]
        for folder in (project, config, data): folder.mkdir(mode=0o700)
        env = {**os.environ, 'XDG_CONFIG_HOME': str(config), 'XDG_DATA_HOME': str(data)}
        def start():
            address = 'http://127.0.0.1:' + str(port())
            proc = subprocess.Popen([str(bundle / 'jpack-desk'), '--port', address.rsplit(':',1)[1], '--jpack', runtime, '--dev-token', TOKEN, '--print-url=false', str(project)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            processes.append(proc)
            answer = wait_for(lambda: request(address + '/api/desk-config'))
            assert answer['localGateway']['status'] == 'ready', answer.get('localGateway')
            return proc, address, answer
        try:
            first, desk, answer = start()
            gateway = answer['localGateway']['gateway']
            assert not (config / 'jpack-desk/desk.json').exists(), 'automatic setup edited user configuration'
            seed = config / 'jpack-desk/secrets/local-gateway.seed'
            assert seed.stat().st_mode & 0o777 == 0o600
            assert 'seed' not in json.dumps(answer)
            second, other_desk, other = start()
            assert other['localGateway']['gateway']['signer'] == gateway['signer']
            assert other['localGateway']['gateway']['url'] != gateway['url']
            # A real PDF goes through the document adapter and signing gateway.
            raw = sample_pdf()
            original = {'name': 'requirements.pdf', 'mediaType': 'application/pdf', 'bytes': base64.b64encode(raw).decode(), 'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()}
            session = str(uuid.uuid4())
            result = request(desk + '/api/research/gateway/acquire', {'session': session, 'source': 'documents', 'arguments': {'document': original, 'options': {'ocr': 'auto'}}})
            assert result['receipt']['receiptVersion'] == '3'
            assert result['receipt']['acquisition']['shape'] == 'command'
            assert 'Local PDF processing works.' in json.dumps(result['result'])
            request(desk + '/api/research/gateway/seal', {'session': session})
            # Managed defaults allow originals; explicit null and false disable them.
            def save_config(documents):
                current = request(desk + '/api/desk-config')
                return request(desk + '/api/desk-config', {'research': {'gateway': None, 'documents': documents}, 'ifMatch': current['sha256']}, 'PUT')
            for setting in (None, {'enabled': False, 'source': 'documents', 'maxFileBytes': 16777216, 'maxRequestBytes': 33554432, 'maxResponseBytes': 8388608}):
                save_config(setting)
                try:
                    request(desk + '/api/attachments/' + str(uuid.uuid4()), {'version': 1, 'original': original}, 'PUT', {'If-Match': 'absent'})
                    raise AssertionError('disabled PDF processing accepted new original')
                except urllib.error.HTTPError as error: assert error.code == 409
            first.terminate(); first.wait(timeout=12)
            wait_for(lambda: closed(gateway['url']))
            assert request(other_desk + '/api/desk-config')['localGateway']['status'] == 'ready'
            # Parent death, without running Desk cleanup, must close the companion.
            other_url = other['localGateway']['gateway']['url']
            second.kill(); second.wait(timeout=5)
            wait_for(lambda: closed(other_url))
            third, desk, restarted = start()
            assert restarted['localGateway']['gateway']['signer'] == gateway['signer'], 'identity rotated on restart'
            # Configured external gateways suppress automatic setup and remain byte-exact.
            configuration = {'deskConfigVersion': 1, 'research': {'gateway': {**gateway, 'url': 'http://127.0.0.1:1'}}}
            config_path = config / 'jpack-desk/desk.json'
            config_path.write_text(json.dumps(configuration))
            before = config_path.read_bytes()
            external = request(desk + '/api/desk-config')
            assert external['localGateway']['status'] == 'external'
            assert config_path.read_bytes() == before
            # Invalid configuration must not get a managed fallback.
            config_path.write_text('{broken')
            assert 'localGateway' not in request(desk + '/api/desk-config')
            print('PASS: automatic setup, two instances, signed extraction, disable/null, graceful shutdown, crash cleanup, stable identity, external preservation, invalid config refusal')
        finally:
            for proc in processes:
                if proc.poll() is None: proc.terminate()
            for proc in processes:
                try: proc.wait(timeout=12)
                except subprocess.TimeoutExpired: proc.kill(); proc.wait()

if __name__ == '__main__': main()
