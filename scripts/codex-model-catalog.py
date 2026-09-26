#!/usr/bin/env python3
"""Derive Desk's closed Codex model catalog from the pinned release's own.

Codex takes each model's tool mode, sub-agent version and experimental tools
from the model catalog before it reads the feature flags in config.toml, and a
signed-in client fetches that catalog from the account's server. Desk therefore
supplies its own catalog through `model_catalog_json`: the release's bundled
catalog (codex-rs/models-manager/models.json at the pinned tag, Apache-2.0),
with the hidden models dropped and those three fields cleared for every model
that remains. Everything else, including each model's instructions, is kept as
published.

The output is internal/codexbridge/model-catalog.json, which the bridge embeds,
writes into the private profile and verifies before every launch. Re-run this
when the runtime pin moves, then update the release and digest in catalog.go.
"""
import argparse
import hashlib
import json
import sys
import urllib.request
from pathlib import Path

RELEASE = 'rust-v0.157.1'
UPSTREAM = 'https://raw.githubusercontent.com/openai/codex/%s/codex-rs/models-manager/models.json' % RELEASE
UPSTREAM_SHA256 = '0178d235c589a31abd6ed0ea1e870935dc5819240eb0e813e178d3ebedf534f4'
CLEARED = {'tool_mode': None, 'multi_agent_version': None, 'experimental_supported_tools': []}
OUTPUT = Path(__file__).resolve().parent.parent / 'internal' / 'codexbridge' / 'model-catalog.json'


def derive(data):
    catalog = json.loads(data.decode('utf-8'))
    models = []
    for model in catalog['models']:
        if model.get('visibility') != 'list':
            continue
        for key in CLEARED:
            if key not in model:
                raise SystemExit('model %s has no %s field; the release format changed' % (model.get('slug'), key))
        model.update(CLEARED)
        models.append(model)
    if not models:
        raise SystemExit('no listed model in the release catalog')
    return {'models': models}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, help='A downloaded copy of the release catalog, instead of fetching it')
    parser.add_argument('--output', type=Path, default=OUTPUT)
    args = parser.parse_args()
    data = args.source.read_bytes() if args.source else urllib.request.urlopen(UPSTREAM, timeout=60).read()
    digest = hashlib.sha256(data).hexdigest()
    if digest != UPSTREAM_SHA256:
        raise SystemExit('release catalog digest %s is not the pinned %s' % (digest, UPSTREAM_SHA256))
    serialized = json.dumps(derive(data), indent=1, ensure_ascii=False) + '\n'
    args.output.write_text(serialized, encoding='utf-8')
    print('%s: %d listed models, %d bytes, sha256 %s' % (args.output, len(json.loads(serialized)['models']),
          len(serialized.encode('utf-8')), hashlib.sha256(serialized.encode('utf-8')).hexdigest()))
    return 0


if __name__ == '__main__':
    sys.exit(main())
