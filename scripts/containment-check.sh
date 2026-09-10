#!/usr/bin/env bash
# Every pane is a containing block — measured in a browser, not read off source.
#
#   scripts/containment-check.sh <chassis-binary> <project-directory> [port] [label]
#
# `web/src/ui/containingBlock.test.ts` holds the declaring rules and says, in
# its own docstring, that it cannot hold the cascade: an override that reaches
# a pane by any other selector — an ancestor, an id, an attribute, a nested
# `&`, a `:global`, an inline style — is outside anything that parses text.
# This is the gate for that half. CI supplies no runtime binary and no project,
# so there is nothing for the chassis to serve; the gate is run by hand. Run
# before every merge that touches a stylesheet. This is a convention; nothing
# automated enforces it.
#
# It builds nothing. Build first, from the repository root:
#
#   npm --prefix web ci && npm --prefix web run build
#   go build -o /tmp/jpack-desk .
#   scripts/containment-check.sh /tmp/jpack-desk /path/to/project 8765
#
# What it arranges, and what it deliberately does not touch:
#
# - The project directory is **copied** into a temporary tree and the copy is
#   what the chassis is pointed at. This drives an editor; it must not drive
#   the tree it was handed.
# - `XDG_CONFIG_HOME` is a throwaway directory, so the desk this starts cannot
#   read or write the one the person running it uses.
# - The desk configuration it writes names an assistant endpoint on
#   `127.0.0.1:9` and stores a placeholder key. Nothing is ever sent there —
#   no run is started — but Admin renders its three Select pickers only once a
#   key is stored, and those pickers are what put a form-participation
#   `<select>` on the page. That is the element the whole invariant is about,
#   so the configuration that renders it is part of the measurement.
# - Every process it starts is killed **by PID**.
#
# - The desk is started with a fixed `--dev-token`, which is the **launch
#   secret**: `curl` presents it as `Authorization: Bearer` and the browser
#   trades it once at `GET /launch?secret=…` for a sixty-second, single-use
#   handoff; the page spends that for a session id it holds in `sessionStorage`
#   and puts on every later request itself.
#
# `JPACK_BIN` names the runtime binary to hand the chassis, if the project
# needs one. `PLAYWRIGHT_CHROME` names a Chrome executable; without it,
# playwright-core is asked for the installed one (`channel: 'chrome'`).
set -uo pipefail

cd "$(dirname "$0")/.."

BIN="${1:?usage: $0 <chassis-binary> <project-directory> [port] [label]}"
PROJECT="${2:?usage: $0 <chassis-binary> <project-directory> [port] [label]}"
PORT="${3:-8765}"
LABEL="${4:-$(git rev-parse --short HEAD 2>/dev/null || echo build)}"

[ -x "$BIN" ] || { echo "not an executable chassis: $BIN" >&2; exit 2; }
[ -d "$PROJECT" ] || { echo "not a project directory: $PROJECT" >&2; exit 2; }
[ -d web/node_modules/playwright-core ] || {
  echo "playwright-core is missing; run: npm --prefix web ci" >&2; exit 2
}

WORK="$(mktemp -d)"
DESK_PID=""
cleanup() {
  # **By PID only.** A pattern kill would reach whatever else on this machine
  # happens to be a desk.
  [ -n "$DESK_PID" ] && kill "$DESK_PID" 2>/dev/null
  rm -rf "$WORK"
  return 0
}
trap cleanup EXIT INT TERM

# The **launch secret**, fixed here so this script can present it. It is never
# on a request query: curl sends `Authorization: Bearer`, and the browser is
# handed one `GET /launch?secret=…` that trades it for a one-shot handoff.
SECRET="$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export XDG_CONFIG_HOME="$WORK/config"
mkdir -p "$XDG_CONFIG_HOME/jpack-desk"
cp -a "$PROJECT" "$WORK/project"

cat > "$XDG_CONFIG_HOME/jpack-desk/desk.json" <<JSON
{
  "deskConfigVersion": 1,
  "assistant": {
    "endpoint": {
      "url": "http://127.0.0.1:9",
      "kind": "gemini",
      "model": "unreachable-on-purpose",
      "tools": ["get_schema", "validate"]
    },
    "engine": "vercel",
    "thinking": "off"
  }
}
JSON

if [ -n "${JPACK_BIN:-}" ]; then
  "$BIN" --dev-token "$SECRET" --port "$PORT" --jpack "$JPACK_BIN" "$WORK/project" \
    > "$WORK/chassis.log" 2>&1 &
else
  "$BIN" --dev-token "$SECRET" --port "$PORT" "$WORK/project" > "$WORK/chassis.log" 2>&1 &
fi
DESK_PID=$!

AUTH="Authorization: Bearer $SECRET"
for _ in $(seq 1 150); do
  curl -sf -H "$AUTH" "http://127.0.0.1:$PORT/api/desk-config" >/dev/null 2>&1 && break
  sleep 0.2
done
if ! curl -sf -H "$AUTH" "http://127.0.0.1:$PORT/api/desk-config" >/dev/null 2>&1; then
  echo "the chassis did not come up on $PORT" >&2
  cat "$WORK/chassis.log" >&2
  exit 2
fi

# The key is stored, never sent: Admin renders no picker without one, and a
# picker is what renders the `<select>` this invariant exists for.
curl -sf -X PUT -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"key":"placeholder-never-sent-0000000000"}' \
  "http://127.0.0.1:$PORT/api/assistant/key" >/dev/null 2>&1 \
  || echo "note: no assistant key stored; Admin will render no pickers" >&2

# The source root is passed, so the measurement reads this repository's sheets,
# `App.tsx` and `playwright-core` rather than whatever directory a copy of the
# `.mjs` was run from.
node scripts/containment-check.mjs "$PORT" "$SECRET" "$LABEL" "$PWD"
CODE=$?

# Contents, not names and sizes: a copy that was edited in place keeps both.
contents() { (cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum); }
BEFORE="$(contents "$PROJECT")"
AFTER="$(contents "$WORK/project")"
echo "the copied project is unchanged (contents): $([ "$BEFORE" = "$AFTER" ] && echo yes || echo NO)"
exit $CODE
