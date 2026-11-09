#!/usr/bin/env python3
"""Validate actual private Plan refusal events against explicitly supplied Agent schemas."""
import copy
import hashlib
import json
from pathlib import Path
import sys

from jsonschema import Draft202012Validator
from referencing import Registry, Resource

schemas, outputs = map(Path, sys.argv[1:])
manifest = json.loads((schemas.parent / 'manifest.json').read_text())
if manifest['commit'] != '3f308215afadd9d72ae963ebea1062247d1963ba':
    raise SystemExit('explicit published Agent v0.6.0 schema fixture required')
resources = []
for entry in manifest['files']:
    raw = (schemas / entry['path']).read_bytes()
    if len(raw) != entry['bytes'] or hashlib.sha256(raw).hexdigest() != entry['sha256']:
        raise SystemExit('schema fixture identity changed: ' + entry['path'])
    schema = json.loads(raw)
    resources.append((schema['$id'], Resource.from_contents(schema)))
registry = Registry().with_resources(resources)
validator = Draft202012Validator(json.loads((schemas / 'run-event.json').read_text()), registry=registry)
events = 0
streams = sorted(outputs.glob('refusal-*.json'))
if len(streams) != 5:
    raise SystemExit('five actual malformed/refused Plan event streams required')
for path in streams:
    rows = json.loads(path.read_text())
    if not rows:
        raise SystemExit('empty refusal event stream')
    for row in rows:
        validator.validate(row)
        events += 1
schema = json.loads((outputs / 'input-schema.json').read_text())
vectors = json.loads((Path(__file__).resolve().parents[2] / 'internal/desk/testdata/assistant-plan-v1.json').read_text())
vector = vectors['arguments']
Draft202012Validator(schema).validate(vector)
for member in ('objective', 'steps', 'questions', 'references'):
    scratch = copy.deepcopy(schema)
    del scratch['properties'][member]
    if Draft202012Validator(scratch).is_valid(vector):
        raise SystemExit('input-schema member omission survived: ' + member)
for member in ('kind', 'id'):
    scratch = copy.deepcopy(schema)
    del scratch['properties']['references']['items']['properties'][member]
    if Draft202012Validator(scratch).is_valid(vector):
        raise SystemExit('reference member omission survived: ' + member)
for kind in ('file', 'dialect'):
    scratch = copy.deepcopy(schema)
    scratch['properties']['references']['items']['properties']['kind']['enum'].remove(kind)
    if Draft202012Validator(scratch).is_valid(vector):
        raise SystemExit('reference kind omission survived: ' + kind)
print(f'Plan refusal schemas: {len(streams)} actual streams, {events} events, six input members and both reference kinds carried against exact published Agent v0.6.0.')
