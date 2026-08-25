#!/usr/bin/env bash
# Break each safeguard on purpose, and record which test notices.
#
# A passing test is not evidence that it tests anything. This applies one
# mutation at a time to a clean tree, runs the suite, records which tests fail,
# and restores. A mutation that leaves the suite green is the interesting
# result: it names a safeguard nothing is holding in place, and it is reported
# as such rather than quietly dropped.
#
#   scripts/mutation-check.sh            # every mutation
#   scripts/mutation-check.sh go         # the Go ones only
#   scripts/mutation-check.sh web        # the web ones only
#
# The tree must be clean: this edits tracked files and restores them with
# `git checkout`, which would discard uncommitted work. Commit first.
set -uo pipefail

cd "$(dirname "$0")/.."
which="${1:-all}"
# An optional substring: run only the rows whose name contains it. Re-verifying
# one repaired row should not mean re-running a half that takes forty minutes,
# and a filter kept in the harness is reproducible where an ad-hoc helper is not.
only="${2:-}"

if [ -n "$(git status --porcelain)" ]; then
  echo "the tree is not clean; commit before mutating (this restores with git checkout)" >&2
  exit 2
fi

commit="$(git rev-parse HEAD)"

restore() { git checkout -- internal web/src 2>/dev/null; }
# However this ends — a failing mutation, Ctrl-C, a kill — the tree goes back.
# A harness that leaves a mutation in place is worse than no harness: the next
# thing anyone runs is testing something nobody wrote.
trap restore EXIT INT TERM

pass=0
fail=0

# apply <file> <needle> <replacement> — asserts the needle is present, so a
# mutation that silently no-ops cannot be read as "the suite survived it".
apply() {
  python3 - "$1" "$2" "$3" <<'PY'
import pathlib, sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
p = pathlib.Path(path)
s = p.read_text()
if old not in s:
    sys.exit("MUTATION DID NOT APPLY: " + old[:70])
p.write_text(s.replace(old, new, 1))
PY
}

report() { # report <name> <result-line>
  printf '| %-52s | %s |\n' "$1" "$2"
}

# A mutation that does not compile, panics, or hangs the suite has not been
# survived — it has not been tested. Reporting any of those as "nothing failed"
# would be the most dangerous thing this script could do, so each is named.
run_go() {
  local out named
  out="$(go test ./internal/desk -count=1 -timeout 45s 2>&1)"
  if grep -q 'build failed\|cannot use\|undefined:\|declared and not used\|syntax error' <<<"$out"; then
    echo "INCONCLUSIVE — did not compile"
    return
  fi
  if grep -q 'panic: test timed out' <<<"$out"; then
    echo "INCONCLUSIVE — suite timed out (the mutation hangs a handler)"
    return
  fi
  if grep -q '^panic:' <<<"$out"; then
    echo "INCONCLUSIVE — suite panicked"
    return
  fi
  named="$(grep -E '^--- FAIL|^    --- FAIL' <<<"$out" | sed 's/^ *--- FAIL: //;s/ (.*//' | sort -u | paste -sd', ' -)"
  if [ -z "$named" ] && grep -q '^FAIL' <<<"$out"; then
    echo "INCONCLUSIVE — failed without naming a test"
    return
  fi
  echo "$named"
}

run_web() {
  local out named
  # The project's own command. An invocation of vitest that differs from it —
  # a different root, a different config resolution — can fail a test that has
  # nothing to do with the mutation, and that failure would appear in every row
  # and make a mutation nothing catches look caught.
  #
  # Bounded, because a mutation can hang a render as easily as a handler, and an
  # unbounded run would stall the whole table rather than report the hang.
  out="$(timeout 300 npm --prefix web test 2>&1)"
  local code=$?
  if [ "$code" -eq 124 ]; then
    echo "INCONCLUSIVE — web suite timed out"
    return
  fi
  if grep -q 'error TS[0-9]\|Transform failed\|Build failed' <<<"$out"; then
    echo "INCONCLUSIVE — did not compile"
    return
  fi
  named="$(grep -E '^ *× ' <<<"$out" | sed 's/^ *× //;s/ [0-9]*ms$//' | sort -u | paste -sd', ' -)"
  if [ -z "$named" ] && grep -qE 'Tests +[0-9]+ failed' <<<"$out"; then
    echo "INCONCLUSIVE — failed without naming a test"
    return
  fi
  echo "$named"
}

mutate() { # mutate <lang> <name> <file> <needle> <replacement>
  local lang="$1" name="$2" file="$3" needle="$4" replacement="$5"
  case "$name" in
    *"$only"*) ;;
    *) return ;;
  esac
  restore
  if ! apply "$file" "$needle" "$replacement"; then
    report "$name" "MUTATION DID NOT APPLY"
    fail=$((fail + 1))
    restore
    return
  fi
  local failures
  if [ "$lang" = go ]; then failures="$(run_go)"; else failures="$(run_web)"; fi
  restore
  case "$failures" in
    "")
      report "$name" "**NOT DISCRIMINATING — nothing failed**"
      fail=$((fail + 1))
      ;;
    INCONCLUSIVE*)
      # Not survived — not tested. Counting this as a pass is how an untested
      # mutation becomes evidence of coverage.
      report "$name" "**$failures**"
      fail=$((fail + 1))
      ;;
    *)
      report "$name" "$failures"
      pass=$((pass + 1))
      ;;
  esac
}

# A baseline that is not green makes every row meaningless: an unrelated failing
# test "catches" every mutation, and the table reads as total coverage.
echo "checking the unmutated baseline at ${commit:0:7}…" >&2
baseline_go="$(run_go)"
baseline_web=""
if [ "$which" = all ] || [ "$which" = web ]; then baseline_web="$(run_web)"; fi
if [ -n "$baseline_go" ] || [ -n "$baseline_web" ]; then
  echo "the unmutated suite is not green — every row below would be meaningless" >&2
  echo "  go:  ${baseline_go:-clean}" >&2
  echo "  web: ${baseline_web:-clean}" >&2
  exit 2
fi

echo "mutation check against $commit"
echo
echo "| mutation | test that failed |"
echo "| --- | --- |"

if [ "$which" = all ] || [ "$which" = go ]; then
  F=internal/desk/files.go
  S=internal/desk/server.go

  mutate go "lexical path guard: the project itself allowed" "$F" \
    '	if clean == "." {' \
    '	if false {'
  mutate go "lexical path guard: fs.ValidPath not consulted" "$F" \
    '	if !fs.ValidPath(clean) {' \
    '	if false {'
  mutate go "lexical path guard: backslash allowed" "$F" \
    "$(printf '\tif strings.ContainsRune(rel, %s) {' "'\\\\'")" \
    '	if false {'
  mutate go "lexical path guard: NUL allowed" "$F" \
    '	if strings.ContainsRune(rel, 0) {' \
    '	if false {'
  mutate go "lexical path guard: absolute allowed" "$F" \
    '	if path.IsAbs(rel) || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {' \
    '	if false {'
  mutate go "lexical layer never called" "$F" \
    '	clean, err := wireRelativePath(r.URL.Query().Get("path"))' \
    '	clean, err := path.Clean(r.URL.Query().Get("path")), error(nil)'
  mutate go "staging files are editable documents" "$F" \
    '	if strings.HasPrefix(path.Base(clean), stagingPrefix) {' \
    '	if false {'
  mutate go "read does not require a regular file" "$F" \
    '	if !info.Mode().IsRegular() {' \
    '	if false {'
  # Deliberately not mutated: replacing the LimitReader with a plain ReadAll
  # produces the same 413 for the same file, because the size verdict is taken
  # after the read either way. What the LimitReader changes is peak memory on a
  # file that grows between the stat and the read, and no test here can observe
  # that. It is kept because it is correct, and it is named here so that its
  # absence from this table is a statement rather than an oversight.
  mutate go "FIFO blocks the open (no O_NONBLOCK)" "$F" \
    '	f, err := s.root.OpenFile(osPath(clean), os.O_RDONLY|openNonBlocking, 0)' \
    '	f, err := s.root.OpenFile(osPath(clean), os.O_RDONLY, 0)'
  mutate go "a refused read is treated as a missing file" "$F" \
    '	if readErr != nil && readStatus != http.StatusNotFound {' \
    '	if false {'
  mutate go "the stale-digest check is skipped" "$F" \
    '	if !req.Override && !strings.EqualFold(strings.TrimSpace(req.BaseSHA256), actual) {' \
    '	if false {'
  mutate go "override is ignored" "$F" \
    '	if !req.Override && !strings.EqualFold' \
    '	if !false && !strings.EqualFold'
  mutate go "the compare-and-commit is not serialized" "$F" \
    '	s.writes.Lock()
	defer s.writes.Unlock()' \
    ''
  mutate go "the write path drops the symlink refusal" "$F" \
    '	if err := s.refuseSymlinkedPath(clean); err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	// What is there now, under the lock.' \
    '	// What is there now, under the lock.'
  mutate go "excluded directories are not endpoint exclusions" "$F" \
    '	for _, part := range strings.Split(clean, "/") {
		if skipDirs[part] {' \
    '	for _, part := range strings.Split(clean, "/") {
		if false && skipDirs[part] {'
  mutate go "the listing hides that it is partial" "$F" \
    '	if len(problems) > 0 {' \
    '	if false {'
  mutate go "the watcher reports success with no watches" internal/desk/watch.go \
    '	if watched == 0 {' \
    '	if watched < 0 {'
  mutate go "the runtime starts from the unresolved pathname" internal/desk/relay.go \
    'func (s *Server) runtimeWorkingDir() string { return s.projectDir }' \
    'func (s *Server) runtimeWorkingDir() string { return s.cfg.ProjectDir }'
  mutate go "Origin accepts empty delimiters" "$S" \
    '		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		strings.ContainsRune(origin, '"'"'#'"'"') {' \
    '		u.Path != "" || u.RawQuery != "" {'
  mutate go "the save is a direct write, not a replace" "$F" \
    '	dir := path.Dir(clean)' \
    '	if true {
		g, ferr := s.root.OpenFile(osPath(clean), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if ferr != nil {
			return ferr
		}
		defer g.Close()
		_, werr := g.Write(data)
		return werr
	}
	dir := path.Dir(clean)'
  mutate go "the file mode is not preserved" "$F" \
    '	if info, serr := s.root.Stat(osPath(clean)); serr == nil {' \
    '	if info, serr := s.root.Stat(osPath(clean)); false && serr == nil {'
  mutate go "the missing directory is a containment failure" "$F" \
    '	if parent := path.Dir(clean); parent != "." {' \
    '	if parent := path.Dir(clean); false && parent != "." {'
  mutate go "the guard drops the token check" "$F" \
    '	if !s.authorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid session token")
		return false
	}' \
    ''
  mutate go "the guard drops the origin check" "$F" \
    '	if !s.originAllowed(r) {
		writeJSONError(w, http.StatusForbidden,
			fmt.Sprintf("origin %q is not permitted", r.Header.Get("Origin")))
		return false
	}' \
    ''
  mutate go "staging files are listed" "$F" \
    '		if strings.HasPrefix(path.Base(rel), stagingPrefix) {
			return
		}' \
    ''
  mutate go "stale staging files are not cleared at startup" "$S" \
    '	s.removeStaleStaging()' \
    ''
  mutate go "origin matching ignores the scheme" "$S" \
    '	if !strings.EqualFold(u.Scheme, requestScheme(r)) {
		return false
	}' \
    ''
  mutate go "origin matching accepts extra URL components" "$S" \
    '	if u.Scheme == "" || u.Host == "" || u.Opaque != "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		strings.ContainsRune(origin, '"'"'#'"'"') {
		return false
	}' \
    '	if u.Scheme == "" || u.Host == "" {
		return false
	}'
  mutate go "the listing classifies by pathname, not through the root" "$F" \
    '			info, lerr := s.root.Lstat(osPath(child))' \
    '			info, lerr := os.Lstat(filepath.Join(s.cfg.ProjectDir, osPath(child)))'
  mutate go "the project root is re-resolved per request" "$F" \
    '	f, err := s.root.OpenFile(osPath(clean), os.O_RDONLY|openNonBlocking, 0)' \
    '	f, err := os.OpenFile(filepath.Join(s.cfg.ProjectDir, osPath(clean)), os.O_RDONLY|openNonBlocking, 0)'
fi

if [ "$which" = all ] || [ "$which" = web ]; then
  A=web/src/routes/AuthorView.tsx
  C=web/src/files/client.ts

  mutate web "dirty means 'something was typed'" "$A" \
    '    () => buffer !== undefined && base !== undefined && buffer !== base.content,' \
    '    () => buffer !== undefined && base !== undefined,'
  mutate web "the base rebases onto background refetches" "$A" \
    '  const [base, setBase] = useState<FileContent | undefined>(undefined)' \
    '  const [baseIgnored, setBase] = useState<FileContent | undefined>(undefined); void baseIgnored; const base = loaded.data'
  mutate web "the write carries the wrong base digest" "$A" \
    '      { path, content: submitted, baseSha256: base.sha256, override },' \
    '      { path, content: submitted, baseSha256: "", override },'
  mutate web "the read-back is assumed, not verified" "$A" \
    '  const verified = outcome !== undefined && outcome.landed.content === outcome.submitted' \
    '  const verified = outcome !== undefined'
  mutate web "verification compares against the live buffer" "$A" \
    '  const verified = outcome !== undefined && outcome.landed.content === outcome.submitted' \
    '  const verified = outcome !== undefined && outcome.landed.content === buffer'
  mutate web "a deleted file unmounts the editor" "$A" \
    '          {selected ? (' \
    '          {selected && listedNow ? ('
  mutate web "a failed listing refresh unmounts the editor" "$A" \
    '      {listing.error && !listing.data ? (' \
    '      {listing.error ? ('
  mutate web "a failed reload installs stale cached bytes" "$A" \
    '      if (result.isSuccess && result.data) {' \
    '      if (result.data) {'
  mutate web "in-app navigation is not blocked" "$A" \
    '  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    dirty && currentLocation.pathname !== nextLocation.pathname
  )' \
    '  const blocker = useBlocker(() => false)'
  mutate web "the caches are left disagreeing with the read-back" "$A" \
    "$(printf '          queryClient.setQueryData([%sdesk-file%s, path], landed)' "'" "'")" \
    '          void landed'
  mutate web "a previous verdict survives the next save" "$A" \
    '    setOutcome(undefined)
    // The snapshot is captured here, with the request.' \
    '    // The snapshot is captured here, with the request.'
  mutate web "discard leaves the conflict standing" "$A" \
    '              setBuffer(base.content)
              setOutcome(undefined)
              write.reset()' \
    '              setBuffer(base.content)'
  mutate web "switching files does not ask about unsaved work" "$A" \
    '    if (
      dirty &&
      !window.confirm(' \
    '    if (
      false &&
      !window.confirm('
  mutate web "reload is available during an in-flight write" "$A" \
    '            disabled={write.isPending}
            onClick={reload}' \
    '            disabled={false}
            onClick={reload}'
  mutate web "override is always sent" "$C" \
    '      override: input.override ?? false' \
    '      override: true'
  mutate web "a 409 is read as an ordinary error" "$C" \
    '  if (response.status === 409 && body !== undefined) {
    throw new StaleWrite(body as ConstructorParameters<typeof StaleWrite>[0])
  }' \
    ''
  mutate web "deleted and changed are no longer distinguished" "$A" \
    "        {stale.exists
          ? 'Something else wrote to it while this edit was open.'
          : 'The file is no longer on disk — something else deleted or moved it.'}{' '}" \
    "        {'Something else wrote to it while this edit was open.'}{' '}"
fi

restore
echo
echo "discriminating: $pass    not discriminating: $fail"
[ "$fail" -eq 0 ]
