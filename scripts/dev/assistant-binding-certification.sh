#!/usr/bin/env bash
# Certify the released API adapter through Desk's actual authenticated Go relay.
# Native certification remains blocked by its released unoffered-call oracle;
# this command does not report the native bridge or production factory certified.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root"
read -r repository revision < <(python3 - <<'PY'
import json
entry = json.load(open('internal/releaseplan/components.json'))['web-packages']['harness']
print(entry['repository'], entry['revision'])
PY
)
source_root=${DESK_ASSISTANT_CERTIFICATION_SOURCE:-$tmp/harness-source}
if [ -z "${DESK_ASSISTANT_CERTIFICATION_SOURCE:-}" ]; then
  checkout_at "$source_root" "${repository#Judgment-Pack/}" "$revision"
fi
[ "$(git -C "$source_root" rev-parse HEAD)" = "$revision" ] || {
  echo 'assistant-binding-certification: source does not match the component lock' >&2
  exit 1
}
[ -d "$source_root/node_modules" ] || (cd "$source_root" && npm ci)
export DESK_ASSISTANT_CERTIFICATION_SOURCE="$source_root"
log=$(mktemp "$tmp/assistant-binding-certification.XXXXXX.log")
trap 'rm -f "$log"' EXIT
if ! go test -count=1 -timeout=15m -v ./internal/desk -run '^TestAssistantBindingCertification(API|TimeoutReply)$' > "$log" 2>&1; then
  cat "$log"
  exit 1
fi
need_passed "$log" TestAssistantBindingCertificationTimeoutReply TestAssistantBindingCertificationAPI
cat "$log"
echo 'assistant-binding-certification: API relay passed; native and production factory are not certified by this command'
