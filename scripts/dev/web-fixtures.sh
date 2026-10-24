#!/usr/bin/env bash
# scripts/dev/web-fixtures.sh STEP
# The steps of dev.json that read another repository's pinned files as test input:
#   dialect-read     a Desk-produced dialect preparation, verified by the pinned specification
#   harness-vectors  the Desk's copies of the harness package's vectors are the package's own
#   certify ADAPTER  the package's certification suite, run on the Desk's wiring
# The other repositories are checked out into a scratch directory (RUNNER_TEMP under CI), except
# the specification, which the web script reads at .spec in the repository (ignored by git).
# Needs network access and git; a dialect-read also needs python3 with venv and pip.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root"

# The package's repository and revision are the component lock's own, never written here.
harness_source() {
  local line repository revision
  line=$(python3 - <<'PY'
import json
entry = json.load(open('internal/releaseplan/components.json'))['web-packages']['harness']
print(entry['repository'], entry['revision'])
PY
)
  repository=${line% *}
  revision=${line#* }
  checkout_at "$tmp/harness-source" "${repository#Judgment-Pack/}" "$revision"
}

step=${1:?usage: web-fixtures.sh STEP}
case "$step" in
  dialect-read)
    checkout_at "$root/.spec" judgment-pack-spec a550d414b5a3159fe3526a93f6f56b8c0ccdb76d
    python3 -m venv "$tmp/spec-venv"
    "$tmp/spec-venv/bin/python" -m pip install --disable-pip-version-check -q -r .spec/requirements-dev.txt
    vector=$(cd web && node --import tsx scripts/dialect-roundtrip.mjs)
    trap 'rm -rf "$vector"' EXIT
    "$tmp/spec-venv/bin/python" .spec/tools/dialect_verify.py \
      --dialect "$vector/mapping.json" --case "$vector/case.json" \
      --acquisitions "$vector/sources.json" --profiles "$vector/profiles.json" \
      --preparation "$vector/preparation.json" --record "$vector/record.json" \
      --require-binding > "$vector/report.json"
    python3 -c 'import json, sys; report = json.load(open(sys.argv[1])); print(json.dumps(report)); assert report["result"] == "reading"' "$vector/report.json" > /dev/null
    echo "dialect-read: the specification's verifier reads the Desk's preparation"
    ;;

  harness-vectors)
    harness_source
    diff internal/desk/testdata/harness-prompt/host-parts.json "$tmp/harness-source/contract/vectors/budget/host-parts.json"
    diff internal/desk/testdata/harness-prompt/state-placement.json "$tmp/harness-source/contract/vectors/budget/state-placement.json"
    diff internal/desk/testdata/harness-approval/grant-states.json "$tmp/harness-source/contract/vectors/approval/grant-states.json"
    echo "harness-vectors: current host-parts, state-placement and approval vectors match their published revisions"
    ;;

  certify)
    adapter=${2:?usage: web-fixtures.sh certify ADAPTER}
    harness_source
    # Source imports resolve dependencies from this checkout, not web/node_modules. Its committed
    # workspace lock is at the same attested revision as the tests. Vector comparison needs none.
    [ -d "$tmp/harness-source/node_modules" ] || (cd "$tmp/harness-source" && npm ci)
    cd web
    HARNESS_SOURCE="$tmp/harness-source/packages/harness" node scripts/certify-harness.mjs --adapter "$adapter"
    ;;

  *)
    echo "web-fixtures.sh: no step named $step" >&2
    exit 2
    ;;
esac
