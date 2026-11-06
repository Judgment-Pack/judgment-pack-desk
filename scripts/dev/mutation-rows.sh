#!/usr/bin/env bash
# DESK_MUTATION_BASE=REF scripts/dev/mutation-rows.sh
# A change's own rows of scripts/mutation-check.sh (issue #272): the rows it adds, and the rows
# it changes (another file, needle or replacement), found by comparing the harness at the merge
# base with REF with the current one (scripts/mutation-rows-changed.sh). Each runs as the nightly
# runs it, against the tests that can notice it, with the whole suite before any survivor is
# reported. Every row must be caught: a survivor or an INCONCLUSIVE row fails. A change that adds
# or changes no row ends in seconds; the rest of the list runs in mutation-nightly.yml.
#
# The run edits tracked files and restores them with `git checkout`, so the tree must be clean:
# commit first. The table, the progress log and the record are left in $RUNNER_TEMP/out (or the
# scratch directory of lib.sh); under CI the table is also added to the job summary.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root"

base=$(git merge-base "${DESK_MUTATION_BASE:?DESK_MUTATION_BASE must name the base to compare with}" HEAD)
rows="$tmp/rows"
out="$tmp/out"
summary=${GITHUB_STEP_SUMMARY:-/dev/null}
rm -rf "$rows" "$out"
mkdir -p "$out"

bash scripts/mutation-rows-changed.sh "$base" "$rows" | tee -a "$summary"
go_rows=$(wc -l < "$rows/go")
web_rows=$(wc -l < "$rows/web")
if [ "$go_rows" -eq 0 ] && [ "$web_rows" -eq 0 ]; then
  echo "mutation-rows: this change adds or changes no row"
  exit 0
fi

# As the nightly: Go mutations receive the pinned offline crypto fixture. A
# missing fixture is a setup failure, never a skipped-document mutant verdict.
if [ "$go_rows" -ne 0 ]; then
  . "$root/scripts/dev/gateway-fixture.sh"
  prepare_gateway_fixture
  . "$root/scripts/dev/dialect-mapping-fixture.sh"
  prepare_dialect_mapping_fixture
fi

# As the nightly: the web suite reads the bundle (src/testing/bundle.test.ts).
if [ "$web_rows" -ne 0 ]; then
  npm --prefix web ci
  (cd web && npx vite build)
fi

# Both halves run whatever the first found; the step fails after them if either had a row not
# caught. Progress goes to the log as it comes.
status=0
for half in go web; do
  [ -s "$rows/$half" ] || continue
  { bash scripts/mutation-check.sh "$half" --rows "$rows/$half" --record 2>&1 1>&3 3>&- \
      | tee "$out/progress-$half.log" >&2; } 3> "$out/table-$half.md" || status=1
  {
    echo "### $half rows"
    echo
    grep '^|' "$out/table-$half.md" || echo "(no table: see the job's log)"
    echo
    grep '^discriminating:' "$out/table-$half.md" || true
    echo
  } >> "$summary"
done
cp scripts/mutation-expects.tsv "$out/"
exit "$status"
