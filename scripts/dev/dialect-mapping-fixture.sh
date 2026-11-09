# Sourced after lib.sh. Reuse the attested companion owner for only the new
# mapping/publication tests, without enabling older JPACK_BIN-gated baselines.
# shellcheck shell=bash
prepare_dialect_mapping_fixture() {
  if [ "${DESK_DIALECT_MAPPING_RUNTIME+x}" != x ]; then
    DESK_DIALECT_MAPPING_RUNTIME=$("${root:?}/scripts/dev/jobs-companion.sh" dialect-mapping-runtime) || return
  fi
  case "$DESK_DIALECT_MAPPING_RUNTIME" in
    /*) ;;
    *) echo 'Dialect mapping Runtime fixture must be an explicit absolute path' >&2; return 1 ;;
  esac
  if [ ! -f "$DESK_DIALECT_MAPPING_RUNTIME" ] || [ ! -x "$DESK_DIALECT_MAPPING_RUNTIME" ]; then
    echo 'The configured Dialect mapping Runtime fixture is unavailable' >&2
    return 1
  fi
  export DESK_DIALECT_MAPPING_RUNTIME
}
