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

case "$which" in
  all|go|web) ;;
  *) echo "usage: $0 [all|go|web] [row-name-substring]" >&2; exit 2 ;;
esac

pass=0
fail=0
matched=0

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
  local out code named
  out="$(go test ./internal/desk -count=1 -timeout 45s 2>&1)"
  code=$?
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
  # **The exit status is the last word, and it was not consulted at all.**
  # Every branch above reads the *output*, so a failure whose output does not
  # match one of those patterns — `go` not on PATH, a package that will not
  # load, a suite with no tests in it — was reported as the empty string, which
  # this table reads as "the mutation survived". A run that ended nonzero and
  # named no test did not survive anything; it did not run.
  if [ -z "$named" ] && [ "$code" -ne 0 ]; then
    echo "INCONCLUSIVE — go test exited $code naming no failing test"
    return
  fi
  if [ -z "$named" ] && ! grep -qE '^(ok|---|PASS)' <<<"$out"; then
    echo "INCONCLUSIVE — go test reported no result at all"
    return
  fi
  # A package that ran no tests exits zero and prints `ok`. For this package
  # that is not a pass: the suite is what the mutation is being measured
  # against, and a build that compiled the tests away would otherwise read as
  # "nothing failed".
  if grep -q 'no tests to run' <<<"$out"; then
    echo "INCONCLUSIVE — go test ran no tests"
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
  # **Nonzero and naming nothing is not survival.** The status was captured and
  # then only ever compared to 124: a suite that failed to start, a `vitest`
  # that is not installed, a config that will not load — each exits nonzero,
  # prints nothing this function recognises, and was reported as the empty
  # string, which the table reads as "nothing failed". A 41-file startup
  # failure was accepted as a clean baseline that way.
  if [ -z "$named" ] && [ "$code" -ne 0 ]; then
    echo "INCONCLUSIVE — the web suite exited $code naming no failing test"
    return
  fi
  # And a run that passed *no* tests is not a run either. `Tests  no tests`
  # is what a filter that matches nothing prints, and it exits zero.
  if [ -z "$named" ] && ! grep -qE 'Tests +[0-9]+ passed' <<<"$out"; then
    echo "INCONCLUSIVE — the web suite ran no tests"
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
  matched=$((matched + 1))
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
# Each half's baseline is taken only where that half's rows will run. `web`
# promised web-only and spent forty seconds in `go test` before every run,
# and a Go toolchain that is not installed failed a mode that needs none.
baseline_go=""
if [ "$which" = all ] || [ "$which" = go ]; then baseline_go="$(run_go)"; fi
baseline_web=""
if [ "$which" = all ] || [ "$which" = web ]; then baseline_web="$(run_web)"; fi
if [ -n "$baseline_go" ] || [ -n "$baseline_web" ]; then
  echo "the unmutated suite is not green — every row below would be meaningless" >&2
  echo "  go:  ${baseline_go:-clean}" >&2
  echo "  web: ${baseline_web:-clean}" >&2
  # **And say so on stdout, in the table's own shape.** This used to write only
  # to stderr and exit, so a caller running one row at a time and collecting
  # rows with `grep '^|'` recorded a *silent gap*: the row it asked for produced
  # no line at all, which reads exactly like a row nobody ran on purpose. A run
  # that skipped its rows must be visible in the table it did not fill in.
  echo "| mutation | test that failed |"
  echo "| --- | --- |"
  report "${only:-every row}" "**BASELINE NOT GREEN — not run**"
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
  # Repaired: the needle named `defer s.writes.Unlock()`, which this file has
  # never carried — the lock is released explicitly before the response is
  # encoded, which is what the row below it is about. Broken at `origin/main`
  # too, so every run of this half reported one row that mutated nothing.
  mutate go "the compare-and-commit is not serialized" "$F" \
    '	s.writes.Lock()
	status, body := s.commitWriteLocked(clean, req)
	s.writes.Unlock()' \
    '	status, body := s.commitWriteLocked(clean, req)'
  # Repaired: the refusal moved inside `commitWriteLocked` and answers with a
  # body rather than writing to the ResponseWriter, so the needle named a
  # handler shape that no longer exists. Broken at `origin/main` too.
  mutate go "the write path drops the symlink refusal" "$F" \
    '	if err := s.refuseSymlinkedPath(clean); err != nil {
		return statusForRefusal(err), errorBody(err)
	}' \
    ''
  # Repaired: the test is `isExcludedName(part)`, not a `skipDirs` map lookup.
  # Broken at `origin/main` too.
  mutate go "excluded directories are not endpoint exclusions" "$F" \
    '	for _, part := range strings.Split(clean, "/") {
		if isExcludedName(part) {' \
    '	for _, part := range strings.Split(clean, "/") {
		if false && isExcludedName(part) {'
  mutate go "the listing hides that it is partial" "$F" \
    '	if len(problems) > 0 {' \
    '	if false {'
  mutate go "the watcher reports success with no watches" internal/desk/watch.go \
    '	if watched == 0 {' \
    '	if watched < 0 {'
  mutate go "the runtime starts from the unresolved pathname" internal/desk/relay.go \
    'func (s *Server) runtimeWorkingDir() string { return s.projectDir }' \
    'func (s *Server) runtimeWorkingDir() string { return s.cfg.ProjectDir }'
  mutate go "the walk does not detect a repeated ancestor" "$F" \
    '		if os.SameFile(ancestor, info) {' \
    '		if false && os.SameFile(ancestor, info) {'
  mutate go "the walk has no entry budget" "$F" \
    '			if budget <= 0 {' \
    '			if false {'
  mutate go "a permission error is called a containment failure" "$F" \
    '		if errors.Is(err, fs.ErrPermission) {' \
    '		if false {'
  mutate go "an unreadable file gets the oversized shape" "$F" \
    '		case status == http.StatusRequestEntityTooLarge:' \
    '		case status != 0:'
  mutate go "excluded names match case-sensitively" "$F" \
    '		if strings.EqualFold(name, excluded) {' \
    '		if name == excluded {'
  mutate go "a reserved-name regular file is listed" "$F" \
    '				// A regular file bearing an excluded directory'"'"'s name is omitted
				// too: GET and PUT refuse that path, and listing something the
				// endpoints will not open is an offer the API does not honour.
				if isExcludedName(name) {
					continue
				}' \
    ''
  mutate go "the write mutex spans the response encoding" "$F" \
    '	status, body := s.commitWriteLocked(clean, req)
	s.writes.Unlock()
	writeJSON(w, status, body)' \
    '	status, body := s.commitWriteLocked(clean, req)
	writeJSON(w, status, body)
	s.writes.Unlock()'
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
		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"missing or invalid session token")
		return false
	}' \
    ''
  mutate go "the guard drops the origin check" "$F" \
    '	if !s.originAllowed(r) {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden,
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
  # Repaired: the variable is `childInfo`, not `info`. Broken at `origin/main`
  # too — a one-word drift that made the row silently inoperable. The mutant
  # also names `s.cfg.ProjectDir`, the field as configured, rather than
  # `s.projectDir` — which is that path with its symlinks already resolved, so
  # a mutant naming it would be the original spelled differently.
  mutate go "the listing classifies by pathname, not through the root" "$F" \
    '			childInfo, lerr := s.root.Lstat(osPath(child))' \
    '			childInfo, lerr := os.Lstat(filepath.Join(s.cfg.ProjectDir, osPath(child)))'
  mutate go "the project root is re-resolved per request" "$F" \
    '	f, err := s.root.OpenFile(osPath(clean), os.O_RDONLY|openNonBlocking, 0)' \
    '	f, err := os.OpenFile(filepath.Join(s.cfg.ProjectDir, osPath(clean)), os.O_RDONLY|openNonBlocking, 0)'

  # createParents. The opt-in, its containment, and its position in the order.
  mutate go "createParents is not opt-in (every write creates its parents)" "$F" \
    '		case !req.CreateParents:' \
    '		case false:'
  mutate go "createParents resolves a pathname instead of the pinned root" "$F" \
    '			if err := s.root.MkdirAll(osPath(parent), 0o777); err != nil {' \
    '			if err := os.MkdirAll(filepath.Join(s.cfg.ProjectDir, osPath(parent)), 0o777); err != nil {'
  mutate go "parents are created before the stale-digest check" "$F" \
    '	if !req.Override && !strings.EqualFold(strings.TrimSpace(req.BaseSHA256), actual) {' \
    '	if parent := path.Dir(clean); parent != "." && req.CreateParents {
		_ = s.root.MkdirAll(osPath(parent), 0o777)
	}
	if !req.Override && !strings.EqualFold(strings.TrimSpace(req.BaseSHA256), actual) {'
  # Verification round: the bound, and the mode a create publishes.
  mutate go "createParents is not bounded by what the listing can walk" "$F" \
    '		if strings.Count(parent, "/")+1 > maxWalkDepth {' \
    '		if false {'
  mutate go "a created file keeps the staging mode instead of taking the umask" "$F" \
    '		f, err := s.root.OpenFile(osPath(full), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)' \
    '		f, err := s.root.OpenFile(osPath(full), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)'

  # Deliberately not mutated: "a parent that is a regular file is created over",
  # which would be `case derr == nil || !req.CreateParents:` -> `case
  # !req.CreateParents:`. That branch is unreachable through the handler and was
  # before this change too: opening `packs/x` where `packs` is a regular file is
  # ENOTDIR, and `readThroughRoot` maps everything that is neither ErrNotExist
  # nor ErrPermission onto the one containment refusal — so the current-bytes
  # read answers 403 first. The row would report NOT DISCRIMINATING for a
  # safeguard that is real but held one layer up, which is a worse statement
  # than this note. `TestCreateParentsRefusesAParentThatIsAFile` pins the
  # behaviour, with and without the member.

  # ---- Codex round 1 on the Create redesign ------------------------------

  # 8. The bound used to sit inside the create-the-parent branch, so a write
  # into 65 directories that already existed went through. This puts it back
  # there rather than removing it — the row above already breaks it outright,
  # and what this one is about is *where* it runs.
  mutate go "the depth bound skips a parent that already exists" "$F" \
    '		if strings.Count(parent, "/")+1 > maxWalkDepth {
			return http.StatusBadRequest, codedBody(CodeTooDeep, fmt.Sprintf(
				"this API writes at most %d levels of directory deep", maxWalkDepth))
		}
		info, derr := s.root.Stat(osPath(parent))' \
    '		info, derr := s.root.Stat(osPath(parent))
		if _, missing := s.root.Stat(osPath(parent)); missing != nil {
			if strings.Count(parent, "/")+1 > maxWalkDepth {
				return http.StatusBadRequest, codedBody(CodeTooDeep, fmt.Sprintf(
					"this API writes at most %d levels of directory deep", maxWalkDepth))
			}
		}'

  # 12. A code is what a client decides on; a sentence is what a person reads.
  mutate go "a refusal carries no code for a client to act on" "$F" \
    '	return map[string]string{"error": err.Error(), "code": codeOf(err)}' \
    '	return map[string]string{"error": err.Error()}'
  mutate go "ENOTDIR is reported as a containment failure again" "$F" \
    '			if errors.Is(err, syscall.ENOTDIR) {
				return withCode(CodeParentIsAFile, fmt.Errorf(
					"a component of %s is a file, not a directory", clean))
			}' \
    ''
  mutate go "a create that found something is called stale" "$F" \
    '		code := CodeStale
		if strings.TrimSpace(req.BaseSHA256) == "" && exists {
			code = CodeExists
		}' \
    '		code := CodeStale'

  # ---- Codex round 2 -----------------------------------------------------

  # A staging or excluded-directory refusal used to carry no code at all and
  # arrive at the client as `internal`, which says "a bug here" about a name
  # the desk simply reserves.
  mutate go "a staging-file refusal carries no code" "$F" \
    '		return "", withCode(CodeStagingFile, fmt.Errorf(
			"%s is a staging file this desk owns, not a document", clean))' \
    '		return "", fmt.Errorf("%s is a staging file this desk owns, not a document", clean)'
  mutate go "an excluded-directory refusal carries no code" "$F" \
    '			return "", withCode(CodeExcludedDirectory, fmt.Errorf(
				"%s is under %s, which this desk does not edit", clean, part))' \
    '			return "", fmt.Errorf("%s is under %s, which this desk does not edit", clean, part)'
  # The status used to be a literal at each call site, so `path is required`
  # carried `bad-request` and answered 403.
  mutate go "the status is not the one the code names" "$F" \
    '	if status, ok := codeStatus[codeOf(err)]; ok {
		return status
	}
	return http.StatusForbidden' \
    '	return http.StatusForbidden'
  mutate go "a malformed request is answered as a containment failure" "$F" \
    '		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	afterResolve(clean)' \
    '		writeJSONError(w, http.StatusForbidden, err)
		return
	}
	afterResolve(clean)'
  # One code answering both 401 and 403 is not a matrix.
  mutate go "the token and the origin share one code again" "$F" \
    '		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"missing or invalid session token")' \
    '		writeJSONCoded(w, http.StatusUnauthorized, CodeForbidden,
			"missing or invalid session token")'
fi
if [ "$which" = all ] || [ "$which" = web ]; then
  A=web/src/routes/AuthorView.tsx
  C=web/src/files/client.ts
  L=web/src/shell/LeftRail.tsx
  K=web/src/shell/shortcuts.ts
  P=web/src/shell/paneState.ts
  S=web/src/shell/StatusStrip.tsx
  R=web/src/shell/RightPane.tsx
  M=web/src/mcp/McpProvider.tsx
  D=web/src/config/deskConfig.ts
  H=web/src/shell/HeaderBar.tsx
  U=web/src/identity/UserControl.tsx
  N=web/src/shell/AppShell.tsx
  X=web/src/shell/CreatePackDialog.tsx
  Y=web/src/mcp/capabilities.ts
  W=web/src/config/DeskConfigProvider.tsx
  V=web/src/routes/AdminView.tsx
  Q=web/src/shell/useHashTarget.ts
  B=web/src/shell/authorBridge.ts
  I=web/src/identity/IdentityProvider.tsx
  E=web/src/shell/RightPane.tsx
  G=web/src/shell.css
  T=web/src/shell/BottomPane.tsx
  J=web/src/mcp/queries.ts
  Z=web/src/config/queries.ts
  O=web/src/shell/measured.ts
  JC=web/src/packs/jpackConfig.ts
  NP=web/src/packs/newPack.ts
  UI=web/src/ui/Button.tsx
  FD=web/src/ui/Field.tsx
  SEL=web/src/ui/Select.tsx
  DG=web/src/ui/Dialog.tsx
  AL=web/src/ui/Alert.tsx
  SELCSS=web/src/ui/Select.module.css
  PP=web/src/packs/packPath.ts
  CR=web/src/packs/createRefusal.ts
  DL=web/src/ui/declarations.ts
  UD=web/src/ui/Dialog.tsx
  LR=web/src/shell/LeftRail.tsx
  FC=web/src/files/client.ts
  SEL=web/src/ui/Select.tsx
  ST=web/src/mcp/starters.ts
  DT=web/src/packs/documentText.ts
  CK=web/src/packs/checks.ts
  QR=web/src/mcp/queries.ts
  CAP=web/src/mcp/capabilities.ts
  OM=web/src/packs/document/OmittedMember.tsx
  MB=web/src/packs/document/members.ts
  CT=web/src/packs/document/ConditionTree.tsx
  RP=web/src/shell/RightPane.tsx
  WR=web/src/packs/useWindowedRows.ts
  RL=web/src/shell/LeftRail.tsx
  CV=web/src/ui/convention.test.ts
  PPC=web/src/packs/PacksPane.module.css
  RF=web/src/packs/references.ts
  IS=web/src/shell/InspectorSlot.tsx
  SPY=web/src/packs/useDocumentSpy.ts
  AB=web/src/packs/document/ApplicabilityBlock.tsx
  CTAB=web/src/packs/inspector/ChecksTab.tsx
  PV=web/src/routes/PackView.tsx
  BK=web/src/packs/document/Block.tsx
  PDV=web/src/packs/document/PackDocumentView.tsx
  MO=web/src/packs/document/MemberOutline.tsx
  PN=web/src/packs/PacksPane.tsx
  PT=web/src/packs/pointers.ts
  RTAB=web/src/packs/inspector/ReferencesTab.tsx
  CTB=web/src/packs/inspector/ChecksTab.tsx
  AS=web/src/shell/AppShell.tsx
  FX=web/src/packs/__fixtures__/full.pack.json
  CS=web/src/packs/CheckStrip.tsx
  FE=web/src/files/useFileEditing.ts
  DGD=web/src/shell/useDirtyGuard.ts

  # The rail's graph gate. `useConfiguredGraphs` falls back to a whole-project
  # `experimental_test_graphs` walk against any runtime without the inventory
  # tool, and in the rail that would fire on every route. Aliased at the import
  # so the mutation is one line and still compiles.
  mutate web "rail calls the whole-project graph walk" "$L" \
    "import { useGraphInventory, usePacks } from '../mcp/queries'" \
    "import { useConfiguredGraphs as useGraphInventory, usePacks } from '../mcp/queries'"
  mutate web "shortcuts fire inside the editor" "$K" \
    "  if (element.tagName === 'INPUT' || element.tagName === 'TEXTAREA') return true
  if (element.isContentEditable === true) return true
  return Boolean(element.closest?.('[contenteditable]:not([contenteditable=\"false\"])'))" \
    "  return false"
  mutate web "pane state is not persisted" "$P" \
    '      writeShellState(storageKey, state, chosen)' \
    '      void state'
  # The other half of the same effect: a record nobody chose must not be
  # written, because the seed prefers a stored record over the configured one —
  # so a shell that persisted its own defaults would shadow the file for ever.
  mutate web "an unchosen layout is persisted anyway" "$P" \
    '      if (!chosen.left && !chosen.inspector && !chosen.console) return
      writeShellState(storageKey, state, chosen)' \
    '      writeShellState(storageKey, state, { left: true, inspector: true, console: true })'
  mutate web "a throwing localStorage takes the shell down" "$P" \
    '  let raw: string | null = null
  try {
    raw = window.localStorage.getItem(key)
  } catch {
    return undefined
  }' \
    '  const raw: string | null = window.localStorage.getItem(key)'
  mutate web "the reset clears more than one key" "$P" \
    '    window.localStorage.removeItem(key)' \
    '    window.localStorage.clear()'
  mutate web "a record from another shell version is restored anyway" "$P" \
    '  if (record.v !== RECORD_VERSION) return undefined' \
    '  if (false) return undefined'
  mutate web "the configured pane default never arrives" "$P" \
    '    if (seededFrom.current === signature) return' \
    '    if (true) return'
  mutate web "re-seeding overrides a layout the viewer chose" "$P" \
    '  const toggleConsole = useCallback(() => {
    touched.current.console = true' \
    '  const toggleConsole = useCallback(() => {'
  mutate web "the restored layout is not clamped to the viewport" "$P" \
    '    left: { mode: viewport.railIsDrawer ? '"'"'icons'"'"' : merged.left.mode },
    inspector: { open: viewport.inspectorIsDrawer ? false : merged.inspector.open },' \
    '    left: merged.left,
    inspector: merged.inspector,'
  mutate web "the strip stops naming the runtime" "$S" \
    '            connected to <code>{server.name}</code> {server.version}' \
    '            connected to <code>{server.name}</code>'
  # A refused configuration is the built-in defaults — correct, and until this
  # cue it was indistinguishable from having no configuration file at all
  # anywhere except /admin.
  mutate web "a refused configuration is silent outside Admin" "$S" \
    '        {problems.length > 0 && (' \
    '        {false && ('
  mutate web "main remounts on a pane change" "$N" \
    '      <main id="main" tabIndex={-1} className="desk-main">' \
    '      <main id="main" key={String(shell.console.open)} tabIndex={-1} className="desk-main">'
  mutate web "the closed inspector stays tabbable" "$R" \
    '      id="desk-inspector"
      hidden={!open}
    >' \
    '      id="desk-inspector"
    >'
  mutate web "the file channel is fed by nothing" "$M" \
    "        recordFileChange(String((notification.params as { path?: unknown })?.path ?? ''))" \
    '        void recordFileChange'
  mutate web "a config problem no longer refuses the whole file" "$D" \
    '  if (problems.length > 0) return { values: undefined, problems, declaredPanes }' \
    '  if (false) return { values: undefined, problems, declaredPanes }'
  mutate web "an unknown config key is accepted silently" "$D" \
    "    problems.push({ key, reason: 'unknown key' })" \
    '    void key'
  mutate web "identity may be configured in the shared project file" "$D" \
    "const PROJECT_KEYS: readonly string[] = COMMON_KEYS" \
    "const PROJECT_KEYS: readonly string[] = [...COMMON_KEYS, 'identity']"
  mutate web "the provider object admits a discriminator" "$D" \
    "    ['label', 'issuer', 'clientId', 'scopes', 'audience', 'claims', 'showRemoteAvatar', 'signOut']," \
    "    ['label', 'issuer', 'clientId', 'scopes', 'audience', 'claims', 'showRemoteAvatar', 'signOut', 'kind', 'mode', 'operator', 'vendor', 'clientSecret'],"
  mutate web "the header invents an organization name" "$H" \
    "  const name = config.organization.name ?? DESK_FALLBACK_NAME" \
    "  const name = config.organization.name ?? 'Acme Co.'"
  mutate web "the NONE menu offers a Sign out" "$U" \
    "          <DropdownMenu.Item asChild className=\"desk-menu-item\">
            <Link to=\"/help\">About</Link>
          </DropdownMenu.Item>" \
    "          <DropdownMenu.Item asChild className=\"desk-menu-item\">
            <Link to=\"/help\">About</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item className=\"desk-menu-item\">Sign out</DropdownMenu.Item>"
  # Below 900px the rail is a Dialog drawer and renders no collapse toggle, so
  # the header's opener is the only pointer affordance there is. Without it the
  # whole left menu is reachable by Mod+B alone, on the width whose likeliest
  # device has no keyboard.
  mutate web "the rail drawer has no opener" "$H" \
    '        {railIsDrawer && (' \
    '        {false && ('
  mutate web "the rail drawer carries no landmark" "$L" \
    '            <nav aria-label="Project">
              <RailBody
                mode="expanded"
                onToggle={onToggle}
                showCollapse={false}
                onNavigate={() => onDrawerOpenChange(false)}
              />
            </nav>' \
    '            <RailBody
              mode="expanded"
              onToggle={onToggle}
              showCollapse={false}
              onNavigate={() => onDrawerOpenChange(false)}
            />'
  mutate web "the inspector drawer answers to no id" "$R" \
    '            id="desk-inspector"
            aria-label="Inspector"' \
    '            aria-label="Inspector"'
  mutate web "the brand leaves the router on every click" "$H" \
    '        <Link className="desk-brand" to="/">' \
    '        <Link className="desk-brand" to="/" reloadDocument>'
  mutate web "an empty organization name is accepted" "$D" \
    "      const name = organizationName(organization.name, problems)" \
    "      const name = optionalString(organization.name, 'organization.name', problems)"
  mutate web "the identity slot admits a discriminator one level up" "$D" \
    "    const identity = section(record.identity, 'identity', ['provider'], problems)" \
    "    const identity = section(record.identity, 'identity', ['provider', 'kind', 'mode', 'operator', 'vendor', 'clientSecret'], problems)"
  mutate web "the control states a session verdict it never checked" "$U" \
    "  const name = provider === null ? displayName : (provider.label ?? provider.issuerHost)" \
    "  const name = provider === null ? displayName : (provider.label ?? 'signed out')"
  mutate web "the configured theme is decoded and never applied" "$W" \
    '  useAppliedTheme(value.config.appearance.theme)' \
    '  void value.config.appearance.theme'
  mutate web "the copy button reports a copy it did not make" "$V" \
    '          const written = navigator.clipboard?.writeText(text)
          if (!written) {
            setCopied(false)
            return
          }
          written.then(
            () => setCopied(true),
            () => setCopied(false)
          )' \
    '          void navigator.clipboard?.writeText(text)
          setCopied(true)'
  mutate web "the section links go nowhere" "$Q" \
    '    target?.scrollIntoView()' \
    '    void target'
  mutate web "the create dialog is mounted on every route" "$L" \
    '      {creating && (
        <CreatePackDialog
          open' \
    '      {true && (
        <CreatePackDialog
          open={creating}'
  # `navigate('/author')` from `/author` matches the same element, so a
  # mount-only take never runs again: the editor stayed where it was and the
  # request was left in module state for an unrelated mount to consume.
  mutate web "create leaves its request behind when the editor is already open" "$B" \
    '  }, [requestedOpen])' \
    '  }, [])'
  mutate web "the example capability is read from one tool" "$Y" \
    "    exampleSupported: names.has('list_examples') && names.has('get_example')," \
    "    exampleSupported: names.has('get_example'),"

  mutate web "dirty means 'something was typed'" "$A" \
    '    () => buffer !== undefined && base !== undefined && buffer !== base.content,' \
    '    () => buffer !== undefined && base !== undefined,'
  mutate web "the base rebases onto background refetches" "$A" \
    '  const [base, setBase] = useState<FileContent | undefined>(undefined)' \
    '  const [baseIgnored, setBase] = useState<FileContent | undefined>(undefined); void baseIgnored; const base = loaded.data'
  # **These five moved with the code they are about.** The save discipline was
  # lifted out of `AuthorView.FileEditor` into `files/useFileEditing.ts` so a
  # second editor could hold the same rules rather than a second spelling of
  # them; each row now breaks the one copy, and `AuthorView.test.tsx` failing
  # alongside the new suites is what says the extraction kept its behaviour.
  mutate web "the write carries the wrong base digest" "$FE" \
    '          baseSha256: input.baseSha256,' \
    "          baseSha256: '',"
  mutate web "the read-back is assumed, not verified" "$FE" \
    '  const verified = outcome !== undefined && outcome.landed.content === outcome.submitted' \
    '  const verified = outcome !== undefined'
  mutate web "verification compares against the live buffer" "$FE" \
    '            setOutcome({ submitted, landed })' \
    '            setOutcome({ submitted: landed.content, landed })'
  mutate web "a deleted file unmounts the editor" "$A" \
    '          {selected ? (' \
    '          {selected && listedNow ? ('
  mutate web "a failed listing refresh unmounts the editor" "$A" \
    '      {listing.error && !listing.data ? (' \
    '      {listing.error ? ('
  # REMOVED: "a failed reload installs stale cached bytes". Broken at main
  # (441a99c) and broken here — its needle, `if (result.isSuccess && result.data)`,
  # appears nowhere in AuthorView.tsx and did not before this branch either, so
  # the row could only ever report MUTATION DID NOT APPLY. It is deleted rather
  # than rewritten to something that happens to fail: the claim it names is
  # already held, exactly and by name, by "reload trusts refetch rather than its
  # own read" below — which breaks the direct `readFile(path)` into a
  # `refetch()` and is caught by "reloads from its own request, not from
  # whatever the cache holds". A row whose claim is covered twice, once
  # inoperably, is one row.
  mutate web "in-app navigation is not blocked" "$DGD" \
    '  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirty && currentLocation.pathname !== nextLocation.pathname
  )' \
    '  const blocker = useBlocker(() => false)'
  mutate web "the caches are left disagreeing with the read-back" "$FE" \
    "$(printf '            queryClient.setQueryData([%sdesk-file%s, input.path], landed)' "'" "'")" \
    '            void landed'
  mutate web "a previous verdict survives the next save" "$FE" \
    '      setOutcome(undefined)
      // Captured here, with the request.' \
    '      // Captured here, with the request.'
  mutate web "discard leaves the conflict standing" "$A" \
    '              setBuffer(base.content)
              editing.reset()' \
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
  mutate web "a partial listing is reported as an empty project" "$A" \
    '            {files.length === 0 && partial.length === 0 ? (' \
    '            {files.length === 0 ? ('
  mutate web "the partial warning is not shown" "$A" \
    '            {partial.length > 0 && (' \
    '            {false && (' 
  mutate web "the save read-back installs over a newer read" "$FE" \
    '            if (state !== undefined && state.dataUpdatedAt > startedAt) return' \
    '            void state'
  # A `refetch` reports success from cache when the watcher's broad
  # `cancelQueries` cancels the request in flight, so its success is not proof
  # that anything was fetched — and installing cached bytes as the new base is
  # a reload that replaces an edit with what it was already showing.
  mutate web "reload trusts refetch rather than its own read" "$FE" \
    '      void readFile(path)
        .then((fresh) => {' \
    "      void Promise.resolve(
        (queryClient.getQueryData(['desk-file', path]) ?? {}) as FileContent
      )
        .then((fresh) => {"
  # Create a pack: the sequence, the entry it writes, and the name it refuses.
  # Repaired: the registration no longer builds an `amended` local — the entry
  # is composed inline on the configuration read in (0b), because that read
  # moved in front of the pack write. The row is the same safeguard against the
  # same defect; only the shape it names moved.
  mutate web "create writes the pack and never registers it" "$X" \
    '        await writeFile({
          path: PROJECT_FILE,
          content: serialiseProjectConfig(
            read.content,
            withPack(current, slug, packEntryFor(landed.path, description))
          ),
          baseSha256: read.sha256
        })' \
    '        void current'
  mutate web "the registration writes a digest it did not read" "$X" \
    '          baseSha256: read.sha256' \
    "          baseSha256: ''"
  mutate web "createParents is not sent" "$X" \
    "        landed = await writeFile({ path, content, baseSha256: '', createParents: true })" \
    "        landed = await writeFile({ path, content, baseSha256: '', createParents: false })"
  mutate web "the pack is written before the project is known to have a jpack.json" "$X" \
    '        read = await readFile(PROJECT_FILE)
        current = parseProjectConfig(read.content)' \
    '        read = { path: PROJECT_FILE, bytes: 0, sha256: '"'"''"'"', content: '"'"'{}'"'"' }
        current = parseProjectConfig(read.content)'
  mutate web "a 409 on jpack.json is reported as an ordinary failure" "$X" \
    "          reason: codeOf(cause) === 'stale' ? STALE_PROJECT_FILE : refusalDetail(cause)" \
    '          reason: refusalDetail(cause)'
  # Repaired, and narrowed: the `role="alert"` half moved into the `Alert`
  # primitive when the dialog stopped rendering bare markup, and it is held
  # there by "the form-level failure is not announced". What is left for this
  # row is the half that is still this file's: that a failure is rendered at
  # all, rather than set in state and shown to nobody.
  mutate web "the create dialog renders no failure at all" "$X" \
    '        {(failure ?? blocked) && (
          <Alert reason={(failure ?? blocked)!.reason}>{(failure ?? blocked)!.lead}</Alert>
        )}' \
    ''
  mutate web "the registration replaces the file rather than amending it" "$JC" \
    '  return { ...config, packs: { ...packs, [slug]: entry } }' \
    '  return { packs: { [slug]: entry } }'
  mutate web "the registration bumps configVersion" "$JC" \
    '  return { ...config, packs: { ...packs, [slug]: entry } }' \
    "  return { ...config, configVersion: '3', packs: { ...packs, [slug]: entry } }"
  mutate web "a blank description is written as an empty string" "$JC" \
    "    ...(trimmed === '' ? {} : { description: trimmed })," \
    '    description: trimmed,'
  mutate web "expectedVersion no longer matches the document's own version" "$JC" \
    '    expectedVersion: NEW_PACK_VERSION' \
    "    expectedVersion: '0.2.0'"
  mutate web "the amended file is reformatted rather than followed" "$JC" \
    '  const encoded = JSON.stringify(config, null, indentOf(source))' \
    '  const encoded = JSON.stringify(config, null, 4)'
  mutate web "the slug is not checked against existing pack keys" "$NP" \
    '  if (project.keys.includes(slug)) {' \
    '  if (false) {'
  mutate web "the slug is not checked against existing files" "$NP" \
    '  if (project.files.some((file) => samePath(file, project.path))) {' \
    '  if (false) {'
  mutate web "a slug that does not begin with a letter is accepted" "$NP" \
    "  if (!/^[a-z]/.test(slug)) return { problem: 'A name must start with a letter.' }" \
    "  if (false) return { problem: 'A name must start with a letter.' }"
  mutate web "the template's specVersion is overwritten" "$NP" \
    '    version: NEW_PACK_VERSION
  }' \
    "    version: NEW_PACK_VERSION,
    specVersion: '0.1.0-desk'
  }"
  mutate web "the empty pack invents outcomes to satisfy minItems" "$NP" \
    "    case 'array':
      return []" \
    "    case 'array':
      return [{ id: 'approve' }, { id: 'decline' }]"
  # The primitives. Each row is one thing a caller cannot see for itself.
  mutate web "the primary button is not a submit (Enter does nothing)" "$UI" \
    "      type={type ?? 'button'}" \
    '      type="button"'
  mutate web "the field's hint is not announced" "$FD" \
    "        'aria-describedby': described.length > 0 ? described.join(' ') : undefined," \
    "        'aria-describedby': undefined,"
  mutate web "the field describes an element it did not render" "$FD" \
    '  const described = [hint ? hintId : undefined, error ? errorId : undefined].filter(Boolean)' \
    '  const described = [hintId, errorId]'
  mutate web "the select reports back a value nobody offered" "$SEL" \
    '        if (offered.has(next)) onValueChange(next)' \
    '        onValueChange(next)'
  # Admin's storage section, and the decoder behind it.
  mutate web "Admin claims a location the listing does not show" "$V" \
    "  if (files.some((file) => file.path.startsWith(\`\${dir}/\`))) return 'holds-files'" \
    "  if (true) return 'holds-files'"
  mutate web "a future storage kind becomes a control" "$V" \
    '<span>database — coming soon</span>' \
    '<input type="radio" disabled readOnly aria-label="database — coming soon" />'
  mutate web "an unknown storage key is accepted" "$D" \
    "          ? section(storage.packs, 'storage.packs', ['kind', 'dir', 'idBase'], problems)" \
    "          ? section(storage.packs, 'storage.packs', ['kind', 'dir', 'idBase', 'bucket'], problems)"
  mutate web "a storage kind other than filesystem is accepted" "$D" \
    "  if (value !== 'filesystem') {" \
    '  if (false) {'
  mutate web "storage defaults are not applied when the member is absent" "$D" \
    '      storage: values?.storage ?? DESK_DEFAULTS.storage' \
    '      storage: values?.storage!'
  mutate web "an escaping pack directory is accepted" "$D" \
    "  if (trimmed.split('/').some((part) => part === '..' || part === '.' || part === '')) {" \
    '  if (false) {'
  # The needle is double-quoted (it carries a `'`), so the backticks and the `$`
  # are escaped: unescaped, bash ran `${trimmed}` as a command substitution,
  # died under `set -u`, and passed a *truncated* needle — which still matched,
  # and produced a tagged template on a string. The row then reported
  # "discriminating" for a TypeError rather than for the missing normalisation.
  mutate web "the id prefix is not normalised, so ids run together" "$D" \
    "  return trimmed.endsWith('/') || trimmed.endsWith('#') ? trimmed : \`\${trimmed}/\`" \
    '  return trimmed'

  mutate web "deleted and changed are no longer distinguished" "$A" \
    "        {stale.exists
          ? 'Something else wrote to it while this edit was open.'
          : 'The file is no longer on disk — something else deleted or moved it.'}{' '}" \
    "        {'Something else wrote to it while this edit was open.'}{' '}"

  # ---- Codex round 1 -----------------------------------------------------
  # One row per safeguard the review's findings put in. Each names the defect
  # it restores rather than the code it edits.

  # 1. The slot. A provider around the Inspector alone is a sibling of the
  # routes, so `useInspectorSlot()` in a route read the closed default for
  # ever and every portal was a no-op.
  mutate web "routes read the Inspector slot's closed default" "$N" \
    '    <InspectorSlotContext.Provider value={slot}>' \
    '    <InspectorSlotContext.Provider value={{ ...slot, target: null }}>'
  mutate web "the pane publishes no portal target" "$E" \
    '      <div ref={publishTarget} className="desk-inspector-slot" />' \
    '      <div className="desk-inspector-slot" />'

  # 2. A grid with an indefinite height grows to fit its content: main never
  # becomes the scroll container and the always-visible strip goes below the
  # fold on a long page.
  mutate web "the frame has no definite height" "$G" \
    '    height: 100vh;
    height: 100dvh;
    overflow: hidden;' \
    '    min-height: 100vh;'
  mutate web "a scrolling pane cannot shrink below its content" "$G" \
    '    scrollbar-gutter: stable;
    min-width: 0;
    min-height: 0;' \
    '    scrollbar-gutter: stable;
    min-width: 0;'

  # 3. The three configured sizes were decoded, shown on Admin as effective,
  # and applied to nothing.
  mutate web "the configured rail width is decoded and never applied" "$N" \
    "    '--rail-w': \`\${config.panes.left.width}px\`," \
    "    '--rail-w': '248px',"
  mutate web "the configured console height is decoded and never applied" "$N" \
    "    '--console-h': \`\${config.panes.console.height}px\`," \
    "    '--console-h': '240px',"
  mutate web "the inspector drawer is a width nobody configured" "$E" \
    "                : ({ '--drawer-w': \`\${declaredWidth}px\` } as CSSProperties)" \
    '                : undefined'

  # 4. One global touched bit made a single toggle speak for all three panes:
  # the other two were serialized from the built-in defaults, and a stored
  # record outranks the configuration file for ever after.
  mutate web "an untouched pane is persisted along with the touched one" "$P" \
    '  const left = touched.left ? state.left : kept.left
  const inspector = touched.inspector ? state.inspector : kept.inspector
  const consoleSection = touched.console ? state.console : kept.console' \
    '  const left = state.left
  const inspector = state.inspector
  const consoleSection = state.console'
  mutate web "one moved pane suppresses the re-seed for every pane" "$P" \
    '    if (chosen.left && chosen.inspector && chosen.console) return' \
    '    if (chosen.left || chosen.inspector || chosen.console) return'

  # 5. The key came from the runtime's `configPath`, which a project with no
  # `jpack.json` does not have — so every configless project on one origin
  # shared the single literal `default` record.
  mutate web "the layout key is not the project the chassis pinned" "$N" \
    '      projectIdentity={listing.data?.root}' \
    '      projectIdentity={undefined}'
  mutate web "a layout is written under the provisional key" "$P" \
    '    if (!keyResolved) return
    const timer = setTimeout(() => {' \
    '    const timer = setTimeout(() => {'

  # 6. Admin removed the key itself and reported success unconditionally: a
  # pending write rewrote it, an early press cleared `default`, and a storage
  # that refused the deletion was reported as "Cleared."
  mutate web "the reset clears a key that is not this project's" "$P" \
    "    if (!keyResolved) return 'unresolved'" \
    '    if (false) return undefined as never'
  mutate web "the reset reports a deletion it did not verify" "$P" \
    '    return window.localStorage.getItem(key) === null' \
    '    return true'
  # The claim is "a write already on its way does not undo the reset". The
  # explicit `clearTimeout` cannot be the row that holds it: the reset also
  # changes state, so React runs the write effect's cleanup and cancels the
  # very same timer — removing the explicit cancel leaves the suite green, and
  # a row that reports that is a row claiming coverage it does not have. What
  # actually makes the pending write harmless is the touched reset beside it,
  # so that is what this breaks.
  mutate web "the reset leaves the panes marked as chosen" "$P" \
    '    touched.current = { ...NOTHING_TOUCHED }' \
    ''

  # 7. The re-seed excluded the viewport deliberately, so a desk opened narrow
  # left an untouched rail at 56px and a configured-open Inspector shut once
  # the window was widened.
  mutate web "widening never gives an untouched pane its layout back" "$P" \
    '  const viewportSignature = `${viewport.railIsDrawer}|${viewport.inspectorIsDrawer}`' \
    "  const viewportSignature = ''"

  # 8. The tab root between the console and its body was an ordinary block, so
  # a log longer than the pane was clipped rather than scrolled.
  mutate web "the console log is clipped rather than scrolled" "$T" \
    '        className="desk-console-tabs"' \
    '        '
  mutate web "the console's flex chain is declared for nothing" "$G" \
    '  .desk-console-tabs {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }' \
    '  .desk-console-tabs {
    display: block;
  }'

  # 11. The rail drawer is modal, so a link that navigated and left it standing
  # put the destination behind an overlay — including where the viewer was
  # already on that route.
  mutate web "the rail drawer stays open over the page it navigated to" "$L" \
    '                onNavigate={() => onDrawerOpenChange(false)}' \
    '                onNavigate={undefined}'

  # 12. A closed Dialog unmounts its portal, so an unconditional `aria-controls`
  # named an id that is not in the document; and neither drawer had a trigger
  # for Radix to restore focus to.
  mutate web "the rail opener points at an element that is not there" "$H" \
    "            aria-controls={railDrawerOpen ? 'desk-rail' : undefined}" \
    '            aria-controls="desk-rail"'
  mutate web "the inspector toggle points at an unmounted drawer" "$H" \
    "          aria-controls={!inspectorIsDrawer || inspectorOpen ? 'desk-inspector' : undefined}" \
    '          aria-controls="desk-inspector"'
  mutate web "closing the rail drawer drops focus on the body" "$L" \
    '            onCloseAutoFocus={(event) => {
              event.preventDefault()
              openerRef?.current?.focus()
            }}' \
    ''
  mutate web "closing the inspector drawer drops focus on the body" "$E" \
    '            onCloseAutoFocus={(event) => {
              event.preventDefault()
              openerRef.current?.focus()
            }}' \
    ''
  mutate web "the rail drawer has no visible way out" "$L" \
    '            <div className="desk-drawer-head">
              <Dialog.Close asChild>
                <button type="button" className="desk-icon-button" aria-label="Close navigation">
                  <IconClose />
                </button>
              </Dialog.Close>
            </div>' \
    ''

  # 13. A 413, a permission refusal or a dead socket resolved to the defaults
  # with the reason recorded where nothing rendered it, so a desk that could
  # not open its own file looked exactly like a desk with no file.
  mutate web "an unreadable configuration is reported as an absent one" "$Z" \
    '      if (cause.status === 404) return effectiveConfig(undefined, reasonFor(cause))' \
    '      if (true) return effectiveConfig(undefined, reasonFor(cause))'
  mutate web "an unreadable configuration is silent outside Admin" "$S" \
    '        {problems.length === 0 && readFailure !== undefined && (' \
    '        {false && ('
  mutate web "the strip's warning is the half that disappears" "$G" \
    '  .desk-strip-warn {
    flex: 0 0 auto;
    white-space: nowrap;
    color: var(--warn);
  }' \
    '  .desk-strip-warn {
    color: var(--warn);
  }'

  # 14. The identity slot's guard was a blacklist of five names, so the rule it
  # held was "not these five" rather than "one field".
  mutate web "the identity type grows a second field" "$D" \
    'export interface IdentityConfig {
  provider: IdentityProviderConfig | null
}' \
    'export interface IdentityConfig {
  provider: IdentityProviderConfig | null
  strategy?: string
}'
  mutate web "the provider object grows a ninth member" "$D" \
    '  showRemoteAvatar: boolean
  signOut: '"'"'local'"'"' | '"'"'provider'"'"'
}' \
    '  showRemoteAvatar: boolean
  signOut: '"'"'local'"'"' | '"'"'provider'"'"'
  strategy?: string
}'
  mutate web "the identity state carries a discriminator again" "$I" \
    '  /** Null exactly where `identity.provider` is null. There is no third value. */
  provider: ProviderIdentity | null' \
    "  mode?: 'local' | 'provider'
  provider: ProviderIdentity | null"

  # 15. `retryOnMount` re-runs an errored query when a second observer
  # subscribes, and every route change is a second observer beside the rail's.
  mutate web "a refused pack listing is called again on every route change" "$J" \
    '    retryOnMount: false,' \
    ''

  # 16. Read loosely, `Mod+B` also claimed Ctrl+Shift+B — and firing on it
  # prevented the default, so the shifted spelling took the key from whatever
  # else wanted it.
  mutate web "a shifted chord fires a shortcut it never declared" "$K" \
    '  if (event.shiftKey) return undefined' \
    ''
  mutate web "Ctrl and Meta together are read as one modifier" "$K" \
    '  const mod = event.ctrlKey !== event.metaKey' \
    '  const mod = event.ctrlKey || event.metaKey'

  # 18. The mark's bound is documented in bytes; counted in UTF-16 code units
  # it is up to three times looser on a mark that is not ASCII — three and not
  # four, because a four-byte astral character costs two UTF-16 units.
  mutate web "the mark's size bound is counted in code units" "$D" \
    '  const bytes = new TextEncoder().encode(value).length' \
    '  const bytes = value.length'

  # ---- Codex round 2 -----------------------------------------------------

  # 1. The outgoing record started at `{v}`, so a Console toggle erased a rail
  # and an Inspector the viewer had chosen on an earlier visit.
  mutate web "a write erases the choices an earlier visit made" "$P" \
    '  const kept = readShellState(key) ?? {}' \
    '  const kept: Partial<ShellState> = {}'

  # 2. Zero renders an "open" pane nobody can see; an enormous one pushes the
  # strip out of a frame that does not scroll.
  mutate web "a pane dimension of zero or twenty thousand is accepted" "$D" \
    '  const bounds = PANE_BOUNDS[key]' \
    '  const bounds = undefined as { min: number; max: number } | undefined'
  mutate web "a configured pane may take the whole frame" "$G" \
    '    grid-template-columns:
      min(var(--rail-current), var(--side-cap))
      minmax(0, 1fr)
      min(var(--inspector-current), var(--side-cap));' \
    '    grid-template-columns: var(--rail-current) minmax(0, 1fr) var(--inspector-current);'
  mutate web "the console may grow past the route it sits under" "$G" \
    '      min(
        var(--console-current),
        max(var(--console-cap), min(var(--console-floor), var(--console-room)))
      )' \
    '      var(--console-current)'
  mutate web "the caps reserve no room for main at all" "$G" \
    '    --main-floor: 120px;
    --side-cap: 40vw;' \
    '    --main-floor: 0px;
    --side-cap: 100vw;'

  # 3. The write was gated and the read was not: a stale `default` record
  # applied one project's layout to another while the listing was in flight,
  # and permanently where it failed.
  mutate web "the provisional record is read even though it is not written" "$P" \
    '  const storedForKey = () => (keyResolved ? readShellState(storageKey) : undefined)' \
    '  const storedForKey = () => readShellState(storageKey)'

  # 4. The full cue is wider than a 320px strip has beside the console button,
  # and it neither shrinks nor wraps.
  mutate web "the narrow strip paints the full sentence over the console control" "$S" \
    '      <span className="desk-strip-warn-short" aria-hidden="true">
        {short}
      </span>' \
    ''
  mutate web "the short cue is never the one painted" "$G" \
    '  @media (max-width: 599px) {
    .desk-strip-warn-full {
      display: none;
    }

    .desk-strip-warn-short {
      display: inline;
    }
  }' \
    ''
  mutate web "the cue's accessible name is whichever spelling is painted" "$S" \
    '    <Link className="desk-strip-warn" to="/admin" aria-label={full}>' \
    '    <Link className="desk-strip-warn" to="/admin">'

  # 5. The live layout was cleared even where the deletion was refused, so
  # Admin said "the layout is unchanged" over panes that had visibly moved.
  mutate web "a refused deletion still moves the panes" "$P" \
    "    if (!resetShellState(storageKey)) return 'refused'" \
    '    const refusedDeletion = !resetShellState(storageKey)'

  # 6. The column's default is 360px and the drawer's has always been 320px.
  mutate web "an unconfigured desk's drawer moves to the column's width" "$E" \
    '              declaredWidth === undefined
                ? undefined
                : ({ '"'"'--drawer-w'"'"': `${declaredWidth}px` } as CSSProperties)' \
    '              ({ '"'"'--drawer-w'"'"': `${declaredWidth ?? 360}px` } as CSSProperties)'
  mutate web "the decoder cannot say which dimensions the file stated" "$D" \
    "        leftWidth: declares(left, 'width')," \
    '        leftWidth: true,'

  # 7. "The reason is the chassis' own" was false for a browser error: nothing
  # answered, so nothing the chassis said is being quoted.
  mutate web "a browser error is attributed to the chassis" "$Z" \
    '    if (cause instanceof FileRequestError) {' \
    '    if (false) {'
  mutate web "Admin sources every unread reason to the chassis" "$V" \
    "          ) : readFailure.source === 'chassis' ? (" \
    '          ) : true ? ('

  # ---- Codex round 3 -----------------------------------------------------

  # 1. A custom property is not validated at parse time, so a second `dvh`
  # declaration wins on a browser that has never heard of `dvh` — and the
  # invalidity then surfaces at substitution, taking grid-template-rows with it.
  mutate web "the dvh cap is a second declaration rather than a guarded one" "$G" \
    '  @supports (height: 100dvh) {
    :root {
      --console-room: max(0px, calc(100dvh - var(--header-h) - var(--strip-h)));
      --main-room: max(
        0px,
        calc(100dvh - var(--header-h) - var(--strip-h) - var(--console-current))
      );
      --console-cap: max(
        0px,
        calc(100dvh - var(--header-h) - var(--strip-h) - var(--main-floor))
      );
    }
  }' \
    ''
  mutate web "the vh cap is the one that never applies" "$G" \
    '    --console-cap: max(0px, calc(100vh - var(--header-h) - var(--strip-h) - var(--main-floor)));' \
    '    --console-cap: max(0px, calc(100dvh - var(--header-h) - var(--strip-h) - var(--main-floor)));'

  # 2. On a viewport too short for the reserve the cap reaches zero, and an
  # open console renders at no height with a toggle still saying it is open.
  mutate web "an open console can be capped down to no height at all" "$G" \
    '        max(var(--console-cap), min(var(--console-floor), var(--console-room)))' \
    '        var(--console-cap)'
  # The other half, and the one the first fix got wrong on its own: a bare
  # 80px floor on a 109px viewport pushed the strip out of a frame that does
  # not scroll — trading this defect for the one the cap exists to prevent.
  mutate web "the console floor may push the strip out of the frame" "$G" \
    '        max(var(--console-cap), min(var(--console-floor), var(--console-room)))' \
    '        max(var(--console-cap), var(--console-floor))'
  mutate web "the room the console may take reserves main's share too" "$G" \
    '    --console-room: max(0px, calc(100vh - var(--header-h) - var(--strip-h)));' \
    '    --console-room: max(0px, calc(100vh - var(--header-h) - var(--strip-h) - var(--main-floor)));'
  mutate web "the console floor is smaller than a console" "$G" \
    '    --console-floor: 80px;' \
    '    --console-floor: 0px;'

  # 3. `slot.size` promises a route the pane's width; the configured number is
  # capped by the sheet and ignored outright by the drawer form.
  mutate web "the slot reports a configured width the pane does not have" "$N" \
    '      size: shell.inspector.open ? (inspectorBox?.width ?? 0) : 0,' \
    '      size: shell.inspector.open ? inspectorWidth : 0,'
  mutate web "the pane is never published for measurement" "$E" \
    '      ref={publishPane}
      className="desk-inspector"' \
    '      className="desk-inspector"'
  mutate web "a measured box is taken once and never again" "$O" \
    '    const observer = new ResizeObserver(read)
    observer.observe(element)
    return () => observer.disconnect()' \
    ''

  # 4. Admin printed a decoded number with nothing said about what bounds it,
  # what the frame does to it, or what is actually on screen.
  mutate web "Admin names no accepted range at all" "$V" \
    "const PANE_DIMENSIONS = [
  { key: 'panes.left.width' },
  { key: 'panes.inspector.width' },
  { key: 'panes.console.height' }
] as const" \
    'const PANE_DIMENSIONS = [] as const'
  mutate web "Admin calls a configured number the rendered one" "$V" \
    '        Rail: <code>{config.panes.left.mode}</code>, configured{'"'"' '"'"'}
        <strong>{config.panes.left.width}px</strong> — rendered{'"'"' '"'"'}
        <Rendered box={rendered.rail} axis="width" />' \
    '        Rail: <code>{config.panes.left.mode}</code>, {config.panes.left.width}px'
  mutate web "an absent pane is reported as a pane of zero" "$V" \
    "  if (box === undefined) return <span className=\"quiet\">not mounted at this width</span>" \
    '  if (box === undefined) return <strong>0px</strong>'

  # 5. A 200 whose body is not the envelope this API promises is still an
  # answer — losing its status put it in the transport-failure bucket.
  mutate web "a malformed answer loses the fact that it was an answer" "$C" \
    '      throw new FileRequestError(
        response.status,
        `the desk answered ${response.status} with text that is not JSON`,
        '"'"'desk'"'"'
      )' \
    '      throw new Error(`the desk answered ${response.status} with text that is not JSON`)'
  mutate web "every answered reason is quoted as the chassis' own" "$C" \
    "      'chassis'," \
    "      'desk',"
  mutate web "provenance is inferred from the status again" "$V" \
    '          {!readFailure.responseReceived ? (' \
    '          {false ? ('
  # ---- Verification round ------------------------------------------------
  # One row per safeguard this round's findings put in. Named after the defect
  # each restores, not the code each edits.

  # The order of the create sequence, which is the whole of what makes it safe.
  mutate web "the id is not re-checked against the file as it is now" "$X" \
    '        keys: existingPackKeys(current),' \
    '        keys: [],'
  mutate web "a file another entry already claims is written over" "$X" \
    '        paths: existingPackPaths(current),' \
    '        paths: [],'
  mutate web "an unreadable jpack.json is discovered after the pack is written" "$X" \
    '      } catch (cause) {
        const absent = cause instanceof FileRequestError && cause.status === 404
        setFailure(
          absent
            ? { lead: NO_PROJECT_FILE }
            : { lead: UNREADABLE_PROJECT_FILE, reason: reasonOf(cause) }
        )
        return
      }' \
    '      } catch {
        /* the mutant finds out later */
      }'
  mutate web "a template that is not a document is sent anyway" "$X" \
    '      let content: string
      try {
        content = shapeTemplate(template, { name, description, slug, idBase })
      } catch (cause) {
        setFailure({ lead: '"'"'This template could not be used.'"'"', reason: reasonOf(cause) })
        return
      }' \
    '      const content = shapeTemplate(template, { name, description, slug, idBase })'

  # A listing that failed is not a project with no files in it.
  mutate web "a listing that failed is reported as a project with no jpack.json" "$X" \
    '  const blocked = listing.isError' \
    '  const blocked = false'
  mutate web "Create is offered against a listing that never answered" "$X" \
    '    listing.isSuccess' \
    '    !listing.isPending'

  # The dialog stays on screen for as long as it is the only place the outcome
  # is stated.
  mutate web "the dialog can be dismissed mid-sequence" "$X" \
    '        if (!next && busy) return' \
    '        void busy'
  mutate web "Cancel stays live while the sequence runs" "$X" \
    '            <Button variant="secondary" disabled={busy}>' \
    '            <Button variant="secondary">'

  # What a refusal says.
  mutate web "a taken pack file is reported in an editor's words" "$X" \
    "          lead: refusalLead(cause) ?? 'The pack could not be created.'," \
    "          lead: 'The pack could not be created.',"
  mutate web "the amended configuration is left in the cache as it was" "$X" \
    "      invalidate([['desk-files'], ['desk-file', PROJECT_FILE], ['list_packs'], ['desk-config']])" \
    "      invalidate([['desk-files'], ['list_packs'], ['desk-config']])"

  # The templates: what is offered, and what is said about it.
  # Superseded by the round-1 rows above, which break the same three claims
  # against the code that now holds them: "absence is claimed from a capability
  # listing that never answered", "Empty is offered before a schema has produced
  # a skeleton", and "the dialog states a verdict about the pack it is
  # creating". Kept as one row rather than three that name nothing.
  mutate web "nothing is selected while the templates are still being asked" "$X" \
    '  const selected = choice ?? (templatesPending ? undefined : options[0]?.value)' \
    '  const selected = choice ?? options[0]?.value'
  mutate web "a refusal with no reason given is quoted at the user anyway" "$ST" \
    "    this.reported = text !== ''" \
    '    this.reported = true'

  # The slug rule, and the two sentences it says.
  mutate web "a name too long for the file it names is accepted" "$NP" \
    '  if (slug.length > MAX_SLUG_LENGTH) {' \
    '  if (false) {'
  mutate web "diacritics are dropped rather than folded" "$NP" \
    "    .normalize('NFKD')
    .replace(/\p{Diacritic}/gu, '')
" \
    ''
  mutate web "an empty pack with no specVersion is written" "$NP" \
    "  if (typeof skeleton.specVersion !== 'string' || skeleton.specVersion === '') return undefined" \
    '  if (false) return undefined'
  mutate web "a file another pack already names is not a collision" "$NP" \
    '  if (claimedBy(project.paths, project.path)) {' \
    '  if (false) {'

  # The file the amendment is written back into.
  mutate web "a CRLF jpack.json is rewritten with the platform's line ending" "$JC" \
    "  const eol = source.includes('\r\n') ? '\r\n' : '\n'" \
    "  const eol = '\n'"

  # The location a pack goes to.
  mutate web "a directory the desk never writes into is accepted as the location" "$D" \
    '  if (skipped !== undefined) {' \
    '  if (false) {'

  # The primitives.
  mutate web "a dialog with no description points a reader at an empty one" "$DG" \
    '          ) : null}' \
    '          ) : (
            <RadixDialog.Description />
          )}'
  mutate web "the form-level failure is not announced" "$AL" \
    '    <p role="alert" className={styles.alert}>' \
    '    <p className={styles.alert}>'
  mutate web "a module spells a radius of its own" "$SELCSS" \
    '  border-radius: var(--radius-sm);' \
    '  border-radius: 4px;'

  # ---- Codex round 1 on the Create redesign ------------------------------

  # 1. The write answers with the path the chassis resolved the request to;
  # registering the requested spelling names a file the runtime cleans away.
  mutate web "the registration names the path that was asked for, not the one written" "$X" \
    '            withPack(current, slug, packEntryFor(landed.path, description))' \
    '            withPack(current, slug, packEntryFor(path, description))'

  # 2. Pending, refused and settled-empty were one state, and Empty was
  # offered on the strength of a capability flag rather than a skeleton.
  mutate web "a pending template listing is an empty one again" "$X" \
    "  const templatesPending = examplesState === 'pending' || emptyState === 'pending'" \
    '  const templatesPending = false'
  mutate web "Empty is offered before a schema has produced a skeleton" "$X" \
    "      ...(emptyState === 'ready' ? [{ value: SCHEMA_EMPTY, label: EMPTY_LABEL }] : [])" \
    '      ...(schemaSupported ? [{ value: SCHEMA_EMPTY, label: EMPTY_LABEL }] : [])'
  mutate web "a refused example listing is dropped on the floor" "$X" \
    "    : examplesState === 'error'" \
    '    : false'
  mutate web "absence is claimed from a capability listing that never answered" "$X" \
    '          ? known
            ? NO_TEMPLATE
            : undefined' \
    '          ? NO_TEMPLATE'

  # 3. The rail below 900px is a modal drawer, and the dialog is inside it.
  mutate web "a created pack leaves the rail drawer standing over it" "$LR" \
    '          onCreated={onNavigate}' \
    '          onCreated={undefined}'
  mutate web "the dialog never says it created anything" "$X" \
    '      onCreated?.()' \
    ''

  # 4. Radix restores focus to its own trigger; this dialog has none.
  mutate web "closing Create drops focus on the body" "$UD" \
    '          onCloseAutoFocus={
            openerRef === undefined
              ? undefined
              : (event) => {
                  event.preventDefault()
                  openerRef.current?.focus()
                }
          }' \
    ''
  mutate web "the Create button is never held for focus restoration" "$LR" \
    '          openerRef={createRef}' \
    '          openerRef={undefined}'

  # 5. The runtime cleans interior `./`, `//` and surviving `..` and folds
  # case; comparing raw spellings let an alias through.
  mutate web "declared paths are compared as raw strings again" "$NP" \
    '  if (claimedBy(project.paths, project.path)) {' \
    '  if (project.paths.includes(project.path)) {'
  mutate web "the path cleaner leaves an interior dot segment alone" "$PP" \
    "    if (segment === '' || segment === '.') continue" \
    "    if (segment === '') continue"
  mutate web "two spellings of one file are compared case-sensitively" "$PP" \
    '  if (a === b) return true
  const left = [...a]' \
    '  if (a !== b) return false
  const left = [...a]'
  mutate web "the candidate path is never asked about directly" "$X" \
    '        await readFile(path)
        setFailure({ lead: PACK_FILE_TAKEN })
        return' \
    '        void path'

  # 6. `partial` means the listing is not all the files, and every question
  # this dialog asks it is a question about absence.
  mutate web "an incomplete listing is treated as a complete one" "$X" \
    '  const partial = (listing.data?.partial ?? []).length > 0' \
    '  const partial = false'
  mutate web "no jpack.json is claimed for any failed read" "$X" \
    '        const absent = cause instanceof FileRequestError && cause.status === 404' \
    '        const absent = true'

  mutate web "an over-deep packs directory decodes clean" "$D" \
    '  if (trimmed.split('"'"'/'"'"').length > MAX_PACK_DIR_DEPTH) {' \
    '  if (false) {'

  # 9. Pending, failed, incomplete and obstructed shared one sentence.
  mutate web "a file at the pack location is not seen at all" "$V" \
    "  if (files.some((file) => file.path === dir)) return 'obstructed'" \
    "  if (false) return 'obstructed'"
  mutate web "a listing that has not answered describes the location anyway" "$V" \
    "  if (listing.isPending) return 'pending'" \
    "  if (false) return 'pending'"

  # 10. `empty` is a name a runtime may legitimately serve.
  #
  # The mutation is on the **example** half, not the sentinel. Both spaces are
  # namespaced now, so renaming the sentinel alone moves the collision rather
  # than restoring it — the bug was that example values were bare runtime
  # names, and this is what puts that back.
  mutate web "an example's value is the bare name the runtime gave it" "$X" \
    'const exampleValue = (name: string) => `example:${encodeURIComponent(name)}`' \
    'const exampleValue = (name: string) => name'

  # 11. A disclaimer, and a verdict the shell derived without asking.
  mutate web "the dialog states a verdict about the pack it is creating" "$X" \
    '        <Field label="Template" error={templateProblem}>' \
    '        <Field label="Template" error={templateProblem} hint="checks report it incomplete until you fill it in">'

  mutate web "the chassis sentence is put in front of whoever typed a name" "$X" \
    '          lead: refusalLead(cause) ?? '"'"'The pack could not be created.'"'"',' \
    "          lead: 'The pack could not be created.',"
  mutate web "a code this desk does not know invents a sentence" "$CR" \
    '  return code === undefined ? undefined : CREATE_REFUSALS[code]' \
    "  return CREATE_REFUSALS[code ?? ''] ?? 'That could not be done.'"
  mutate web "the chassis code never reaches the client" "$FC" \
    '      typeof envelope?.code === '"'"'string'"'"' ? envelope.code : undefined' \
    '      undefined'

  # 14. "Name" with no required and no description.
  mutate web "the required field is not marked required" "$X" \
    '          label="Name (required)"' \
    '          label="Name"'

  # 15. NFKD does not decompose these, so they were deleted.
  mutate web "a letter with no decomposition is dropped rather than carried" "$NP" \
    "  const transliterated = [...folded]
    .map((character) => TRANSLITERATED.get(character) ?? character)
    .join('')" \
    '  const transliterated = folded'
  mutate web "an unsupported Latin letter is deleted rather than named" "$NP" \
    '  if (unsupported.length > 0) {' \
    '  if (false) {'

  # 16. The rule matched border/outline and then skipped them.
  mutate web "a named colour is not a colour" "$DL" \
    '  if (named !== undefined) return `the named colour ${named}`' \
    '  if (false) return undefined'
  mutate web "a shorthand is not somewhere a colour can be written" "$DL" \
    "  return COLOUR_BEARING.has(property) || property.endsWith('-color')" \
    "  return property === 'color' || property === 'background'"

  # ---- Codex round 2 -----------------------------------------------------

  # 1. `toLowerCase()` is not `strings.EqualFold`: `ſ` and `ς` lowercase to
  # themselves, so neither ever met the letter it folds with.
  mutate web "paths are folded by lowercasing rather than by orbit" "$PP" \
    '  return ORBIT_OF.get(rune) ?? rune.toLowerCase()' \
    '  return rune.toLowerCase()'
  mutate web "the fold table loses the letters JavaScript will not close" "$PP" \
    "  ['s', 'S', '\\u017F']," \
    "  ['s', 'S'],"
  mutate web "an uppercase clause folds the Turkish dotless i into i" "$PP" \
    '  return a === b || foldRune(a) === foldRune(b)' \
    '  return a === b || foldRune(a) === foldRune(b) || a.toUpperCase() === b.toUpperCase()'
  mutate web "folding compares UTF-16 units rather than code points" "$PP" \
    '  const left = [...a]
  const right = [...b]' \
    "  const left = a.split('')
  const right = b.split('')"
  mutate web "samePath stops folding altogether" "$PP" \
    '  return left === right || equalFold(left, right)' \
    '  return left === right'

  # 2. `undefined` makes Radix's Select uncontrolled; the first real value
  # switches it, and React warns.
  mutate web "the Select goes uncontrolled while the listing is in flight" "$SEL" \
    "      value={value ?? ''}" \
    '      value={value}'

  # 3. The set is mirrored on the client, and the mirror has to be complete.
  mutate web "a chassis code has no Create sentence" "$CR" \
    "  'not-a-file': 'Something that is not a file is in the way. Nothing was created.'," \
    ''
  mutate web "the mirrored code set drifts from the chassis'" "$CR" \
    "  'excluded-directory'," \
    ''
  mutate web "an intentional control-flow code is treated as an omission" "$CR" \
    "export const CONTROL_FLOW_CODES = ['not-found'] as const" \
    "export const CONTROL_FLOW_CODES = [] as const"

  # 5. The rule stripped each `var()` before looking for names, and accepted
  # any value that merely contained one.
  mutate web "a var() fallback is not inspected" "$DL" \
    '    const problem = colourProblemIn(reference.fallback.trim())
    if (problem !== undefined) return `${problem} in a var() fallback`' \
    '    void reference'
  mutate web "a var() reference stops at the first closing paren" "$DL" \
    '      if (value[scan] === '"'"')'"'"') {
        depth -= 1
        if (depth === 0) {
          end = scan
          break
        }
      }' \
    '      if (value[scan] === '"'"')'"'"') {
        end = scan
        break
      }'
  mutate web "a module-local colour property is a token" "$DL" \
    '      const problem = colourProblemIn(held.trim())
      if (problem !== undefined && problem !== '"'"'no token'"'"') {' \
    '      const problem = colourProblemIn(held.trim())
      if (false) {'
  mutate web "a module-local colour definition is never inspected" "$DL" \
    '      const problem = colourProblemIn(declaration.value.trim())
      // Only a *colour* is a problem here. A local `--gap: 4px` is ordinary.
      if (problem !== undefined && problem !== '"'"'no token'"'"') {
        problems.push({ where: declaration.property, problem })
      }
      continue' \
    '      continue'
  # ---------------------------------------------------------------------------
  # The pack view (issue: pack view phase 1). Each row breaks one claim the
  # document, the writer, the check reader or the pane makes.
  # ---------------------------------------------------------------------------

  # 7. The writer. A splice that re-serializes is a whole-file diff dressed as
  # a one-field edit, which is exactly what ADR-0019 makes a human read.
  mutate web "the splice reserializes the whole document" "$DT" \
    '  return text.slice(0, span.valueStart) + json + text.slice(span.valueEnd)' \
    '  const whole = JSON.parse(text)
  void span
  void pointer
  void json
  return JSON.stringify(whole, null, 2)'
  mutate web "removing a member leaves its comma" "$DT" \
    '  let scan = end
  while (scan < text.length && isSpace(text[scan]!)) scan += 1
  if (text[scan] === '"'"','"'"') {' \
    '  let scan = end
  while (scan < text.length && isSpace(text[scan]!)) scan += 1
  if (false) {'
  # **The layout after the comma belongs to the member that follows.** Taking it
  # looks equivalent to taking the layout before this one and is not: a document
  # written with unequal indentation was silently reformatted by a delete.
  mutate web "a removal takes its neighbour layout rather than its own" "$DT" \
    '    start = layoutStart
    end = scan + 1' \
    '    end = scan + 1
    while (end < text.length && isSpace(text[end]!)) end += 1'
  # A dynamic member name assigned into `{}` invokes the prototype setter for
  # `__proto__` and stores nothing, so the reading carried no such member.
  mutate web "the scanner assigns member names into a plain object" "$DT" \
    '      const object: Record<string, unknown> = Object.create(null) as Record<string, unknown>' \
    '      const object: Record<string, unknown> = {}'
  # And the one gate that exists to catch a reading that disagrees with
  # JSON.parse consulted the prototype chain, so it found the inherited member
  # and reported agreement about a document it disagreed on.
  mutate web "the disagreement gate walks the prototype chain" "$DT" \
    '      if (!Object.hasOwn(mine, name) || !Object.hasOwn(theirs, name)) {' \
    '      if (!(name in mine) || !(name in theirs)) {'
  mutate web "duplicate members are read last-wins" "$DT" \
    '  const problems: Disagreement[] = []
  for (const duplicate of index.duplicates) {' \
    '  const problems: Disagreement[] = []
  if (text.length >= 0) return problems
  for (const duplicate of index.duplicates) {'

  # 6. The check reader. A layer nothing listed is a layer that did not run.
  mutate web "a layer nothing listed is reported as passed" "$CK" \
    "  const named = rows
    .map((row) => row.name)
    .filter((name): name is string => typeof name === 'string')" \
    "  const named = [...LADDER] as string[]"
  mutate web "a diagnostic re-anchors after the bytes move" "$CK" \
    '  if (checkedBytes === undefined || currentBytes === undefined) return false
  return checkedBytes !== currentBytes' \
    '  void checkedBytes
  void currentBytes
  return false'
  mutate web "a diagnostic with no exact match is dropped" "$CK" \
    '    for (const ancestor of parentPointers(named)) {
      if (rendered.has(ancestor)) {
        return { diagnostic, anchor: ancestor, named, approximate: true }
      }
    }' \
    '    void parentPointers'
  # The Checks tab's filter. Equality alone told a reader selecting a rule card
  # "No other diagnostic names this member." over a rule the runtime had just
  # refused at `/rules/0/when/value` — the one sentence this tab exists to
  # never say by accident.
  mutate web "a diagnostic under the selected member is hidden" "$CK" \
    '    (entry) => entry.anchor === pointer || entry.anchor.startsWith(`${pointer}/`)' \
    '    (entry) => entry.anchor === pointer'
  mutate web "one rule's pointer is read as a prefix of another's" "$CK" \
    'entry.anchor.startsWith(`${pointer}/`)' \
    'entry.anchor.startsWith(pointer)'
  mutate web "an empty document is sent to be refused" "$QR" \
    "      documentText !== undefined &&
      documentText !== ''," \
    '      documentText !== undefined,'
  # A payload saying `invalid` with `carrier passed, structural failed` printed
  # `structural — 1 diagnostic`: neither the verdict the runtime reached nor the
  # layer that did run.
  mutate web "the layer sentence drops the status and every row but the failure" "$CK" \
    '  const spelled = rows
    .map((row) => `${row.name ?? '"'"'an unnamed layer'"'"'} ${row.status ?? '"'"'with no status'"'"'}`)
    .join('"'"', '"'"')' \
    '  const spelled = rows
    .filter((row) => row.status !== '"'"'passed'"'"')
    .map((row) => `${row.name ?? '"'"'an unnamed layer'"'"'}`)
    .join('"'"', '"'"')'
  mutate web "a truncated list still claims nothing else was found" "$CK" \
    "  if (report?.diagnosticsTruncated !== true) return undefined" \
    "  if (report !== undefined) return undefined"

  # 5. The check query. Identical bytes answer differently on a runtime
  # bundling different artifacts, so the epoch is half the key.
  mutate web "the check query is keyed on the buffer alone" "$QR" \
    "    queryKey: ['validate', connectionEpoch, documentText ?? null]," \
    "    queryKey: ['validate', documentText ?? null],"
  # A report that does not carry the bytes it ran over cannot be compared with
  # the bytes on screen, and the comparison is the whole of the anchoring rule.
  mutate web "the report claims bytes it did not check" "$QR" \
    '      return { report: parsed, checkedBytes: documentText! }' \
    "      return { report: parsed, checkedBytes: '' }"
  mutate web "validate is assumed present" "$CAP" \
    "    validateSupported: names.has('validate')" \
    "    validateSupported: true"

  # 9. The document. An omission stated is the whole point of the rewrite.
  mutate web "an omitted member renders nothing" "$OM" \
    '  return (
    <Block pointer={pointer} className={styles.omitted}>' \
    '  if (label !== undefined) return null
  return (
    <Block pointer={pointer} className={styles.omitted}>'
  mutate web "members render in canonical order rather than the document's" "$MB" \
    '  const order = MEMBER_UNITS.filter((unit) => unitIsPresent(document, unit)).sort(
    (left, right) => positionOf(left) - positionOf(right)
  )' \
    '  const order = MEMBER_UNITS.filter((unit) => unitIsPresent(document, unit))
  void positionOf'
  # The five identity members are five units, so each finds its own place in the
  # document order. The nav collapses them again — a nav question answered in
  # the nav, not by moving three members in front of a fourth.
  # The spy answers in reading-unit pointers and the nav lists Identity once, so
  # four of the five equalled no entry and marked nothing at all.
  mutate web "an identity member marks no outline entry" "$PV" \
    '  const active = seen === null ? null : (representative.get(seen) ?? seen)' \
    '  const active = seen
  void representative'
  mutate web "a grouped unit is looked up under its own id" "$MB" \
    '    const listed = entries.find((entry) => entry.id === (unit.group ?? unit.id))' \
    '    const listed = entries.find((entry) => entry.id === unit.id)'
  mutate web "the outline lists each identity member separately" "$MB" \
    '  const entries: MemberUnit[] = []
  const placed = new Set<string>()' \
    '  const entries: MemberUnit[] = []
  if (order.length >= 0) return [...order]
  const placed = new Set<string>()'
  # An absent **required** member renders nothing, because its absence is a
  # refusal the runtime issues at that pointer and a block here takes that
  # diagnostic off the strip — where every reader sees it — and puts it behind a
  # selection nobody has made.
  # A missing required member is a refusal the runtime issues at that pointer,
  # not an omission this page states — and a "not declared" line for one takes
  # that diagnostic off the strip and hides it behind a selection nobody made.
  # It also used to be *first* in reading order, which put the document's one
  # tab stop on a pointer with no element behind it.
  mutate web "an absent required member gets an omission line" "$MB" \
    '    if (unit.required === true) continue' \
    '    if (false) continue'
  # Three of the seven roots the schema requires were marked optional here.
  mutate web "the rules member is treated as optional" "$MB" \
    "  {
    id: 'rules',
    label: 'Rules',
    members: ['rules'],
    pointer: '/rules',
    counted: true,
    required: true
  }," \
    "  { id: 'rules', label: 'Rules', members: ['rules'], pointer: '/rules', counted: true },"
  # Every omission at the end would have passed the ordering test this replaces:
  # it filtered every omission out of the actual output before comparing.
  mutate web "an omission is drawn after the members rather than in its place" "$MB" \
    '    order.splice(anchor + 1, 0, unit)' \
    '    void anchor
    order.push(unit)'
  mutate web "the condition tree paraphrases" "$CT" \
    '        <Block pointer={`${at}/operator`} as="span" className={styles.op}>
          {String(node.operator ?? '"''"')}
        </Block>{'"' '"'}
        <Block pointer={`${at}/value`} as="code" className={styles.literal}>
          {JSON.stringify(node.value)}
        </Block>' \
    '        <Block pointer={`${at}/operator`} as="span" className={styles.op}>
          is greater than
        </Block>{'"' '"'}
        <Block pointer={`${at}/value`} as="code" className={styles.literal}>
          {String(node.value)}
        </Block>'

  # 12. The pane's empty state used to stand beside every published panel.
  mutate web "the empty state stands beside a published panel" "$RP" \
    '      {showEmpty && <p className="desk-pane-empty">{EMPTY_STATE}</p>}' \
    '      <p className="desk-pane-empty">{EMPTY_STATE}</p>'

  # 14. jsdom lays nothing out, so a measured height of zero must render every
  # row — otherwise every test of the pane asserts against an empty list.
  mutate web "the windowed list renders nothing when the viewport cannot be measured" "$WR" \
    '  if (height <= 0 || rowHeight <= 0) {
    return { ref, scrollRowIntoView, start: 0, end: count, padTop: 0, padBottom: 0 }
  }' \
    '  if (false) {
    return { ref, scrollRowIntoView, start: 0, end: count, padTop: 0, padBottom: 0 }
  }'
  # A window computed from a scroll position that belongs to the longer list
  # begins after the end of the shorter one and renders no rows at all: filter
  # 300 rows to one while scrolled down and the pane goes blank with a match in
  # it. Held by the hook test, which measures a viewport jsdom cannot.
  mutate web "a shorter list keeps the scroll position of the longer one" "$WR" \
    '    const limit = Math.max(0, count * rowHeight - node.clientHeight)
    if (node.scrollTop > limit) {' \
    '    const limit = Math.max(0, count * rowHeight - node.clientHeight)
    if (false) {'
  # The listener followed a ref object that never changes, so a list unmounted
  # by a failed refetch and mounted again at the same length kept its listener
  # on the detached node: scrolling did nothing, for ever, with no way back but
  # a reload. Ignoring later nodes is exactly that defect.
  mutate web "the scroll listener stays on the element that was replaced" "$WR" \
    '  const ref = useCallback((next: HTMLElement | null) => setNode(next), [])' \
    '  const ref = useCallback(
    (next: HTMLElement | null) => setNode((previous) => previous ?? next),
    []
  )'
  # A row the keyboard asks for that is not rendered cannot be focused. The
  # scroll comes first and the focus follows it in.
  mutate web "an off-window row is never brought on screen" "$WR" \
    '      if (top < node.scrollTop) node.scrollTop = top
      else if (bottom > node.scrollTop + node.clientHeight) {
        node.scrollTop = bottom - node.clientHeight
      }' \
    '      void top
      void bottom'

  # 16. A `0` beside Packs is a claim about a project the desk knows nothing
  # about.
  mutate web "the rail claims a count for a listing that never answered" "$RL" \
    '  const count = error === null && data !== undefined ? (data.packs ?? []).length : undefined' \
    '  const count = (data?.packs ?? []).length'


  # ---------------------------------------------------------------------------
  # The verification round. Each row breaks one claim a review found the desk
  # making without holding.
  # ---------------------------------------------------------------------------

  # The References panel. `escalation.triggers` is a closed enum of five reason
  # words, so reading one as an evidence-requirement id printed a
  # dangling-reference claim on every conformant pack.
  mutate web "escalation triggers are read as evidence requirement ids" "$RF" \
    '  // The fallback outcome names an outcome like any other reference does.' \
    '  if (at === pointer(['"'"'escalation'"'"'])) {
    for (const id of document.escalation?.triggers ?? []) {
      lines.push(named('"'"'trigger'"'"', id, evidenceAt, '"'"'evidence requirement'"'"'))
    }
  }

  // The fallback outcome names an outcome like any other reference does.'
  # Two outcomes named `approve` made a rule link to `/outcomes/1` as though the
  # document had said which. It had not: it had said the id twice.
  mutate web "a duplicated id is resolved to one of the two" "$RF" \
    '    if (targets.length === 1) return { relation, id, target: targets[0] }
    return { relation, id, candidates: targets }' \
    '    return { relation, id, target: targets[0] }'
  mutate web "an id declared twice keeps only the last place it was declared" "$RF" \
    '    const existing = where.get(entry.id)
    if (existing === undefined) where.set(entry.id, [at])
    else existing.push(at)' \
    '    where.set(entry.id, [at])'
  # **One evaluator, or two panels disagree about one address.** This read the
  # index out of token one and never looked further, so `/rules/0/nonesuch` and
  # `/rules/0/constructor` printed rule zero's references beside a member panel
  # showing nothing at all.
  mutate web "an address the document does not carry still answers" "$RF" \
    '  if (valueAt(document, at) === undefined) return []' \
    '  void valueAt'
  mutate web "a rule with no outcome still gets an unresolved-id line" "$RF" \
    "    if (typeof rule.outcome === 'string') {" \
    '    if (true) {'
  # And the fixture the whole reference model is read against.
  mutate web "a fixture may assert a shape the spec forbids" "$FX" \
    '  "triggers": ["missing-required-evidence", "unknown"],' \
    '  "triggers": ["screening-report"],'

  # The writer's span index — the whole reason the module exists. A
  # first-occurrence replace passes every other case in that file, because the
  # two pointers they exercise hold text that is unique in the document.
  mutate web "the splice replaces the first matching text rather than the span" "$DT" \
    '  return text.slice(0, span.valueStart) + json + text.slice(span.valueEnd)' \
    '  return text.replace(text.slice(span.valueStart, span.valueEnd), json)'

  # The slot. A claim without a publication suppresses the pane's empty state
  # and puts nothing in its place.
  mutate web "the slot is claimed for a node that is not there" "$IS" \
    "  const publishing = target !== null && node !== null && node !== undefined" \
    '  const publishing = target !== null'

  # The scroll-spy. `?at` addresses every block; the outline lists twelve units.
  mutate web "a selection under a member marks no outline entry" "$SPY" \
    '  const inOutline = selected === null ? undefined : outlineUnitFor(pointers, selected)' \
    '  const inOutline = selected === null ? undefined : selected'
  # **The selection used to win, and `?at` persists**, so the observer could
  # never answer again: a reader who selected a rule and scrolled to the sources
  # watched the outline keep marking Rules for the rest of the visit.
  mutate web "a standing selection outranks what is on screen" "$SPY" \
    '  return seen ?? inOutline ?? null' \
    '  return inOutline ?? seen ?? null'
  # An answer carried across documents marks a unit that is no longer there.
  mutate web "what was seen in one document is kept for the next" "$SPY" \
    '  useEffect(() => {
    setSeen(null)
  }, [key, revision])' \
    '  useEffect(() => {
    void key
  }, [key, revision])'
  # **The member list is not the document.** A refetch of another revision with
  # the same top-level members left the answer standing and the observer
  # watching element objects the render had already replaced.
  mutate web "the reset is keyed on the member list rather than the document" "$SPY" \
    '  useEffect(() => {
    setSeen(null)
  }, [key, revision])' \
    '  useEffect(() => {
    setSeen(null)
  }, [key])'
  mutate web "the observer outlives the document it was built for" "$SPY" \
    '    return () => observer.disconnect()
  }, [key, revision])' \
    '    return () => observer.disconnect()
  }, [key])'

  # One address, one element.
  mutate web "applicability renders two elements at one pointer" "$AB" \
    '    <section>' \
    '    <section id={at} data-pointer={at}>'

  # The Checks tab. An empty set is not an answer while the check is in flight,
  # and it is not one about bytes that have moved either.
  mutate web "a check that has not answered reads as a clean bill" "$CTAB" \
    '      {pending ? (' \
    '      {pending && false ? ('
  mutate web "a stale check still issues its clean bill" "$CTAB" \
    '      ) : stale ? null : (' \
    '      ) : stale && false ? null : ('
  mutate web "the route never says the check is still running" "$PV" \
    '        pending={fetching}' \
    '        pending={false}'
  # "checked against the bytes of x" printed under "this document is unchecked"
  # is one of the two lying, and the reader cannot tell which.
  mutate web "a check that never ran is still said to have run" "$PV" \
    '  const provenance =
    unavailable !== undefined
      ? undefined
      : fetching
        ? `checking against ${whichBytes}`
        : check.data !== undefined
          ? `checked against ${whichBytes}`
          : undefined' \
    '  const provenance = `checked against ${whichBytes}`'
  mutate web "the panel invents provenance it was not given" "$CTB" \
    '      {checkedWhat !== undefined && <p className={styles.footer}>{checkedWhat}</p>}' \
    "      <p className={styles.footer}>{checkedWhat ?? 'checked against the bytes on screen'}</p>"

  # **The High.** `stale` was hard-coded false, so a report over the file on
  # disk was anchored onto the served document: a `/rules/0` diagnostic landing
  # on a rule that is not the rule it is about.
  mutate web "the page never asks whether the check ran over these bytes" "$PV" \
    '  const stale = isStale(check.data?.checkedBytes, shownText)' \
    '  const stale = false
  void isStale
  void shownText'
  # And the other half: knowing it is stale and anchoring anyway. Not "fewer"
  # diagnostics and not "the ones that still resolve" — none of them.
  mutate web "a stale report is anchored onto the document anyway" "$PV" \
    '  const report = stale ? undefined : check.data?.report' \
    '  const report = check.data?.report'
  # A disabled query reports `isPending` for ever, so an empty buffer said
  # "Checking…" about a check that was never going to start.
  mutate web "an empty document is reported as being checked" "$PV" \
    '  if (bytes === undefined || bytes === '"'"''"'"') {
    return '"'"'There are no bytes to check yet, so this document is unchecked.'"'"'
  }' \
    '  void bytes'

  # A diagnostic that named no rendered member.
  mutate web "a diagnostic anchored on the document is counted and never printed" "$CS" \
    '      {rootAnchored.length > 0 && (' \
    '      {false && ('

  # The two other views on this pack, which nothing else links to.
  mutate web "the what-if view loses its last way in" "$PV" \
    '                    <Link
                      className={styles.elsewhereLink}
                      to={`/packs/${encodeURIComponent(packId ?? '"''"')}/evaluate`}
                    >
                      Try it
                    </Link>' \
    '                    {null}'

  # Selecting with the pane closed.
  mutate web "an address that arrives with a selection opens no pane" "$PV" \
    '    if (at === null) return
    slot.reveal()' \
    '    if (at === null) return
    void slot'
  # **Back is an arrival.** Recording only the keys that revealed meant an entry
  # without `?at` returned before writing anything down, so Back to the selected
  # entry before it looked like the rerender it is not.
  mutate web "an arrival with no selection is not recorded as visited" "$PV" \
    '    if (visited.current === locationKey) return
    visited.current = locationKey
    if (at === null) return' \
    '    if (at === null) return
    if (visited.current === locationKey) return
    visited.current = locationKey'
  # A mount-only effect made *zero* calls for /packs/a -> /packs/a?at=/rules/0,
  # which reuses this component: every References link opened nothing.
  mutate web "a selection arriving in an address the route is already at opens nothing" "$PV" \
    '  }, [at, locationKey, slot])' \
    '    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])'
  # The other half: this must not fight a viewer who closed the pane and stayed
  # where they are. The unit is a history entry, not a render.
  mutate web "every rerender reopens a pane the viewer closed" "$PV" \
    '    if (visited.current === locationKey) return' \
    '    if (false) return'
  # "If closed, toggle" is one gesture read twice, and StrictMode — which
  # production runs in — runs an effect twice on purpose.
  mutate web "the shell flips the Inspector where a route asked it to open" "$AS" \
    '  const reveal = shell.openInspector' \
    '  const reveal = shell.toggleInspector'
  mutate web "opening the Inspector is a toggle" "$P" \
    '    setState((previous) =>
      previous.inspector.open ? previous : { ...previous, inspector: { open: true } }
    )' \
    '    setState((previous) => ({ ...previous, inspector: { open: !previous.inspector.open } }))'

  # The document's one tab stop, and what it is for.
  mutate web "no block is reachable by keyboard" "$BK" \
    '      tabIndex={cursor.at === pointer ? 0 : -1}' \
    '      tabIndex={-1}'
  mutate web "the arrow keys move nothing" "$PDV" \
    "  if (event.key === 'ArrowDown') next = Math.min(last, current + 1)" \
    '  if (false) next = current'
  mutate web "Enter on a block selects nothing" "$PDV" \
    "  if (event.key === 'Enter' || event.key === ' ') {" \
    '  if (false) {'

  # Two paths to one act, one history entry, one address.
  mutate web "an outline entry fills the Back stack" "$MO" \
    '              to={{ search, hash: `#${entry.pointer}` }}
              // Choosing what to inspect is not a navigation, and the block
              // beside it replaces. Two paths to one act, one history entry.
              replace' \
    '              to={{ hash: `#${entry.pointer}` }}'
  # The document renders an addressed block for an omitted member, so an outline
  # entry that could not reach it was the only line in this nav naming something
  # you could not go to. This is that shape, restored.
  mutate web "an omitted member is the one outline entry you cannot follow" "$MO" \
    "            <Link
              className={entry.present ? styles.outlineLink : styles.outlineAbsentLink}
              to={{ search, hash: \`#\${entry.pointer}\` }}
              // Choosing what to inspect is not a navigation, and the block
              // beside it replaces. Two paths to one act, one history entry.
              replace
              aria-current={active === entry.pointer ? 'true' : undefined}
            >
              {entry.label}
              {entry.present ? (
                entry.count !== undefined && (
                  <span className={styles.outlineCount}> {entry.count}</span>
                )
              ) : (
                <span className={styles.outlineAbsent}> — not declared</span>
              )}
            </Link>" \
    "            {entry.present ? (
              <Link
                className={styles.outlineLink}
                to={{ search, hash: \`#\${entry.pointer}\` }}
                replace
                aria-current={active === entry.pointer ? 'true' : undefined}
              >
                {entry.label}
                {entry.count !== undefined && (
                  <span className={styles.outlineCount}> {entry.count}</span>
                )}
              </Link>
            ) : (
              <span className={styles.outlineLink}>
                {entry.label}
                <span className={styles.outlineAbsent}> — not declared</span>
              </span>
            )}"

  # A version the listing did not answer with.
  # A row the keyboard asks for that is not rendered is focused in the render
  # that brings it in — the step between `moveFocus` and `scrollRowIntoView`,
  # which each had a test and the thing between them did not.
  mutate web "a row that is not rendered yet is never focused" "$PN" \
    '              else setWanted(next)' \
    '              else void next'
  mutate web "a shorter row height leaves the scroll where it was" "$WR" \
    '  }, [node, count, rowHeight])' \
    '  }, [node, count])'
  # The data model kept every candidate and had a row for it; what a reader sees
  # was held by nothing, and a panel quietly linking the first would have passed.
  mutate web "the panel picks one of two identically named outcomes" "$RTAB" \
    '          {reference.candidates !== undefined ? (' \
    '          {false ? ('
  # The enum walk descended through composite nodes while checking only the
  # operators inside `fact` ones.
  mutate web "a composite condition may be spelled any way at all" "$FX" \
    '"op": "all"' \
    '"op": "alll"'

  # react-query keeps the last good data through a refetch error, so a failed
  # refresh left this button under the failure sentence offering to show all N
  # of a listing the pane had just said it could not read.
  mutate web "a failed refresh still offers to show every pack" "$PN" \
    '      {isSuccess && !expanded && packs.length > FIRST_SCREENFUL && (' \
    '      {!expanded && packs.length > FIRST_SCREENFUL && ('
  mutate web "an empty version is drawn as a version" "$PN" \
    '                  {isSpelled(pack.packVersion) ? (' \
    '                  {pack.packVersion !== undefined ? ('

  # The pointer escaping, at the one call site whose step is document data.
  mutate web "a pointer step is concatenated rather than escaped" "$PT" \
    '  const parts = parsePointer(at)
  if (parts === undefined) return at
  return pointer([...parts, step])' \
    '  return `${at}/${String(step)}`'

  # **One evaluator, three ways it used to be wrong.** A malformed escape read
  # as ordinary text; `Number(part)` taking `01`, `1e0` and `-0` for indices; and
  # `in`, which answers for members no JSON document has.
  mutate web "a malformed escape is read as ordinary text" "$PT" \
    "      const next = token[index + 1]
      if (next === '0') decoded += '~'
      else if (next === '1') decoded += '/'
      else return undefined" \
    "      const next = token[index + 1]
      if (next === '0') decoded += '~'
      else if (next === '1') decoded += '/'
      else { decoded += '~'; continue }"
  mutate web "an array index is read with Number rather than the grammar" "$PT" \
    '  if (!/^(0|[1-9][0-9]*)$/.test(token)) return undefined
  return Number(token)' \
    '  const index = Number(token)
  if (!Number.isInteger(index) || index < 0) return undefined
  return index'
  mutate web "a pointer selects a member from the prototype chain" "$PT" \
    '    if (typeof value === '"'"'object'"'"' && value !== null && Object.hasOwn(value, part)) {' \
    '    if (typeof value === '"'"'object'"'"' && value !== null && part in value) {'

  # The rail's count, in the place assistive technology reads it.
  mutate web "the rail count is invisible to a screen reader" "$RL" \
    "          aria-label={count === undefined ? 'Packs' : \`Packs, \${count}\`}" \
    '          aria-label="Packs"'

  # 8. The widened convention rule, and a module outside src/ui to break it on.
  mutate web "a module outside src/ui may spell a colour" "$CV" \
    "const allModules = everyModule(SRC).sort()" \
    "const allModules = everyModule(join(SRC, 'ui')).sort()"
  mutate web "a colour in a module outside src/ui goes unreported" "$PPC" \
    "  color: var(--danger);" \
    "  color: #ff0000;"

  # 9. Edit mode (issue: pack view phase 2). Each row breaks one claim the
  # editor makes about the bytes it writes, the check it quotes, or the run it
  # asks for.
  WRT=web/src/packs/edit/writes.ts
  BUF=web/src/packs/edit/useDocumentBuffer.ts
  COP=web/src/packs/edit/conditionOps.ts
  CB=web/src/packs/edit/ConditionBuilder.tsx
  CF=web/src/packs/edit/CardForm.tsx
  PF=web/src/packs/edit/PointerField.tsx
  TIP=web/src/packs/edit/TryItPane.tsx
  SWA=web/src/packs/edit/StaleWriteAlert.tsx
  LL=web/src/packs/edit/LockLine.tsx
  ABR=web/src/shell/authorBridge.ts

  # **The splice, which is the whole mechanism.** A writer that re-serialized
  # would pass every assertion about the *value* and hand a reviewer a diff of
  # every line for a one-word change — which is the diff ADR-0019 makes a human
  # read.
  mutate web "the writer re-serializes the document instead of splicing it" "$DT" \
    '  return text.slice(0, span.valueStart) + json + text.slice(span.valueEnd)' \
    '  const whole = JSON.parse(text) as unknown
  const parts = pointer === "" ? [] : pointer.slice(1).split("/")
  let holder: Record<string, unknown> = whole as Record<string, unknown>
  for (const part of parts.slice(0, -1)) holder = holder[part] as Record<string, unknown>
  if (parts.length > 0) holder[parts[parts.length - 1]!] = JSON.parse(json) as unknown
  return JSON.stringify(whole, null, 2)'
  mutate web "an added member invents its own indentation" "$DT" \
    '  const layout = leadingLayout(text, model.memberStart)' \
    "  const layout = '\\n  '"
  mutate web "a move leaves the array exactly as it was" "$DT" \
    '  if (from === to) return text' \
    '  if (from !== to) return text'

  # Blanking a `nonEmptyString`. `""` is a document the runtime refuses by name;
  # the member's absence is a document that is merely smaller.
  mutate web "blanking a nonEmptyString writes an empty string" "$WRT" \
    "  if (value === '' && isNonEmptyString(pointer)) {" \
    "  if (value === '\\u0000' && isNonEmptyString(pointer)) {"
  # The field an author reaches for is the one their draft omitted.
  mutate web "a member that is not there yet is written nowhere" "$WRT" \
    '  if (spanAt(current.index, pointer) !== undefined) {
    return rewritten(replaceValue(current.text, current.index, pointer, json))
  }' \
    '  if (spanAt(current.index, pointer) !== undefined) {
    return rewritten(replaceValue(current.text, current.index, pointer, json))
  }
  return current'
  mutate web "a new member is appended rather than placed by the schema" "$WRT" \
    '  const order = memberOrder(container)
  const at = order.indexOf(name)
  if (at < 0) return { last: true }' \
    '  const order = memberOrder(container)
  const at = order.indexOf(name)
  if (at >= -1) return { last: true }'

  # The buffer. Dirty is bytes, undo is actions, and the base is the viewer's.
  mutate web "dirty is a parse comparison rather than a byte one" "$BUF" \
    '  const dirty = text !== undefined && base !== undefined && text !== base.content' \
    '  const dirty =
    text !== undefined && base !== undefined && text.replace(/\s+/g, "") !== base.content.replace(/\s+/g, "")'
  mutate web "undo pushes one entry per keystroke" "$BUF" \
    '      if (key !== undefined && top !== undefined && top.coalesceKey === key) return entries' \
    "      if (key === '\\u0000' && top !== undefined && top.coalesceKey === key) return entries"
  mutate web "undo lies about how far back it can go" "$BUF" \
    '      return grown.length > UNDO_DEPTH ? grown.slice(grown.length - UNDO_DEPTH) : grown' \
    '      return grown'
  mutate web "discard leaves the last save attempt’s verdict standing" "$BUF" \
    '    setStack([])
    onDiscard?.()' \
    '    setStack([])'
  # **Repaired.** The obvious mutant — dropping the `base === undefined` guard
  # outright — rebases on every render and hangs the suite, which the harness
  # reports as INCONCLUSIVE: caught, and caught in a way that names no test.
  # This one is the defect itself and nothing else: a watcher answer carrying
  # different bytes silently becomes the base, so the save that follows
  # overwrites a change nobody saw without the 409 that exists to prevent it.
  mutate web "the base moves on a watcher refetch" "$BUF" \
    '    if (loaded !== undefined && base === undefined) {' \
    '    if (loaded !== undefined && loaded.content !== base?.content) {'

  # The builder shapes and never refuses, and it never retypes what an author
  # wrote.
  mutate web "an ordered comparison emits a number rather than a decimal string" "$CB" \
    '            write((current) => setRawJson(current, at, JSON.stringify(event.target.value)), {' \
    '            write((current) => setRawJson(current, at, event.target.value), {'
  mutate web "the form refuses an empty in list" "$CB" \
    '    if (isJson(next)) {' \
    "    if (isJson(next) && next.trim() !== '[]') {"
  mutate web "changing the operator retypes the author’s operand" "$COP" \
    '  const span = spanAt(current.index, at)!
  return buffered(
    current.text.slice(0, span.valueStart) +
      JSON.stringify(operator) +
      current.text.slice(span.valueEnd)
  )' \
    '  const span = spanAt(current.index, at)!
  const written =
    current.text.slice(0, span.valueStart) +
      JSON.stringify(operator) +
      current.text.slice(span.valueEnd)
  const next = buffered(written)
  return operator === "in" ? setRawJson(next, `${at}/value`, "[]") : next'
  mutate web "a kind change throws away the operand the author wrote" "$COP" \
    '    const carried = held(name)
    if (carried !== undefined) {
      next[name] = carried
      continue
    }' \
    '    const carried = held(name)
    if (carried !== undefined && name !== "value") {
      next[name] = carried
      continue
    }'
  mutate web "a wrap re-serializes the child instead of moving its bytes" "$COP" \
    '  const raw = current.text.slice(span.valueStart, span.valueEnd)' \
    '  const raw = JSON.stringify(JSON.parse(current.text.slice(span.valueStart, span.valueEnd)))'

  # The page and the form are over the same bytes. This is the riskiest claim
  # in the whole change and it has its own row.
  mutate web "the page draws the served document while the form writes the buffer" "$PV" \
    '    (read?.index.value as PackDocument | undefined) ?? pack.data?.document' \
    '    pack.data?.document'
  mutate web "form mode is offered over a document the two readings disagree about" "$PV" \
    '  const formAvailable = disagreement.length === 0 && read?.index.value !== undefined' \
    '  const formAvailable = read?.index.value !== undefined'
  mutate web "the check gates the save" "$PV" \
    '      if (path === undefined || bufferText === undefined || buffer.base === undefined) return
      // The check runs before the save' \
    '      if (path === undefined || bufferText === undefined || buffer.base === undefined) return
      if ((check.data?.report.diagnostics?.length ?? 0) > 0) return
      // The check runs before the save'
  mutate web "Mod+S is swallowed inside the field it exists to fire in" "$PV" \
    '    if (!editing || !buffer.dirty) return
    event.preventDefault()' \
    '    if (!editing || !buffer.dirty) return
    if ((event.target as HTMLElement).tagName === "TEXTAREA") return
    event.preventDefault()'
  mutate web "Escape discards the buffer" "$PV" \
    '  const onEditorKey = (event: KeyboardEvent<HTMLElement>) => {' \
    '  const onEditorKey = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape") {
      buffer.discard()
      return
    }'

  # A diagnostic that is on screen and not described to its control is a
  # diagnostic a screen reader never reaches.
  mutate web "a diagnostic is printed beside its field rather than described to it" "$PF" \
    '      <Field
        label={label}
        hint={hint}
        error={found.length === 0 ? undefined : <Diagnostics found={found} />}
      >
        {children}
      </Field>' \
    '      <Field label={label} hint={hint}>
        {children}
      </Field>
      {found.length > 0 && <Diagnostics found={found} />}'

  # A reorder moves every `/rules/N` pointer at once.
  mutate web "focus does not follow a rule that moved" "$CF" \
    '    document.getElementById(`${arrayPointer}/${landed.at}`)?.focus()' \
    '    document.getElementById(`${arrayPointer}/${landed.at}`)'

  # The one call that can append a record nobody asked for.
  mutate web "a draft run sends both pack and pack_id" "$QR" \
    '    input.source === '"'"'pack'"'"'
      ? { pack: input.pack, facts: input.facts }' \
    '    input.source === '"'"'pack'"'"'
      ? { pack: input.pack, pack_id: '"'"'vendor-onboarding'"'"', facts: input.facts }'
  mutate web "a draft run drops the rehearsal declaration" "$QR" \
    '  if (rehearsalSupported) args.rehearsal = true' \
    "  if (rehearsalSupported && input.source === 'pack_id') args.rehearsal = true"
  mutate web "an unadvertised rehearsal is probed silently" "$TIP" \
    '  const needsConfirmation = source === '"'"'pack'"'"' && !rehearsalSupported' \
    '  const needsConfirmation = false'
  mutate web "a run stays fresh after the buffer moves" "$TIP" \
    '  const stale = ran !== null && ran.bytes !== buffer' \
    '  const stale = false'
  mutate web "the foot prints the project’s decision id rather than the payload’s" "$TIP" \
    '            <code>{ran.run.payload.packId}</code>' \
    '            <code>{packId}</code>'

  # Nothing was written, and the control that would write anyway is not the
  # primary one.
  mutate web "Overwrite anyway is the primary control" "$SWA" \
    '          <Button variant="quiet" disabled={pending} onClick={onOverwrite}>' \
    '          <Button variant="primary" disabled={pending} onClick={onOverwrite}>'

  # The desk computes no lock state, and says nothing where it knows nothing.
  mutate web "the lock line states a verdict about the reviewed set" "$LL" \
    '      This project keeps a reviewed set. Updating it is the project&rsquo;s own step.' \
    '      This pack is in the reviewed set, and the set is up to date.'
  mutate web "the lock line shows with no lock file listed" "$LL" \
    '  const listed = paths.some((path) => path === LOCK_FILE || path.endsWith(`/${LOCK_FILE}`))' \
    '  const listed = paths.length >= 0'

  # The mode is a search parameter for exactly one reason.
  mutate web "a mode toggle prompts about unsaved bytes" "$DGD" \
    '      dirty && currentLocation.pathname !== nextLocation.pathname' \
    '      dirty &&
      (currentLocation.pathname !== nextLocation.pathname ||
        currentLocation.search !== nextLocation.search)'
  mutate web "the rail dot is one global flag again" "$ABR" \
    '  if (next) dirtyPaths.set(path, true)
  else dirtyPaths.delete(path)' \
    '  dirtyPaths.clear()
  if (next) dirtyPaths.set(path, true)'

  # Carried low from phase 1: the pane's keyboard, held by the element focus
  # landed on rather than by text that happens to contain the row's name.
  mutate web "the packs pane never takes focus to the row it arrowed to" "$PN" \
    '              if (already instanceof HTMLElement) already.focus()' \
    '              if (already instanceof HTMLElement) void already'
fi

restore
echo
# A filter that matched nothing is not a clean run: it is a filter naming a row
# that no longer exists, and exiting 0 on it would report a mutation as verified
# when none was applied.
if [ -n "$only" ] && [ "$matched" -eq 0 ]; then
  echo "no row matched \"$only\" — nothing was checked" >&2
  exit 2
fi
echo "discriminating: $pass    not discriminating: $fail"
[ "$fail" -eq 0 ]
