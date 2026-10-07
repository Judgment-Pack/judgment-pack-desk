#!/usr/bin/env bash
# The rows of scripts/mutation-check.sh that this commit adds or changes.
#
#   scripts/mutation-rows-changed.sh <base-commit> <out-dir>
#
# writes <out-dir>/go and <out-dir>/web: the names of the Go and web rows, one
# a line, the form `mutation-check.sh --rows` reads. A row counts where HEAD
# has it and <base-commit> has no row of that kind and name, or has one that
# mutates another file, needle or replacement. So a row whose needle moved
# with the code it targets is run again, and a row that was only removed is
# not (there is nothing left to run).
#
# The rows are read, not parsed: each version's row section is sourced with
# `mutate` replaced by one that prints the row (as needle-check.sh does), so a
# row built from variables or `printf` is compared as the harness would run it.
# Nothing is applied and no suite runs.
set -uo pipefail
base="${1:?usage: mutation-rows-changed.sh <base-commit> <out-dir>}"
out="${2:?usage: mutation-rows-changed.sh <base-commit> <out-dir>}"
cd "$(dirname "$0")/.." || exit 2
work="$(mktemp -d)" || exit 2
trap 'rm -rf "$work"' EXIT

# rows_of <harness file>: kind<TAB>name<TAB>digest of (file, needle, replacement), a row a line.
rows_of() {
  sed -n '/^if \[ "\$which" = all \] || \[ "\$which" = go \]; then$/,$p' "$1" \
    | sed 's/^\[ "\$fail" -eq 0 \]//' > "$work/rows.sh" || return 2
  (
    apply() { :; }; report() { :; }; run_web() { :; }; run_go() { :; }
    restore() { :; }; selection_report() { :; }
    mutate() {
      printf '%s\t%s\t%s\n' "$1" "$2" "$(printf '%s\0%s\0%s' "$3" "$4" "$5" | sha256sum | cut -c1-32)" >&3
    }
    which=all only="" rows="" pass=0 fail=0 matched=0
    # shellcheck disable=SC1091
    { source "$work/rows.sh" > /dev/null 2>&1 < /dev/null; } 3>&1
  )
}

git show "$base:scripts/mutation-check.sh" > "$work/base.sh" 2>/dev/null || : > "$work/base.sh"
rows_of "$work/base.sh" | LC_ALL=C sort > "$work/base.rows"
rows_of scripts/mutation-check.sh | LC_ALL=C sort > "$work/head.rows"
mkdir -p "$out"
LC_ALL=C comm -13 "$work/base.rows" "$work/head.rows" > "$work/changed"
for kind in go web; do
  awk -F'\t' -v k="$kind" '$1 == k { print $2 }' "$work/changed" > "$out/$kind"
done
echo "rows added or changed against $(git rev-parse --short "$base"): $(wc -l < "$out/go") Go, $(wc -l < "$out/web") web"
