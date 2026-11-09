#!/usr/bin/env bash
# scripts/dev/development-build.sh
# When the component lock holds a development pin, build the exact development companions, build
# the Desk with them and start the companions' programs. With no development pin there is
# nothing to build: the step tests packaging, says so and passes. The release job
# (release-archives) builds the archives in that case instead. Run on each platform CI builds for (see the development-bundle
# job of ci.yml); needs network access for the companions' sources.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root"

# Packaging contracts must run on native macOS too, including the no-development branch.
python3 scripts/package-release_test.py

development=$(python3 -c 'import json; p=json.load(open("internal/releaseplan/components.json")); print(str(any(c["channel"]=="development" for c in p["components"].values())).lower())')
if [ "$development" != true ]; then
  echo "development-build: the component lock has no development pin; nothing to build"
  exit 0
fi

npm --prefix web ci
npm --prefix web run build
set -o pipefail
python3 scripts/dev-components.py | tee "$tmp/development-companions.log"
companions=$(tail -n 1 "$tmp/development-companions.log")
test -d "$companions"
rm -rf "$tmp/desk-development"
cp -a "$companions" "$tmp/desk-development"
go build -trimpath -o "$tmp/desk-development/jpack-desk" .
"$tmp/desk-development/jpack-desk" --desk-home-layout-version
"$tmp/desk-development/jpack-runner" relocate-workspace --capabilities
