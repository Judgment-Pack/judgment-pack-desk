#!/usr/bin/env python3
"""Scoped source omissions; clean-Git mutation driver remains a CI requirement."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--language', choices=['go', 'web'], required=True)
args = parser.parse_args()
rows = json.loads((root / 'scripts/assistant-link-mutations.json').read_text())
out = Path(os.environ['JPDEV_SCRATCH']) / ('link-omissions-' + args.language)
out.mkdir(parents=True, exist_ok=True)
records = []
for index, row in enumerate(rows):
    if row['language'] != args.language:
        continue
    path = root / row['file']
    original = path.read_bytes()
    source = original.decode()
    if source.count(row['needle']) != 1:
        raise SystemExit('mutation needle is not unique: ' + row['name'])
    changed = source.replace(row['needle'], row['replacement'], 1).encode()
    if row['language'] == 'go':
        mutant = out / f'{index:02d}.go'
        mutant.write_bytes(changed)
        overlay = out / f'{index:02d}-overlay.json'
        overlay.write_text(json.dumps({'Replace': {str(path): str(mutant)}}))
        command = ['go', 'test', '-overlay', str(overlay), './internal/desk', '-run', '^' + row['witness'] + '$', '-count=1', '-timeout=90s', '-v']
        cwd = root
    else:
        path.write_bytes(changed)
        command = [str(root / 'web/node_modules/.bin/vitest'), 'run', 'src/chat/linkWindowParity.test.ts']
        cwd = root / 'web'
    try:
        result = subprocess.run(command, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
    finally:
        if row['language'] == 'web':
            path.write_bytes(original)
    log = out / f'{index:02d}.log'
    log.write_bytes(result.stdout)
    text = result.stdout.decode(errors='replace')
    named = ('--- FAIL: ' + row['witness']) in text if row['language'] == 'go' else row['witness'] in text and 'AssertionError' in text
    caught = result.returncode != 0 and named
    record = {'name': row['name'], 'witness': row['witness'], 'language': row['language'], 'sourceSHA256': hashlib.sha256(original).hexdigest(), 'mutantSHA256': hashlib.sha256(changed).hexdigest(), 'exit': result.returncode, 'namedWitness': named, 'caught': caught, 'log': str(log)}
    records.append(record)
    (out / 'index.json').write_text(json.dumps(records, indent=2) + '\n')
    if path.read_bytes() != original:
        raise SystemExit('source changed during omission check: ' + row['file'])
    print(('caught: ' if caught else 'INCONCLUSIVE: ') + row['name'], flush=True)
    if not caught:
        print(text[-5000:])
        raise SystemExit(1)
print(f'Link local omissions: {len(records)} named {args.language} rows caught; Unicode Go/browser twins are checked separately; server custody rows have no browser custody counterpart.')
