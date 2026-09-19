#!/usr/bin/env python3
"""Check publisher OAuth in a synthetic bundle; never contacts Google.

Usage: python3 scripts/publisher-oauth-check.py /path/to/synthetic/bundle
The bundle must use publisher-test.apps.googleusercontent.com, not a real client.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
from urllib.parse import urlsplit, parse_qs


def main():
    binary = Path(sys.argv[1]).resolve() / 'gateway-connections'
    with tempfile.TemporaryDirectory(prefix='desk-publisher-oauth-') as directory:
        root = Path(directory)
        os.chmod(root, 0o700)
        for provider, scope in [('google-drive', 'drive.file'), ('gmail', 'gmail.readonly')]:
            command = [str(binary), '--state-dir', str(root), '--principal', 'test-owner', '--provider', provider]
            def exercise(disabled=False):
                process = subprocess.Popen(command + (['--disabled'] if disabled else []),
                    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
                deadline = threading.Timer(20, process.kill)
                deadline.start()
                def call(method, params=None):
                    process.stdin.write(json.dumps({'id':'synthetic', 'method':method, 'params':params or {}})+'\n')
                    process.stdin.flush()
                    line = process.stdout.readline()
                    assert line, 'connection companion exited'
                    reply = json.loads(line)
                    assert reply.get('id') == 'synthetic'
                    return reply
                try:
                    reply = call('status')
                    if disabled:
                        assert reply['result']['state'] == 'blocked'
                        assert call('connect')['error'] == 'blocked-by-policy'
                        return
                    assert reply['result']['state'] == 'not-connected', 'publisher default required a configure call'
                    assert 'clientId' not in json.dumps(reply) and 'synthetic-desktop-client' not in json.dumps(reply)
                    flow = call('pick' if provider == 'google-drive' else 'connect')['result']
                    url = urlsplit(flow['url']); query = parse_qs(url.query)
                    assert url.scheme == 'https' and url.netloc == 'accounts.google.com'
                    assert query['client_id'] == ['publisher-test.apps.googleusercontent.com']
                    assert query['scope'] == ['https://www.googleapis.com/auth/'+scope]
                    assert query['code_challenge_method'] == ['S256']
                    assert 'client_secret' not in query
                    callback = urlsplit(query['redirect_uri'][0])
                    assert callback.hostname == '127.0.0.1' and callback.port
                    assert call('cancel', {'id':flow['id']})['result']['state'] == 'canceled'
                finally:
                    process.stdin.close()
                    try:
                        assert process.wait(timeout=10) == 0
                    except subprocess.TimeoutExpired:
                        process.kill(); process.wait(); raise
                    finally:
                        deadline.cancel()
            exercise()
            exercise()  # Persistence/restart does not return to credential setup.
            exercise(True)
            exercise()  # Operator re-enablement retains the installed client.
    print('PASS: bundled publisher client starts Drive/Gmail OAuth without configure, preserves restart state, and respects disabled policy. No Google requests made.')


if __name__ == '__main__':
    main()
