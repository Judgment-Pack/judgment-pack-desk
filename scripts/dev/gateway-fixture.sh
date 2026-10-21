# Sourced after lib.sh. Build only the repository-pinned Gateway bundle; never
# borrow an installed Gateway. All companion and mutation lanes share this path.
# shellcheck shell=bash
prepare_gateway_fixture() {
  local bundle="$tmp/dialect-gateway"
  if [ ! -f "$bundle/gateway-bundle.json" ]; then
    rm -rf "$bundle"
    python3 "$root/scripts/build-bundle.py" --gateway-only --output "$bundle" || return
  fi
  export JPS_TEST_GATEWAY_BUNDLE="$bundle"
  # Pass the selected test-toolchain interpreter explicitly to private fixtures.
  # Tests never resolve a provider program or interpreter by ambient name.
  export JPS_TEST_PYTHON="${JPS_TEST_PYTHON:-$(command -v python3)}"
  test -x "$JPS_TEST_PYTHON" || return
}
