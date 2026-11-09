#!/usr/bin/env bash
# scripts/dev/local-gateway.sh
# Builds the pinned local companions and the Desk (without the web build) into a scratch
# directory, then runs the real Gateway lifecycle and PDF extraction against that bundle.
# CI runs it on Linux and macOS (the "Local gateway" job of ci.yml).
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root"

bundle="$tmp/Desk bundle"
rm -rf "$bundle"
python3 scripts/build-bundle.py --skip-web --output "$bundle"
python3 scripts/local-gateway-check.py "$bundle" "$(command -v true)"
