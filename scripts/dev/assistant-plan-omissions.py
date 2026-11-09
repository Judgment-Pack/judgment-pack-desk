#!/usr/bin/env python3
"""Scoped source omissions; clean-Git mutation driver remains a CI requirement."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--language', choices=['go'], required=True)
parser.add_argument('--only', default='')
args = parser.parse_args()
selected = [value for value in args.only.split(',') if value]
rows = json.loads((root / 'scripts/assistant-plan-mutations.json').read_text())
out = Path(os.environ['JPDEV_SCRATCH']) / ('plan-omissions-' + args.language)
out.mkdir(parents=True, exist_ok=True)
records = []
for index, row in enumerate(rows):
    if row['language'] != args.language or selected and not any(value in row['name'] for value in selected):
        continue
    path = root / row['file']
    original = path.read_bytes()
    source = original.decode()
    if source.count(row['needle']) != 1:
        raise SystemExit('mutation needle is not unique: ' + row['name'])
    changed = source.replace(row['needle'], row['replacement'], 1).encode()
    mutant = out / f'{index:02d}.go'
    mutant.write_bytes(changed)
    overlay = out / f'{index:02d}-overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(path): str(mutant)}}))
    command = ['go', 'test', '-overlay', str(overlay), './internal/desk', '-run', '^' + row['witness'] + '$', '-count=1', '-timeout=90s', '-v']
    result = subprocess.run(command, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
    log = out / f'{index:02d}.log'
    log.write_bytes(result.stdout)
    text = result.stdout.decode(errors='replace')
    named = ('--- FAIL: ' + row['witness']) in text and row['failure'] in text
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
if not records:
    raise SystemExit('no named omissions selected')
print(f'Plan local omissions: {len(records)} named Go rows caught; new custody/reference admission is Go-only, canonical browser readers are exercised separately.')
