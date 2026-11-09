#!/usr/bin/env bash
# scripts/dev/policy-source-contract.sh
# The policy source producer contract. Builds the real Gateway canon and a focused Gateway test
# program from an exact Gateway fixture revision, records the source and build provenance,
# generates the unmodified real producer observation, and has the Desk's own test accept those
# exact bytes with the real canon. The fixture is a synthetic contract fixture, not an official
# component release and not a change to the component lock.
#
# The evidence (provenance.json, the logs, the observation and its digest) is left in
# $RUNNER_TEMP/policy-source-contract (or the scratch directory of lib.sh). Needs network access
# and git.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root"

revision=27df7367b6be5f380c6ae5989bde625bf9dde68a
fixture="$tmp/gateway-fixture"
evidence="$tmp/policy-source-contract"
export GOWORK=off GOTOOLCHAIN=local

checkout_at "$fixture" judgment-pack-gateway "$revision"
test "$(git -C "$fixture" rev-parse HEAD)" = "$revision"
test -z "$(git -C "$fixture" status --porcelain=v1)"
rm -rf "$evidence"
mkdir -p "$evidence"
go -C "$fixture/go" build -trimpath -buildvcs=true -o "$evidence/gateway-canon" .
go -C "$fixture/adapters" test -c -trimpath -buildvcs=true -o "$evidence/gateway-connections.test" ./connections
go -C "$root" test -c -trimpath -buildvcs=true -o "$evidence/desk.test" ./internal/desk

# Record actual source and build provenance.
GATEWAY_FIXTURE_REVISION="$revision" GATEWAY_FIXTURE="$fixture" EVIDENCE="$evidence" python3 - <<'PYTHON'
import hashlib
import json
import os
from pathlib import Path
import subprocess

root = Path.cwd()
evidence = Path(os.environ['EVIDENCE'])
def output(*args):
    return subprocess.check_output(args, text=True).strip()
def source(directory):
    status = output('git', '-C', str(directory), 'status', '--porcelain=v1')
    return {'revision': output('git', '-C', str(directory), 'rev-parse', 'HEAD'),
            'dirty': bool(status), 'status': status.splitlines()}
def program(name):
    path = evidence / name
    data = path.read_bytes()
    return {'sha256': hashlib.sha256(data).hexdigest(), 'byteLength': len(data),
            'buildInfo': output('go', 'version', '-m', str(path))}
record = {
    'kind': 'policy-source-producer-contract-fixture',
    'synthetic': True,
    'officialRelease': False,
    'componentLockRole': 'not used for fixture source selection or modified by this job',
    'gatewayFixture': {
        'repository': 'Judgment-Pack/judgment-pack-gateway',
        'requiredRevision': os.environ['GATEWAY_FIXTURE_REVISION'],
        **source(Path(os.environ['GATEWAY_FIXTURE']))},
    'deskUnderTest': source(root),
    'goVersion': output('go', 'version'),
    'buildFlags': ['-trimpath', '-buildvcs=true'],
    'programs': {name: program(name) for name in
                 ['gateway-canon', 'gateway-connections.test', 'desk.test']},
}
(evidence / 'provenance.json').write_text(json.dumps(record, indent=2) + '\n')
PYTHON

# Generate the unmodified real producer observation.
(
  cd "$fixture/adapters/connections"
  JPACK_TEST_SOURCE_OBSERVATION="$evidence/observation.json" \
    "$evidence/gateway-connections.test" \
      -test.run '^TestSourceObservationRetainsRealOriginalExtractionAndOpaqueRevisionAfterRestart$' \
      -test.count=1 -test.v | tee "$evidence/producer.log"
)
test -s "$evidence/observation.json"
grep -Fq -- '--- PASS: TestSourceObservationRetainsRealOriginalExtractionAndOpaqueRevisionAfterRestart ' "$evidence/producer.log"
(cd "$evidence" && sha256sum observation.json > observation.sha256)
EVIDENCE="$evidence" python3 - <<'PYTHON'
import json
import os
from pathlib import Path
evidence = Path(os.environ['EVIDENCE'])
provenance = json.loads((evidence / 'provenance.json').read_text())
observation = json.loads((evidence / 'observation.json').read_bytes())
expected = 'sha256:' + provenance['programs']['gateway-connections.test']['sha256']
if observation['document']['provenance']['adapter']['digest'] != expected:
    raise SystemExit('Observation does not name the recorded producer test binary')
PYTHON

# Accept the exact producer bytes with the real Gateway canon.
test -x "$evidence/gateway-canon"
(
  cd "$root/internal/desk"
  JPACK_POLICY_SOURCE_GATEWAY_CANON="$evidence/gateway-canon" \
  JPACK_POLICY_SOURCE_GATEWAY_OBSERVATION="$evidence/observation.json" \
    "$evidence/desk.test" \
      -test.run '^TestPolicySourceSyncRealGatewayCanonicalObservation$' \
      -test.count=1 -test.v | tee "$evidence/consumer.log"
)
grep -Fq -- '--- PASS: TestPolicySourceSyncRealGatewayCanonicalObservation ' "$evidence/consumer.log"
(cd "$evidence" && sha256sum --check observation.sha256)
