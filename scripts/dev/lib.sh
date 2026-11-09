# Sourced by the scripts in scripts/dev/ that carry the steps of dev.json too long for one line.
# Sets $root (the repository) and $tmp (a scratch directory outside it; RUNNER_TEMP under CI).
# shellcheck shell=bash
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp"

# The attested release downloads and the component lookups read GH_TOKEN; where it is unset,
# use the login of the gh command when there is one.
if [ -z "${GH_TOKEN:-}" ] && command -v gh >/dev/null 2>&1; then
  GH_TOKEN=$(gh auth token 2>/dev/null || true)
  export GH_TOKEN
fi

# checkout_at DIR REPO COMMIT [full]: DIR holds a checkout of Judgment-Pack/REPO at the full
# COMMIT; DIR is under $tmp, or is .spec in the repository. A directory already at that commit
# is kept. "full" fetches the whole history and the tags (a signed release is verified from
# them); otherwise one commit is fetched.
checkout_at() {
  local dir=$1 repo=$2 commit=$3 mode=${4:-}
  if [ -d "$dir/.git" ] && [ "$(git -C "$dir" rev-parse HEAD 2>/dev/null)" = "$commit" ]; then
    return 0
  fi
  case "$dir" in
    "$tmp"/* | "$root/.spec") rm -rf "$dir" ;;
    *) echo "checkout_at: $dir is not a scratch checkout" >&2; return 1 ;;
  esac
  git init -q "$dir"
  git -C "$dir" remote add origin "https://github.com/Judgment-Pack/$repo.git"
  if [ "$mode" = full ]; then
    git -C "$dir" fetch -q --tags origin
  else
    git -C "$dir" fetch -q --depth 1 origin "$commit"
  fi
  git -C "$dir" checkout -q --detach "$commit"
  [ "$(git -C "$dir" rev-parse HEAD)" = "$commit" ]
}

# passed LOG NAME: the Go test log says that test NAME ran and passed.
passed() {
  awk -v want="--- PASS: $2 (" '{ sub(/^[ \t]+/, "") } index($0, want) == 1 { found = 1 } END { exit !found }' "$1"
}

# need_passed LOG NAME...: every named test ran and passed, or say which did not.
need_passed() {
  local log=$1 name
  shift
  for name in "$@"; do
    passed "$log" "$name" || { echo "$name did not run and pass"; return 1; }
  done
}
