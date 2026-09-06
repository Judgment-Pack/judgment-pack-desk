#!/usr/bin/env bash
# Every mutation needle still matches its file, exactly once.
#
# A row whose needle has drifted reports MUTATION DID NOT APPLY, and a full pass
# takes long enough that nobody finds out until a review does — which is how a
# re-indentation in one PR left a row from an earlier one broken for four rounds
# of it. This applies nothing and runs no suite: it sources the row bodies with
# `apply` replaced by a check that the needle is present, and once.
#
#   scripts/needle-check.sh .
#
# It is not a substitute for running the rows. It answers one question — is the
# matrix complete — that a full pass answers slowly and this answers in a second.
set -uo pipefail
cd "$1"
missing=0
total=0
apply() {
  total=$((total+1))
  python3 - "$2" "$3" <<'PY' || { echo "STALE NEEDLE in $2 :: ${3:0:70}"; missing=$((missing+1)); }
import pathlib, sys
path, old = sys.argv[1], sys.argv[2]
s = pathlib.Path(path).read_text()
n = s.count(old)
if n == 0:
    sys.exit(1)
if n > 1:
    print(f"AMBIGUOUS ({n}x) in {path}: {old[:60]!r}")
PY
}
report() { :; }
run_web() { echo ""; }
run_go() { echo ""; }
restore() { :; }
mutate() {
  local lang="$1" name="$2" file="$3" needle="$4" replacement="$5"
  if ! apply "$lang" "$file" "$needle"; then echo "  ↳ row: $name"; fi
}
# Source only the row bodies: replace the harness's own preamble.
sed -n '/^if \[ "\$which" = all \] || \[ "\$which" = go \]; then$/,$p' scripts/mutation-check.sh \
  | sed 's/^\[ "\$fail" -eq 0 \]//' > /tmp/rows-only.sh
which=all
only=""
pass=0; fail=0; matched=0
source /tmp/rows-only.sh
echo "rows checked: $total   stale: $missing"
[ "$missing" -eq 0 ]
