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
  # **120s, and the number is not arbitrary.** The bound exists to turn a
  # mutation that hangs a handler into a reported hang rather than a stalled
  # table. It was 45s, which was already marginal: two relay rows remove an
  # *idle* deadline and their tests then wait out a deliberate twenty-second
  # stall, so the mutated suite runs about fifty seconds — and this branch's
  # forty new session tests pushed both rows over the line and reported
  # INCONCLUSIVE for a suite that was working. A deliberate wait inside a test
  # is not a hang; a real one is minutes, and 120s still catches it.
  out="$(go test ./internal/desk -count=1 -timeout 120s 2>&1)"
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
  # unbounded run would stall the whole table rather than report the hang. The
  # bound is well clear of a clean run — the suite takes under a minute — and
  # clear of the per-case ceiling in `vitest.config.ts` times the handful of
  # cases one mutation can hang, so a hang is still reported as a hang.
  out="$(timeout 900 npm --prefix web test 2>&1)"
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
  PJ=internal/desk/project.go
  PL=internal/desk/project_linux.go

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
  # Every replacement below keeps `shell` and `dirFile` in use: a mutation that
  # does not compile is not one the suite survived.
  mutate go "the runtime starts from the unresolved pathname" "$PL" \
    '	cmd := exec.CommandContext(ctx, shell, "-c", runtimeTrampoline, binary, "mcp")
	// **The documented contract**: this becomes descriptor 3 in the child,
	// with close-on-exec cleared for it there and nowhere else.
	cmd.ExtraFiles = []*os.File{dirFile}
	return cmd, nil' \
    '	_, _ = shell, dirFile
	cmd := exec.CommandContext(ctx, binary, "mcp")
	cmd.Dir = s.cfg.ProjectDir
	return cmd, nil'
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
  # Renamed and repaired: the guard reads a bearer session now, not a `?token=`
  # query, so the needle and the name both said something this file stopped
  # saying.
  mutate go "the guard drops the session check" "$F" \
    '	if !s.authorized(r) {
		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"no session: open the URL jpack-desk printed at startup, or present the launch secret as `Authorization: Bearer`")
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
  # Renamed and repaired: the guard's sentence changed with the credential.
  mutate go "the session and the origin share one code again" "$F" \
    '		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"no session: open the URL jpack-desk printed at startup, or present the launch secret as `Authorization: Bearer`")' \
    '		writeJSONCoded(w, http.StatusUnauthorized, CodeForbidden,
			"no session: open the URL jpack-desk printed at startup, or present the launch secret as `Authorization: Bearer`")'

  # ---- The assistant slot: key custody, and the one outbound request -------
  #
  # Every row here breaks a custody claim the README makes in words. The three
  # that matter most are the first three: where the key lives, who can read it,
  # and what happens to the old one when a new one is stored.
  # The three files the assistant slot lives in, declared together and before
  # any row uses one. They were declared where each section began, which put
  # `$CU` after its first use the moment a row was retargeted from
  # `assistant.go` into `custody.go` — and `set -u` turns that into a run that
  # fills in no rows at all.
  A=internal/desk/assistant.go
  CU=internal/desk/custody.go
  DF=internal/desk/deskfile.go
  MR=internal/desk/modelrelay.go

  mutate go "the key file is readable by everyone on the machine" "$CU" \
    '	custodyFileMode = 0o600' \
    '	custodyFileMode = 0o644'
  # A truncate-and-write keeps the file it opened, and leaves a window in which
  # a reader sees neither key whole.
  mutate go "the key is written in place rather than replaced" "$CU" \
    '	if err := s.secrets.Rename(name, assistantKeyName); err != nil {
		remove()
		return err
	}' \
    '	remove()
	if err := s.secrets.WriteFile(assistantKeyName, encoded, custodyFileMode); err != nil {
		return err
	}'
  # **Repaired**: the line gained the destination the key is bound to, which
  # is a scheme and a host and never a secret. The mutation is the same defect.
  mutate go "the key is logged beside the event" "$A" \
    '	s.log.Printf("desk: the assistant key was stored on this machine for %s", origin)' \
    '	s.log.Printf("desk: the assistant key %s was stored on this machine for %s", key, origin)'
  # **Repaired twice**: the answer is built by `keyState`, which now carries
  # the binding *and this desk's verdict about it* — so gofmt aligned the
  # literal and the needle moved with it. The mutation is the same defect it
  # always was: the value where the fingerprint belongs.
  mutate go "the key is answered to the page instead of its fingerprint" "$A" \
    'Fingerprint:      fingerprint(stored.key),' \
    'Fingerprint:      stored.key,'
  # Four and four discloses a short key in full. Eight and not one: at one,
  # `runes[:4]` on a shorter key panics, and a mutation that crashes the suite
  # has not been survived — it has not been tested.
  mutate go "a short key is fingerprinted into the open" "$A" \
    'const minFingerprintable = 12' \
    'const minFingerprintable = 8'
  # A newline in a credential is header injection in the outbound request.
  mutate go "a control character in a key is carried" "$A" \
    'func isControl(r rune) bool { return r < 0x20 || r == 0x7f }' \
    'func isControl(r rune) bool { return false }'
  mutate go "a key of any size is accepted" "$A" \
    'const maxKeyBytes = 4 << 10' \
    'const maxKeyBytes = 1 << 30'
  mutate go "an empty key is stored" "$A" \
    '	key := strings.TrimSpace(req.Key)
	if key == "" {' \
    '	key := strings.TrimSpace(req.Key)
	if false {'
  # A handler that stored and then refused would pass every status assertion.
  mutate go "the store runs without the token and origin guard" "$A" \
    'func (s *Server) handleAssistantKeyWrite(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
' \
    'func (s *Server) handleAssistantKeyWrite(w http.ResponseWriter, r *http.Request) {
'
  mutate go "a probe runs with no key to present" "$A" \
    '	if !stored.present {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantNoKey,' \
    '	if false {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantNoKey,'
  mutate go "any object takes any member at all" "$DF" \
    '		if !contains(allowed, member) {' \
    '		if false && !contains(allowed, member) {'
  mutate go "the tool allow-list is not consulted" "$DF" \
    '				if !contains(AssistantTools, name) {' \
    '				if false && !contains(AssistantTools, name) {'
  # An engine nobody certified, and a tier nothing implements. Both are
  # settings that read as a grant to whoever wrote them, so both refuse the
  # whole file rather than falling back to the default.
  mutate go "an engine nobody certified is accepted" "$DF" \
    '	problems = append(problems, oneOf(record, "assistant", "engine", AssistantEngines)...)' \
    ''
  mutate go "a thinking tier nothing implements is accepted" "$DF" \
    '	problems = append(problems, oneOf(record, "assistant", "thinking", AssistantThinkingTiers)...)' \
    ''
  # The corpus proves what a file *decodes to* and not only whether it is
  # accepted: the two sides could otherwise agree a file is legal and disagree
  # about what it means, and the fixtures would pass.
  mutate go "an accepted engine decodes to the default anyway" "$DF" \
    '	if named, ok := record["engine"].(string); ok && contains(AssistantEngines, named) {
		slot.engine = named
	}' \
    ''
  mutate go "an accepted thinking tier decodes to the default anyway" "$DF" \
    '	if named, ok := record["thinking"].(string); ok && contains(AssistantThinkingTiers, named) {
		slot.thinking = named
	}' \
    ''
  # A bearer credential in clear text over a network is a credential given away.
  mutate go "http is accepted off loopback" "$DF" \
    '	if parsed.Scheme == "http" &&
		(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1") {
		return ""
	}' \
    '	if parsed.Scheme == "http" {
		return ""
	}'
  # Go strips Authorization across hosts and knows nothing about x-api-key.
  # **The binding, and the three ways it could stop holding.** Round 1: the
  # page can write the desk-level file, so it can name any endpoint it likes —
  # what keeps "the destination cannot come from the page" true is that the
  # credential travels only to the destination it was entered for.
  mutate go "the relay presents the key to an endpoint it was not entered for" "$MR" \
    '	if reason := bindingProblem(stored, endpoint); reason != "" {' \
    '	if reason := ""; reason != "" {'
  mutate go "the probe presents the key to an endpoint it was not entered for" "$A" \
    '	if reason := bindingProblem(stored, endpoint); reason != "" {' \
    '	if reason := ""; reason != "" {'
  # A write that moved the host must say a new key is needed; a page told
  # otherwise would report a working assistant that refuses every request.
  mutate go "a write across a host change reports no new key is needed" "$A" \
    '		rebind = landed.Endpoint == nil || bindingProblem(stored, *landed.Endpoint) != ""' \
    '		rebind = false'
  # A file this build cannot read as a bound key must not be read as an
  # unbound one: a credential with no binding is the state this ends.
  mutate go "an unversioned key file is read as a bare key" "$CU" \
    '	if jerr := json.Unmarshal(data, &record); jerr != nil || record.Version != storedKeyVersion {' \
    '	if jerr := json.Unmarshal(data, &record); false && jerr != nil {'
  # The origin is scheme and host: a binding to the whole URL would lapse on
  # adding `?route=eu`, and a binding to the host alone would not notice a
  # scheme change.
  mutate go "a binding compares the whole configured URL" "$A" \
    '	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), true' \
    '	return strings.ToLower(parsed.String()), true'
  mutate go "the probe follows a redirect with the credential" "$A" \
    '	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },' \
    '	CheckRedirect: nil,'
  mutate go "the probe is unbounded" "$A" \
    '	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()' \
    '	ctx, cancel := context.WithCancel(ctx)
	defer cancel()'
  mutate go "a refused credential is reported as reachable" "$A" \
    '	reachable := response.StatusCode >= 200 && response.StatusCode < 300' \
    '	reachable := response.StatusCode < 500'
  # Deliberately gone rather than retargeted: two rows that broke the old
  # `scrub` and the old 200-character bound on a quoted sentence. Neither
  # exists any more — nothing an endpoint writes is repeated at all — and the
  # property that replaced them is held by "the endpoint's own body travels to
  # the page" below. Named here so their absence is a statement.
  # Retargeted: the credential is attached from one table now, read by the
  # probe and by the model relay, so this row breaks the table rather than one
  # of its two callers. Breaking a copy would leave the other one right.
  mutate go "the anthropic protocol is given the other one's header" "$MR" \
    '	case "anthropic":
		return "x-api-key", key, true' \
    '	case "anthropic":
		return "Authorization", "Bearer " + key, true'
  mutate go "the version header this protocol requires is dropped" "$A" \
    '		request.Header.Set("anthropic-version", "2023-06-01")' \
    '		request.Header.Del("anthropic-version")'
  mutate go "the anthropic probe is not the smallest request" "$A" \
    '			"max_tokens": 1,' \
    '			"max_tokens": 1024,'
  # "There is none, and it would be here" is an answer, and Admin needs the path.
  mutate go "an absent desk-level file is a refusal" "$A" \
    '	if !present {
		writeJSON(w, http.StatusOK, DeskLevelConfig{
			Path: path, Present: false, Project: s.projectPaths(), Runtime: s.runtimePaths()})
		return
	}' \
    '	if !present {
		writeJSONCoded(w, http.StatusNotFound, CodeNotFound, "no desk-level configuration file")
		return
	}'
  mutate go "a relative XDG_CONFIG_HOME is honoured" "$A" \
    '	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {' \
    '	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {'

  # ---- Custody: the directory a credential is kept in ---------------------
  #
  # Each of these is an arrangement another local user can make in a
  # configuration tree they can write to. The first four were all possible at
  # once before the store was validated and pinned.
  mutate go "a symlinked directory is walked through" "$CU" \
    '	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf(
			"%s is a symbolic link; a key is not kept anywhere reached through one", path)
	}' \
    ''
  mutate go "a directory owned by somebody else is ours" "$CU" \
    '	if ours && owner != me {' \
    '	if false && owner != me {'
  # The sticky exception is what makes /tmp usable; without the guard around
  # it, every world-writable ancestor is admitted.
  mutate go "any world-writable ancestor is admitted" "$CU" \
    '		if info.Mode()&fs.ModeSticky == 0 {' \
    '		if false {'
  mutate go "the ancestors are never walked" "$CU" \
    '	if err := checkAncestors(parent); err != nil {
		return &assistantStore{dir: dir, problem: err}
	}' \
    ''
  # The narrowing decision moved into `needsNarrowing` when the special bits
  # were added to it, so one row now breaks it for both of the desk's own
  # directories rather than one row per site.
  mutate go "the desk's own directories are left at whatever mode they had" "$CU" \
    '	return mode.Perm() != custodyDirMode ||
		mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0' \
    '	return false'
  # `Perm()` masks the special bits away, so comparing it alone left a 02700
  # directory setgid while the README said the mode was 0700.
  mutate go "the special mode bits are not narrowed away" "$CU" \
    '	return mode.Perm() != custodyDirMode ||
		mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0' \
    '	return mode.Perm() != custodyDirMode'
  # The key file itself, which is the name an attacker plants a link at.
  mutate go "a symlinked key file is followed" "$CU" \
    '	if info.Mode()&fs.ModeSymlink != 0 {
		return none, fmt.Errorf(
			"%s is a symbolic link rather than a key, and was not read",' \
    '	if false {
		return none, fmt.Errorf(
			"%s is a symbolic link rather than a key, and was not read",'
  # **One rule, one row.** It used to be written out at both the name and the
  # descriptor, which made each copy invisible: break one and the other
  # refuses the same file a moment later, so the table reported an unheld
  # safeguard while two things were holding it. It lives in `ownerOnlyFile`
  # now, and this breaks that.
  mutate go "a key anyone on the machine can read is still the key" "$CU" \
    '	if perm := mode.Perm(); perm&0o077 != 0 {' \
    '	if perm := mode.Perm(); false && perm&0o077 != 0 {'
  mutate go "a key that is not a regular file is read anyway" "$CU" \
    '	if !mode.IsRegular() {
		return fmt.Errorf("%s is not a regular file, and was not read", path)
	}' \
    ''
  # **This row replaced one that did not discriminate**, and the replacement is
  # the interesting part. The row used to remove `O_NOFOLLOW` from the open, on
  # the belief that the flag was what refused a link swapped in after the
  # check. Every test survived it: `os.Root` resolves the final component
  # itself and the flag never reaches a syscall that would refuse. What
  # actually closes the window is comparing the opened file to the inspected
  # one by identity, so that is what is broken here.
  mutate go "the opened key is not checked against the inspected one" "$CU" \
    '	if !os.SameFile(info, opened) {
		return none, fmt.Errorf(' \
    '	if false {
		return none, fmt.Errorf('
  # The same check on the file that names where a credential goes.
  mutate go "the opened configuration is not checked against the inspected one" "$CU" \
    '	if !os.SameFile(info, opened) {
		return false, nil, withCode(CodeForbidden, fmt.Errorf(' \
    '	if false && !os.SameFile(info, opened) {
		return false, nil, withCode(CodeForbidden, fmt.Errorf('
  # Deliberately not mutated: the `usable()` guards inside the store. Removing
  # one does not produce a wrong answer, it produces a nil-pointer panic — a
  # refused store holds no descriptors at all — and a mutation that crashes the
  # suite has not been survived, it has not been tested. The guards are
  # asserted directly instead, by TestARefusedStoreTouchesTheFilesystemNotAtAll
  # and TestAnUnusableStoreLeavesTheRestOfTheDeskAlone. Named here so that
  # their absence from this table is a statement rather than an oversight.

  # ---- The one contract, and the credential in it -------------------------
  mutate go "the control check runs after the value is trimmed" "$A" \
    '	if index := strings.IndexFunc(req.Key, isControl); index >= 0 {' \
    '	if index := strings.IndexFunc(strings.TrimSpace(req.Key), isControl); index >= 0 {'
  # The HIGH: a file the browser refuses must authorise nothing.
  mutate go "a refused configuration still authorises a probe" "$A" \
    '	decoded := decodeDeskFile(data)
	if decoded.refused() {' \
    '	decoded := decodeDeskFile(data)
	if false {'
  # The body is discarded, and the vocabulary is what travels. Mutated as one
  # block, because reading the bytes into a variable and never using them is
  # not the defect — putting them in the answer is.
  mutate go "the endpoint's own body travels to the page" "$A" \
    '	_, _ = io.Copy(io.Discard, response.Body)
	reachable := response.StatusCode >= 200 && response.StatusCode < 300
	diagnostic := ""
	if !reachable {
		// One word, chosen from the status. Never the bytes just discarded.
		diagnostic = statusDiagnostic(response.StatusCode)
	}' \
    '	body, _ := io.ReadAll(response.Body)
	reachable := response.StatusCode >= 200 && response.StatusCode < 300
	diagnostic := ""
	if !reachable {
		diagnostic = string(body)
	}'
  # A configured URL may carry a query string, and a query string is a place
  # people put credentials.
  mutate go "the whole endpoint URL is logged" "$A" \
    '		loggableOrigin(endpoint.url), result.Status, result.LatencyMs)' \
    '		endpoint.url, result.Status, result.LatencyMs)'
  mutate go "the logged origin carries the rest of the URL" "$A" \
    '	return parsed.Scheme + "://" + parsed.Host' \
    '	return parsed.String()'
  mutate go "a member refused as a credential is also refused as unknown" "$DF" \
    '		if problem.Reason != keysAreNeverInConfiguration && credentialed[problem.Key] {
			continue
		}' \
    ''
  mutate go "the credential scan never runs" "$DF" \
    '	problems = append(problems, scanForKeys("", parsed)...)' \
    ''
  # Round 1: only the page's half of the query was checked, and the configured
  # half — which `PUT /api/desk-config` had just made page-writable — travelled
  # upstream byte for byte on every later call.
  mutate go "a configured query is not held to any rule" "$DF" \
    '	if reason := endpointQueryProblem(parsed.RawQuery); reason != "" {' \
    '	if reason := ""; reason != "" {'
  # A configured `alt` would be a second copy of the one pair the relay admits
  # from the page: a query two parsers count differently.
  # **Repaired**: the comparison folds case since round 2.
  mutate go "a configured query may use a name the relay reserves" "$DF" \
    '		if containsFold(reservedQueryNames, decoded) {' \
    '		if false {'
  # "A key is never written into configuration" cannot be a rule about members
  # only while a URL sits beside them.
  mutate go "a configured query may carry a credential" "$DF" \
    '		if isCredentialQueryName(decoded) {' \
    '		if false {'
  # Round 2: `url.QueryUnescape("%FF")` answers one byte and no error while the
  # browser's decoder throws, so the chassis accepted a file the page refused —
  # and could send the key on the strength of it.
  # Only the UTF-8 half is broken: dropping `decoded` altogether leaves it
  # unused and does not compile, and a mutation that does not compile has not
  # been survived — it has not been tested.
  mutate go "a configured query is read as bytes the browser cannot read" "$DF" \
    '			if err != nil || !utf8.ValidString(decoded) {' \
    '			if err != nil || (false && !utf8.ValidString(decoded)) {'
  # Round 2: the reserved names were compared case-sensitively, so `?ALT=sse`
  # was accepted and the relay added its own pair beside it.
  mutate go "a reserved query name in another case is accepted" "$DF" \
    '		if containsFold(reservedQueryNames, decoded) {' \
    '		if contains(reservedQueryNames, decoded) {'
  mutate go "a key smuggled into the URL is accepted" "$DF" \
    '	if parsed.User != nil {' \
    '	if false {'
  # **The desk-level write, and the four ways it could stop being narrow.**
  # This is the one route that changes a configuration file, and the argument
  # for having it is entirely in these four checks.
  #
  # The round trip is the whole safety argument: what is decoded is the file
  # this desk would store, so a key-shaped member or an unknown kind refuses
  # the write rather than being written and then reported as refused.
  mutate go "a configuration write is not decoded before it lands" "$A" \
    '	decoded := decodeDeskFile(composed)
	if decoded.refused() {' \
    '	decoded := decodeDeskFile(composed)
	if false && decoded.refused() {'
  # **The UTF-8 rule, broken where it has one spelling.**
  #
  # Round 1: Go's decoder replaces an invalid byte inside a string while
  # decoding and `json.RawMessage` keeps the original, so a `0xff` decoded
  # clean and would have been written into a file every later read refuses.
  # The repair applies the rule three times on this route — to the request
  # body, to the composed file before staging, and to the read-back — which is
  # wanted (each covers bytes the others never see: a body, a current file
  # carried across, and whatever actually landed).
  #
  # **A row that broke one of the three reported NOT DISCRIMINATING, and it was
  # right to**: with the body check gone the composed check refuses the same
  # request, with the same status and the same code. Three applications of one
  # rule are not three safeguards. So the row is the rule itself — the
  # predicate all three call — which is the same shape `ownerOnlyFile` took for
  # the same reason, and it is the row that can actually fail.
  mutate go "bytes that are not text are treated as text" "$DF" \
    'func validUTF8(data []byte) bool { return utf8.Valid(data) }' \
    'func validUTF8(data []byte) bool { _ = utf8.Valid(data); return true }'
  # Round 1: the composed file was never bounded, so an envelope inside the
  # request bound could compose past the bound every reader applies.
  mutate go "a composed configuration is not bounded before it is staged" "$A" \
    '	if len(composed) > maxDeskConfigBytes {' \
    '	if false {'
  # One JSON value, and nothing behind it: a body two readers disagree about is
  # the class this desk refuses everywhere else.
  mutate go "a configuration write accepts a second value behind the first" "$A" \
    '	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {' \
    '	if false {'
  # No override on this route: a file that moved under the writer is refused,
  # because this is the file that names where a credential goes.
  #
  # **Repaired after round 2 reported it NOT DISCRIMINATING, correctly.** The
  # comparison was written out twice — once against the bytes the transaction
  # read and again immediately before the rename — so breaking either copy left
  # the other answering the same 409 with the same digests. Two spellings of
  # one rule are invisible to a harness that breaks one of them. There is one
  # predicate now, and this is it.
  mutate go "a configuration write ignores the digest it was given" "$A" \
    'func deskConfigUnmoved(ifMatch, actual string) bool {
	return strings.EqualFold(strings.TrimSpace(ifMatch), actual)
}' \
    'func deskConfigUnmoved(ifMatch, actual string) bool {
	_, _ = ifMatch, actual
	return true
}'
  # And the half the second comparison used to stand in for: a request already
  # known to be stale must never reach the disk at all. "Nothing was written"
  # and "nothing was staged" are different claims.
  mutate go "an already-stale write is staged before it is refused" "$A" \
    '	if !deskConfigUnmoved(*req.IfMatch, actual) {
		return http.StatusConflict, conflict{' \
    '	if false {
		return http.StatusConflict, conflict{'
  # Through the pinned custody descriptor, staged and renamed, published at the
  # mode this desk chose — `os.WriteFile` follows a name and keeps whatever
  # mode it finds, which is the observable difference the suite measures.
  # **Repaired**: the call gained the pre-rename revalidation round 1 asked for.
  # The mutation is the same defect — a write by pathname rather than through
  # the pinned descriptor, which follows a name and keeps whatever mode it
  # finds.
  mutate go "a configuration write goes round the custody root" "$A" \
    '	var moved *deskConfigMoved
	if err := s.assistant.writeConfigFile(composed, func() error {' \
    '	var moved *deskConfigMoved
	if err := func(data []byte, _ func() error) error {
		return os.WriteFile(path, data, 0o600)
	}(composed, func() error {'
  # Every other member is carried across by its own bytes, in its own place: a
  # rewrite of one member must not restate the rest of a file somebody wrote.
  #
  # **Repaired**: the needle named the map-and-order pair that round 1 replaced
  # with a single ordered walk. The mutation is the same defect — the other
  # members gone — and the version is kept so the composed file still decodes
  # and the row measures the preservation rather than the round trip.
  mutate go "a rewrite drops the other members of the file" "$A" \
    '		members = append(members, deskMember{name: key, raw: value})' \
    '		if key == "deskConfigVersion" {
			members = append(members, deskMember{name: key, raw: value})
		}'
  # Round 1: every retained member went through `json.Indent`, so the
  # byte-for-byte claim held only for a file already in the shape that emits.
  mutate go "a retained member is reflowed rather than copied" "$A" \
    '		out.Write(member.raw)' \
    '		var reflowed bytes.Buffer
		if ierr := json.Indent(&reflowed, member.raw, "  ", "  "); ierr == nil {
			member.raw = json.RawMessage(reflowed.String())
		}
		out.Write(member.raw)'
  # Round 2: the walk stopped as soon as there was no next member, which is
  # true of a truncated object as well as a closed one — so a half-written file
  # was rewritten into well-formed JSON with its malformed bytes dropped.
  mutate go "a file that is not one whole object is repaired on rewrite" "$A" \
    '	closing, err := decoder.Token()
	if err != nil {
		return nil, "", fmt.Errorf("the object is not closed: %w", err)
	}' \
    '	closing, err := json.Token(json.Delim(0x7d)), error(nil)
	if false {
		return nil, "", err
	}'
  # Round 1: values came from a map and positions from the walk, so two
  # spellings of one name became the last value at the first position.
  mutate go "a duplicate top-level member is collapsed rather than refused" "$A" \
    '		if seen[key] {
			return nil, key, nil
		}' \
    ''
  # Round 1: the digest was compared at the read and never again, so an
  # ordinary editor writing between that and the rename was overwritten.
  mutate go "a configuration write does not look again before it publishes" "$CU" \
    '	if stillMatches != nil {
		if err := stillMatches(); err != nil {
			remove()
			return err
		}
	}' \
    ''
  # Caught by the live drive: the protocol path was appended to the whole URL
  # string, so a configured query put it after the query.
  #
  # **Repaired**: the body this named moved into `probeAddressWithQuery` when
  # the gemini arm needed a parameter of its own, and the needle went stale
  # silently. The mutation is the same defect — the path after the query — and
  # it now reproduces it for both arms at once.
  mutate go "the protocol path is appended to the whole URL" "$A" \
    '	appendPath(parsed, suffix)
	if pair != "" {
		parsed.RawQuery = appendQueryPair(parsed.RawQuery, pair)
		parsed.ForceQuery = false
	}
	return parsed.String()' \
    '	_ = parsed
	if pair == "" {
		return base + suffix
	}
	return base + suffix + "?" + pair'
  # `u.Path` is the decoded path; writing it alone re-encodes %2F into a
  # separator and sends the credential to a different resource.
  mutate go "an escaped path is re-encoded on the way out" "$A" \
    '	escaped := strings.TrimRight(u.EscapedPath(), "/") + suffix' \
    '	escaped := strings.TrimRight(u.Path, "/") + suffix'
  mutate go "a fragment on the endpoint is accepted" "$DF" \
    '	if parsed.Fragment != "" || strings.Contains(raw, "#") {' \
    '	if false {'
  mutate go "the credential scan does not look inside arrays" "$DF" \
    '	case []any:
		for index, element := range typed {' \
    '	case []any:
		for index, element := range typed[:0] {'

  # ---- The model relay: the page's traffic, this machine's key -------------
  #
  # Every row here breaks a sentence the README makes about what the relay
  # takes off a request, what it puts on, and what it will not carry. The
  # measurement in each case is at the endpoint, which is why they discriminate
  # at all: a test that asserted the handler called `Del` would survive most of
  # these.
  # **Two rows are gone and this one replaced them**, which is the point of the
  # shape that replaced the denylist. There is no loop deleting credential
  # headers any more and no pair of `Del`s for Origin and Referer: nothing
  # travels unless it is on the allow-list, so breaking that one check is the
  # only way to make any of them travel. Rows for code that no longer exists
  # would have mutated nothing, and rows for redundant `Del`s would have
  # reported "nothing failed" for ever.
  mutate go "any header at all is forwarded to the endpoint" "$MR" \
    '				if !relayedRequestHeader(header) {' \
    '				if false {'
  # The one thing this route adds. Without it the endpoint is called with no
  # credential at all, which is a page that cannot work rather than a page that
  # is unsafe — but it is the sentence the whole route exists for.
  # Repaired: the needle named `out.Header.Set`, which this file stopped saying
  # when the outbound headers became an allow-list built into a new set. A row
  # that cannot apply is a row reporting nothing.
  mutate go "the configured key is never attached" "$MR" \
    '			carried.Set(name, value)' \
    '			_, _ = name, value'
  # **Two rows are gone and this one replaced them.** They broke a *strip* —
  # the page's query forwarded with this chassis' token taken out of it — and
  # that arrangement leaked the token three times, three ways, to three
  # reviewers: `%74oken` (the guard decodes names and a raw compare did not),
  # `;` (Go rejects such a pair and some servers split on it), and `Token`
  # (this desk compared case-sensitively and ASP.NET Core folds case). Each fix
  # was a better comparison and the next parser disagreed somewhere else, so
  # there is no strip any more.
  #
  # **Named for what it breaks.** The page's query is never *copied* outbound
  # at all: `appendPath` builds the address from the configured URL and a
  # validated path suffix, and the only thing that can join it is the one closed
  # literal. What `relayQueryProblem` does is **refuse the request**, so a
  # skipped refusal produces a request that reaches the endpoint when it should
  # have reached nothing — carrying only the configured query, which is what
  # makes the endpoint's **arrival count** the measurement and not its
  # `RawQuery`. See `TestNothingOfThePagesQueryReachesTheEndpoint`.
  #
  # The needle first named `sessionTokenParameter`, a query this chassis no
  # longer has; a second draft admitted the raw query as the stream pair, which
  # the kind check then refused a line later, so nothing travelled either way.
  mutate go "a page query no longer refuses the request" "$MR" \
    '		return "", "a relayed request carries no query of the page'"'"'s own: nothing of it is " +' \
    '		continue
		_ = "a relayed request carries no query of the page'"'"'s own: nothing of it is " +'
  mutate go "the relayed path is never validated" "$MR" \
    '	if reason := relaySuffixProblem(suffix); reason != "" {' \
    '	if reason := ""; reason != "" {'
  mutate go "a relayed path may leave the endpoint's path space" "$MR" \
    '		if segment == "." || segment == ".." {' \
    '		if false {'
  # No percent sign is what makes the escaped and the unescaped reading of an
  # accepted suffix the same string, so `%2e%2e%2f` cannot be a dot segment in
  # a costume and `a%2Fb` cannot become two.
  mutate go "any character at all is a relayed path" "$MR" \
    '			if !relayPathRune(r) {' \
    '			if false && !relayPathRune(r) {'
  # Renamed, because the old name was a claim the mutation could not break:
  # disabling the fast rejection leaves the body bounded at the reader, so the
  # status is still 413 and nothing outbound still happens. What the fast path
  # decides is *when* the weighing happens — before the store, the endpoint and
  # the key are read — and a desk with none of those answers `too-large`
  # instead of `assistant-unconfigured`, which is what the test measures.
  mutate go "an over-size body is weighed only after the desk is read" "$MR" \
    '	if r.ContentLength > maxRelayBody {' \
    '	if false {'
  mutate go "an undeclared body length is unbounded" "$MR" \
    '	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRelayBody))' \
    '	body, err := io.ReadAll(r.Body)'
  # **Read whole before a byte is dispatched**, which is what makes "refused,
  # never truncated" true rather than nearly true: bounding at the reader while
  # the proxy was already streaming sent the endpoint the first eight
  # mebibytes of a request this desk then refused.
  mutate go "an over-size body is streamed upstream before it is refused" "$MR" \
    '	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRelayBody))
	_ = controller.SetReadDeadline(time.Time{})' \
    '	body, err := []byte(nil), error(nil)
	r.Body = http.MaxBytesReader(w, r.Body, maxRelayBody)
	_ = controller.SetReadDeadline(time.Time{})'
  # **The answer can carry the key back**, and the page must not receive it.
  # The credential this desk sends is the endpoint's own, so an endpoint that
  # echoes what it was sent would otherwise put the machine-held key into the
  # browser — the single thing this route exists to prevent.
  mutate go "the key is handed back in an answer's header" "$MR" \
    '	for _, name := range reflectedCredentialHeaders {
		header.Del(name)
	}' \
    ''
  # The name list cannot cover a header nobody named; the value comparison is
  # what does, and it is its own row because it is its own rule.
  #
  # Repaired: the needle named `if value == key {`, which stopped being a line
  # of its own when the length rule joined it in one predicate. Only the
  # exact-match arm is broken here — the substring arm has its own row below —
  # so what fails is the short-key case, where exact equality is the whole rule.
  mutate go "a header whose value is the key is handed back" "$MR" \
    '			if value == key || (long && strings.Contains(value, key)) {' \
    '			if (false && value == key) || (long && strings.Contains(value, key)) {'
  # The page and this chassis share an origin, so a cookie from the endpoint
  # would be stored against the desk.
  mutate go "the endpoint may set a cookie on the desk's origin" "$MR" \
    '"X-Api-Key", "Api-Key", "X-Goog-Api-Key", "Set-Cookie",' \
    '"X-Api-Key", "Api-Key", "X-Goog-Api-Key",'
  # A queue rather than a bound: the request past the fourth waits instead of
  # being told. The test uses a client with a timeout for exactly this row, so
  # a queued request fails the suite rather than hanging it.
  mutate go "the relay queues rather than bounds" "$MR" \
    '	select {
	case s.relaySlots <- struct{}{}:
		defer func() { <-s.relaySlots }()
	default:' \
    '	s.relaySlots <- struct{}{}
	defer func() { <-s.relaySlots }()
	if false {'
  # **Held by the declared-length test and not by the SSE one.**
  # `httputil.ReverseProxy` flushes immediately on its own for a
  # `text/event-stream` body and for one of unknown length, whatever
  # FlushInterval says — so the SSE test survives this mutation and reported a
  # safeguard nothing was holding. What FlushInterval decides is the remaining
  # case: a streamed answer that declares its Content-Length.
  mutate go "a relayed answer is buffered rather than streamed" "$MR" \
    '		FlushInterval: -1,' \
    '		FlushInterval: 0,'
  mutate go "a stalled stream is never cut" "$MR" \
    '			response.Body = boundedByIdle(response.Body, cancel)' \
    ''
  mutate go "one relayed request is unbounded in time" "$MR" \
    '	ctx, cancel := context.WithTimeout(r.Context(), relayDeadline)' \
    '	ctx, cancel := context.WithCancel(r.Context())'
  # Four clients that authenticate and then stop reading held every slot for
  # ever: the two deadlines cancel the *upstream* context, and neither of them
  # ends a write to a page that is not listening.
  mutate go "a page that stops reading holds its slot for ever" "$MR" \
    '	proxy.ServeHTTP(&deadlineWriter{
		ResponseWriter: w,
		controller:     controller,
		until:          deadline,
	}, r)' \
    '	proxy.ServeHTTP(w, r)'
  # **Retired, with its reason, rather than dropped.** Two parsers disagreed
  # about `;` and the desk's own session token went to the endpoint: Go read
  # `x=1;token=T&token=T` as one `token` parameter and accepted it, while a
  # strip that removed the pair it could see preserved the first one for a
  # server that does split on `;`. That row broke the `;` check and watched the
  # suite fail.
  #
  # It measures nothing now, and this run proved it: since the launch exchange
  # **no pair of the page's own is forwarded at all**, so a query containing a
  # semicolon is refused by the general rule whether or not the `;` check is
  # there. The check is kept because its sentence is the specific one, and the
  # row is named here so that its absence from the table is a statement rather
  # than an oversight.
  # The proxy copies the endpoint's trailers to the page after the body, past
  # every filter on this route.
  mutate go "the endpoint's trailers are forwarded to the page" "$MR" \
    '			response.Trailer = nil
			response.Header.Del("Trailer")' \
    ''
  mutate go "a trailer staged by the proxy still reaches the page" "$MR" \
    '	for name := range header {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			header.Del(name)
		}
	}' \
    ''
  # A 1xx reaches the page through a client trace, before ModifyResponse runs.
  mutate go "an informational response is forwarded to the page" "$MR" \
    '	if status < 100 || status >= 200 {' \
    '	if true {'
  # A long key is looked for anywhere in a value; below twelve bytes it is
  # exact equality, because a short key is a substring of ordinary text.
  mutate go "a long key echoed inside a value is handed back" "$MR" \
    '			if value == key || (long && strings.Contains(value, key)) {' \
    '			if value == key || (false && long && strings.Contains(value, key)) {'
  # **The cap, which is what the finding was about**: without it every write
  # takes a fresh idle bound, so an idle bound longer than the overall deadline
  # means the overall deadline bounds nothing and a stalled page holds its slot
  # for the idle bound instead.
  mutate go "a write is not capped by the overall deadline" "$MR" \
    '	if next.After(d.until) {
		next = d.until
	}' \
    ''
  # **Deliberately not mutated: `relayFinalWrite` itself.** Replacing the five
  # seconds with the idle bound changes nothing any test can observe, and the
  # reason is worth writing down rather than leaving as a row that reports
  # "nothing failed" for ever. The one write it governs is the refusal envelope
  # on the error path — two hundred bytes onto a socket whose buffer is empty by
  # construction, because that path runs *instead of* a body rather than after
  # one. A client would have to have a full receive window and no body to have
  # filled it, which is not a state this route can produce. The cap above is the
  # safeguard; the constant is the size of the tail behind it, and it is asserted
  # by reading rather than by measurement.
  # **The colon exception, and the three ways it could stop being closed.**
  # The part after a colon is a verb: an open list would let whoever holds the
  # session token ask the configured endpoint to *do* something nobody wrote
  # down, with the stored credential attached.
  # Only the method half is broken: leaving `method` unused would not compile,
  # and a mutation that does not compile has not been survived — it has not
  # been tested.
  mutate go "the method after a colon is not held to the list" "$MR" \
    '			if index != len(segments)-1 || name == "" ||
				!contains(relayPathMethods, method) {' \
    '			if index != len(segments)-1 || name == "" ||
				(false && !contains(relayPathMethods, method)) {'
  # Round 1: the rule was written per segment and never asked where the segment
  # was, so `v1beta/a:countTokens/b` was forwarded with the credential.
  mutate go "a colon method is accepted in a non-final segment" "$MR" \
    '			if index != len(segments)-1 || name == "" ||' \
    '			if (false && index != len(segments)-1) || name == "" ||'
  # The one pair the page may send is admitted for one wire and refused for the
  # other two, which carry streaming in the request body and need none.
  mutate go "the stream pair is admitted on every kind" "$MR" \
    '	if extra != "" && extra != relayExtraQueryPair(endpoint.kind) {' \
    '	if false {'
  # At most once: `alt=sse&alt=sse` is a query two parsers could count
  # differently, which is the whole class this rule exists to keep out.
  # Repaired: the condition lost an indentation level when the `token` branch
  # around it went, so the needle no longer matched. The property is unchanged.
  mutate go "a second copy of the stream pair is admitted" "$MR" \
    '			parameter == relayStreamPair && extra == "" {' \
    '			parameter == relayStreamPair {'
  mutate go "the relay's log line carries the whole address" "$MR" \
    '	s.log.Printf("desk: assistant relay %s answered %d", loggableOrigin(endpoint.url), status)' \
    '	s.log.Printf("desk: assistant relay %s %s answered %d", endpoint.url, suffix, status)'
  mutate go "the relay runs without the session guard" "$MR" \
    '	if !s.guard(w, r) {
		return
	}' \
    ''

  # ---- The one relayed answer the desk reads ------------------------------
  #
  # A listing is a set of strings this desk renders — into a picker, into page
  # state, into a field somebody can copy — so an endpoint that reflects its own
  # credential as a model id would hand the machine-held key to the browser
  # through the route that exists so it never gets there. The page cannot help:
  # it has never held the key and could not recognise one.
  # `false &&` rather than `false`: the mutation has to keep `typed` used, or it
  # does not compile — and a mutation that does not compile has not been
  # survived, it has not been tested.
  mutate go "a model listing is forwarded without being scanned" "$MR" \
    '			if carries(typed) {
				return errListingCarriesKey
			}' \
    '			if false && carries(typed) {
				return errListingCarriesKey
			}'
  # A listing this desk cannot read to the end is one it cannot say anything
  # about, and forwarding the part it did read is the truncation every other
  # bound here refuses.
  # **A scan that cannot read a body cannot clear it.** A decode error used to
  # fall back to the raw bytes, so plain text, an empty answer, a truncated
  # document or malformed JSON with an escaped credential past the error was
  # forwarded whenever the literal key bytes happened to be absent.
  mutate go "a listing this desk cannot read is forwarded anyway" "$MR" \
    '	if values != 1 || depth != 0 {
		return errListingNotJSON
	}' \
    '	if false {
		return errListingNotJSON
	}'
  mutate go "a decode failure is not a refusal" "$MR" \
    '		if err != nil {
			return errListingNotJSON
		}' \
    '		if err != nil {
			break
		}'
  # **The half of the answer no bound reached.** `boundedByIdle` is installed
  # once the transport has a response, so an endpoint that accepted a request
  # and then sent nothing at all — not a header, not a byte — was held by the
  # overall deadline, and four of them exhausted every slot for ten minutes.
  mutate go "the wait for the first byte is bounded by nothing but the overall deadline" "$MR" \
    '		Transport:     beforeTheFirstByte{inner: relayTransport, cancel: cancel},' \
    '		Transport:     relayTransport,'
  # A number outside float64 is a valid JSON document, and whether Go can hold
  # it is not a fact about the endpoint's listing.
  mutate go "a listing is refused for a number Go cannot hold" "$MR" \
    '	decoder.UseNumber()' \
    ''
  # The listing branch buffers rather than streams, so the wrapper that bounds
  # every other answer never reached it: one byte and a stall held a slot until
  # the overall deadline.
  mutate go "a stalled listing is bounded only by the overall deadline" "$MR" \
    '				bounded := boundedByIdle(response.Body, cancel)' \
    '				bounded := response.Body'
  mutate go "an over-long listing is forwarded as far as it was read" "$MR" \
    '				if len(read) > maxListingBody {
					return errListingTooLarge
				}' \
    '				if false {
					return errListingTooLarge
				}'

  # ---- Chunk 6a: the default project, and the launch that reads it -------
  #
  # `project.file` is the one member of this file that decides something before
  # there is a server: it names which project a desk launched with no directory
  # argument opens. Each row below breaks one of the three things that makes
  # that safe to have.
  LA=internal/desk/launch.go

  # **The whole point of the member.** A fallback that ignored it would open
  # whatever directory the process happened to start in and report success.
  mutate go "the launch fallback ignores the configured file" "$LA" \
    '	chosen, err := usableProjectDir(deskFile.file)' \
    '	chosen, err := projectChoice{dir: "."}, error(nil)'

  # **Read before there is a working directory to resolve one against.** A
  # relative path accepted here is a default project that means a different
  # directory depending on where the desk was launched from.
  mutate go "the default project accepts a relative path" "$DF" \
    '	if !absolutePath(trimmed) {' \
    '	if false {'

  # **A member that is absent is untouched.** Two Admin cards write two members
  # of one file and neither sends the other's; a compose that replaced both
  # would have each card silently overwrite the other's slot with nothing.
  mutate go "an unsent member is written as well as the one that was sent" "$A" \
    '		if len(member.raw) == 0 {
			continue
		}' \
    '		if len(member.raw) == 0 {
			member.raw = json.RawMessage("null")
		}'

  # ---- Round 1: what a page may persist, and what a launch may honour ----

  # **The equality check is the whole ruling.** Without it a page can name any
  # path, and the value it writes chooses the root of the next launch — an
  # authority the page itself never had.
  # The replacement keeps `text` in use, because a mutation that does not
  # compile is not one the suite survived — it is one nothing ran.
  mutate go "the page may nominate a project it is not running in" "$A" \
    '	if text == s.projectPaths().File {' \
    '	if text != "" {'

  # **A default that is not there is not a project.** Honouring one takes the
  # parent of a name nobody wrote a file at — the review's `/jpack-desk.json`
  # is exactly that shape.
  #
  # **Round 2 found the first spelling of this proving nothing.** It replaced
  # the resolution with `resolved := file`, and a nonexistent path then failed
  # at the very next `Lstat` — so the mutant refused the same paths and the
  # test killed it on a diagnostic word. This one *honours* the missing file,
  # returning its parent, so the test has to observe a nonexistent default
  # being opened.
  mutate go "the launch honours a configured file that is not there" "$LA" \
    '	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return projectChoice{}, fmt.Errorf("it could not be resolved: %w", err)
	}' \
    '	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return projectChoice{dir: filepath.Dir(file)}, nil
	}'

  # **Absolute on this host, not in the shared spelling.** A drive-letter path
  # is a relative one here, so without this the desk resolves it against
  # whatever directory it was launched from.
  #
  # **Round 2 found the first spelling of this proving nothing** either: the
  # test used a temporary directory with nothing in it, so the mutant still
  # refused — at the basename check, for a path that does not exist. The test
  # now builds `<cwd>/C:/p/jpack-desk.json`, so removing this check opens a
  # real launch-directory-relative project and the row is behavioural.
  mutate go "a foreign-platform path is honoured on this host" "$LA" \
    '	if !filepath.IsAbs(file) {' \
    '	if false {'

  # **An omitted digest is not the empty sentinel**, and where the file is
  # absent the actual digest is empty too — so without this a body with no
  # `ifMatch` is a write with no precondition.
  mutate go "an omitted digest is read as the empty sentinel" "$A" \
    '	if req.IfMatch == nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			`ifMatch is required; send "" to state that there is no file yet`)
		return
	}' \
    '	if req.IfMatch == nil {
		empty := ""
		req.IfMatch = &empty
	}'

  # ---- Round 2: what is validated has to be what is served ---------------
  # **The identity check is the whole of the pinning argument.** Without it a
  # rename between validating the configured file and opening its directory
  # substitutes another tree, and the desk serves what it never checked.
  mutate go "the pinned directory is not the one that was validated" "$PJ" \
    '	if !os.SameFile(c.dirInfo, pinned.info) {
		return errProjectMoved
	}' \
    '	if false {
		return errProjectMoved
	}'
  # The second half: the directory may be the one validated and the file that
  # chose it may have been replaced inside it.
  # The replacement keeps `held` in use and drops only the identity half, so
  # the mutation is the defect rather than a build failure.
  mutate go "the configuration file that chose the project is not re-checked" "$PJ" \
    '	if !held.Mode().IsRegular() || !os.SameFile(c.fileInfo, held) {' \
    '	if !held.Mode().IsRegular() {'
  # And the smaller window inside the open itself: a name inspected and then
  # opened is two operations, and what is held has to be what was inspected.
  mutate go "the descriptor is not compared to the directory that was inspected" "$PJ" \
    '	if !os.SameFile(inspected, held) {' \
    '	if held == nil {'

  # **An omission is not a withdrawal.** Without this, `{"project":{}}` clears
  # an operator's hand-edited default and answers 200.
  # ---- Round 4: one owner, and a documented descriptor number ------------

  # **The descriptor closed before the runtime execs**, or the runtime inherits
  # a capability it never asked for and this desk never meant to grant.
  mutate go "the project descriptor is left open across the exec" "$PL" \
    'const runtimeTrampoline = `cd /proc/self/fd/3/. && exec 3<&- && exec "$0" "$@"`' \
    'const runtimeTrampoline = `cd /proc/self/fd/3/. && exec "$0" "$@"`'

  # **One cell, or a copy made before the hand-over closes a running server's
  # descriptors.** A flag inside an exported struct copies with it.
  mutate go "ownership is per copy rather than shared" "$PJ" \
    '	p.own.mu.Lock()
	adopted := p.own.adopted
	p.own.mu.Unlock()
	if adopted {
		return errAdoptedByServer
	}' \
    '	if false {
		return errAdoptedByServer
	}'

  # **At most one release, however many callers ask.** Closing a descriptor
  # twice is closing whatever took its number in between.
  # The replacement runs the same closure every time instead of once, so the
  # call site stays well formed — a mutation that does not compile is not one
  # the suite survived.
  mutate go "the descriptors are released once per caller" "$PJ" \
    '	o.once.Do(func() {' \
    '	(func(release func()) { release() })(func() {'

  # ---- Round 3: what the runtime and the watcher actually follow ---------

  # **The descriptor or a name, and a name is what came apart.** Started from
  # the pathname again, a rename-and-replace leaves the runtime judging one
  # tree while the file API edits another.
  # **The trampoline dropped for `cmd.Dir`**, which is the shape round 3 had
  # and round 4 refused: it works only while an ordering inside `os/exec`
  # happens to hold, and it is the parent's descriptor number.
  mutate go "the child is started from the pathname again" "$PL" \
    '	cmd := exec.CommandContext(ctx, shell, "-c", runtimeTrampoline, binary, "mcp")
	// **The documented contract**: this becomes descriptor 3 in the child,
	// with close-on-exec cleared for it there and nowhere else.
	cmd.ExtraFiles = []*os.File{dirFile}' \
    '	_, _ = shell, dirFile
	cmd := exec.CommandContext(ctx, binary, "mcp")
	cmd.Dir = s.projectDir'
  # The same for the watcher, which takes a path because inotify does.
  mutate go "the watcher is initialised from the pathname again" "$S" \
    '	watchRoot := pinned.dir
	if through, ok := pinned.descriptorWorkingDir(); ok {
		watchRoot = through
	}' \
    '	watchRoot := pinned.dir'
  # **The fallback every non-Linux host relies on**, broken here so a Linux run
  # can still answer for it: the check is its own function precisely so that a
  # row aimed at it is reachable from the suite that actually runs.
  mutate go "the pre-spawn identity check is removed on the pathname path" internal/desk/relay.go \
    '	if !sameDirectory(dir, pinned) {' \
    '	if false {'

  # **Adoption detaches, or two owners close one descriptor.** Without it the
  # caller's wrapper takes the file API out from under a running server.
  mutate go "an adopted project root is not detached from its caller" "$S" \
    '	pinned.detach()' \
    ''

  mutate go "an unstated project file is treated as a withdrawal" "$A" \
    '	if !present {
		// **An omission is not a withdrawal.** `{}` replaced the member with
		// an empty object, which the decoder reads as no default — so a
		// request that meant nothing by leaving `file` out silently cleared an
		// operator'"'"'s own setting.
		return &deskProblem{Key: "project.file", Reason: projectFileMustBeStated}
	}' \
    '	if !present {
		return nil
	}'

  # ---- The bootstrap: the handoff, the exchange, and what a bearer is ------
  #
  # The six rows below break the chassis half of the shape this desk's session
  # is. Each one is a property the README states in a sentence, and each one
  # was a review finding on the branch this design replaced.
  SE=internal/desk/session.go
  # **Single use is what bounds the residual.** A script that captures the
  # handoff inside its sixty seconds and forges the fetch-metadata header takes
  # the session — and the whole of what makes that *visible* rather than quiet
  # is that the page's own exchange then fails. A reusable handoff is a desk
  # with two users and nobody told.
  # Repaired: `consume` became `spend` and answers a verdict, so the needle
  # names the removal that makes a handoff single use. The property is
  # unchanged — a handoff left in `given` can be spent again and again.
  mutate go "the handoff is reusable" "$SE" \
    '		delete(ls.given, key)
		if ls.now().After(until) {' \
    '		if ls.now().After(until) {'
  # **The bound is a refusal, and refusing is the property.** Making room would
  # end a live session from outside the page holding it, which is the second
  # actor this whole design exists without.
  # Repaired: `create` answers a sequence too, so its refusal has three values.
  # Both bounds go — the one asked before a handoff is spent has its own row.
  mutate go "the 65th session is accepted" "$SE" \
    '	if len(st.live) >= maxSessions {
		return "", 0, errTooManySessions
	}' \
    ''
  # **One credential each way on the upgrade.** A session id on the handshake's
  # `Authorization` header is a second path for the page's credential, and a
  # path the browser cannot even use — a `WebSocket` constructor has no header
  # parameter.
  mutate go "a session id is accepted on the upgrade's header" "$S" \
    '	// No offer. A script'"'"'s socket, and only the launch secret opens one: a
	// session id on this header authorizes nothing here.
	return s.launchSecretPresented(r)' \
    '	if _, live := s.sessions.lookup(bearerOf(r)); live {
		return true
	}
	return s.launchSecretPresented(r)'
  # **Nothing on a query authorizes anything.** A credential on a query is a
  # credential in an address bar, a `Referer`, a proxy log and `Response.url`.
  mutate go "a session id on the query authorizes a request" "$SE" \
    '	if id := bearerOf(r); id != "" {
		if held, ok := s.sessions.lookup(id); ok {
			return held, true
		}
	}
	return session{}, false' \
    '	for _, id := range []string{bearerOf(r), r.URL.Query().Get("token")} {
		if held, ok := s.sessions.lookup(id); ok {
			return held, true
		}
	}
	return session{}, false'
  # **No cookie authorizes anything but the exchange.** A cookie has no port, so
  # one accepted anywhere else is one every sibling service on this host holds —
  # which is the finding that set the previous branch aside.
  mutate go "a cookie authorizes a gated route" "$SE" \
    '	if id := bearerOf(r); id != "" {
		if held, ok := s.sessions.lookup(id); ok {
			return held, true
		}
	}
	return session{}, false
}' \
    '	if id := bearerOf(r); id != "" {
		if held, ok := s.sessions.lookup(id); ok {
			return held, true
		}
	}
	if cookie, err := r.Cookie("jpack-desk-session"); err == nil {
		if held, ok := s.sessions.lookup(cookie.Value); ok {
			return held, true
		}
	}
	return session{}, false
}'
  # **A reload and a theft are different facts.** One code for both left the
  # page unable to tell them apart, so it kept its session either way and the
  # residual was invisible to the person it happened to.
  mutate go "a spent handoff reads as an absent one" "$SE" \
    '			"code":        CodeHandoffSpent,' \
    '			"code":        CodeNoHandoff,'
  # **A refusal clearing the cookie concealed a theft.** The clearing header and
  # the refusal travel in one response, so a reload between the two presented
  # nothing, read `no-handoff`, and kept a session the person was never told
  # about.
  mutate go "a refusal clears the handoff too" "$SE" \
    '	case handoffSpent:
		// **And which session it bought**' \
    '	case handoffSpent:
		http.SetCookie(w, expireLaunchCookie(s.launchCookie, requestScheme(r) == "https"))
		// **And which session it bought**'
  # **The store remembers what it finished with**, which is the whole of how a
  # theft is told from a stranger's cookie.
  mutate go "the store forgets the handoffs it finished with" "$SE" \
    '	if ending, ok := ls.finished[key]; ok {
		return ending.how, ending.produced
	}' \
    '	if ending, ok := ls.finished[key]; ok && false {
		return ending.how, ending.produced
	}'
  # **And a value it never minted is ignored.** A page on any sibling loopback
  # port can plant a cookie of this name at a longer path; refusing what is not
  # recognised turns that into a permanent false theft.
  mutate go "an unrecognised cookie is treated as a spent handoff" "$SE" \
    '		case handoffSpent, handoffExpired:
			// The first classified answer stands, and a later live one still
			// wins — which is why this does not return.
			if best == handoffUnknown {
				best, produced = how, was
			}' \
    '		case handoffSpent, handoffExpired:
			if best == handoffUnknown {
				best, produced = how, was
			}
		default:
			if best == handoffUnknown {
				best = handoffSpent
			}'
  # **Every cookie of the name**, because `r.Cookie` answers the first and a
  # browser sends the longest path first.
  mutate go "only the first cookie of the name is read" "$SE" \
    '	for _, cookie := range r.Cookies() {
		if cookie.Name != s.launchCookie {
			continue
		}' \
    '	for _, cookie := range r.Cookies()[:1] {
		if cookie.Name != s.launchCookie {
			continue
		}'
  # **The count that outlives the handoff.** Everything about a stolen handoff
  # is over in sixty seconds; this is what a reload hours later can read.
  # Anchored on `readSession`'s own body: the mint answer carries the same
  # member, so a needle naming only the member matched the wrong one and the
  # row reported that nothing failed — which was true of the edit it made.
  # Repaired: both answers go through `sessionsBody` now, so the row breaks the
  # one place the numbers are composed.
  mutate go "the session endpoint reports no counts" "$SE" \
    '	return map[string]any{"minted": s.sessions.mintedSoFar(), "yours": yours}' \
    '	return map[string]any{}'
  # **A sequence per session**, which is what tells a page's own spent link from
  # somebody else's: without it every session says the same thing.
  mutate go "every session shares one sequence" "$SE" \
    '	st.minted++
	st.live[st.handle(id)] = session{
		subject: subject, issuer: issuer, created: time.Now(), seq: st.minted,
	}
	return id, st.minted, nil' \
    '	st.minted++
	st.live[st.handle(id)] = session{
		subject: subject, issuer: issuer, created: time.Now(), seq: 1,
	}
	return id, 1, nil'
  # **And the tombstone records which session its handoff bought.** Without it
  # a page cannot tell the echo of its own spent link from a theft, and a copy
  # planted at a longer path is a permanent false theft.
  mutate go "a spent handoff records no session" "$SE" \
    '	for _, value := range claimed {
		s.launches.attribute(value, seq)
	}' \
    '	_ = claimed'
  # **The bound is asked before a handoff is spent**, so a desk at capacity
  # refuses without eating the launch link.
  mutate go "a capacity refusal eats the launch link" "$SE" \
    '	if s.sessions.full() {
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeSessionsFull, errTooManySessions.Error())
		return
	}' \
    ''
  # **Every live handoff in one request is attributed to the one session
  # minted**, or the next reload mints a second from a cookie nobody used.
  mutate go "only the first live handoff is claimed" "$SE" \
    '		case handoffAccepted:
			claimed = append(claimed, cookie.Value)' \
    '		case handoffAccepted:
			if len(claimed) == 0 {
				claimed = append(claimed, cookie.Value)
			}'
  # **And an endpoint may not redirect the page.** A 307 to `/api/session` made
  # the page's own fetch repeat the request against the exchange with this
  # desk's bearer on it.
  mutate go "an upstream may redirect the page" "$MR" \
    '			response.Header.Del("Location")' \
    '			_ = "Location"'
  # **The mark on this desk's own refusals.** Without it the page cannot tell a
  # 401 this chassis wrote from a 401 the configured endpoint wrote, and an
  # expired model key reads as a lost desk session.
  # Repaired: the mark moved from `writeJSONCoded` into `writeJSON`, the one
  # function every JSON answer goes through — `writeJSONError` used to go round
  # the back of it.
  # Repaired: the mark reads the encoded body now, so a struct-shaped refusal
  # carries its own code rather than a generic one.
  mutate go "this chassis does not mark its own refusals" "$F" \
    '	w.Header().Set(RefusalHeader, codeIn(data))' \
    '	_ = codeIn(data)'
  # **And the mark says what the body says.** A generic mark on a refusal whose
  # body names a code is a second opinion about one refusal.
  mutate go "a structured refusal is marked generically" "$F" \
    '	if err := json.Unmarshal(data, &named); err == nil && named.Code != "" {
		return named.Code
	}' \
    '	if false {
		return named.Code
	}'
  # **The handlers that write their own refusals.** The WebSocket library's 400
  # and the file server's 416 are chassis-authored and were unmarked.
  mutate go "a library's own refusal goes unmarked" "$F" \
    '	if status >= 400 && m.Header().Get(RefusalHeader) == "" {
		m.Header().Set(RefusalHeader, m.code)
	}' \
    '	if false {
		m.Header().Set(RefusalHeader, m.code)
	}'
  # **And an endpoint may not wear it.** An endpoint that could set the mark
  # could end somebody's desk session from the far side of the relay.
  mutate go "an upstream keeps this desk's refusal mark" "$MR" \
    '			response.Header.Del(RefusalHeader)' \
    '			_ = RefusalHeader'
  # **Only GET mints a handoff.** Go'"'"'s mux matches HEAD on a GET pattern, so a
  # HEAD with a valid secret handed a credential to a request that carries no
  # body.
  mutate go "HEAD on the launch path mints a handoff" "$SE" \
    '	if r.Method != http.MethodGet {' \
    '	if false {'
  # **The exchange spends what it mints against.** Minting without consuming is
  # a handoff left live in a cookie jar for the rest of its minute, worth a
  # session to anything that can read it.
  #
  # Repaired: the edit used to keep the `consume` call and ignore its answer,
  # which is a *different* property — the handoff was still spent, so nothing
  # about "without spending" was broken and the row measured the refusal it
  # already had a row for. This one skips the spend, so the handoff survives,
  # and `TestTheHandoffIsSingleUse` inspects the store's own count after a
  # successful exchange.
  # Repaired: spending moved into `handoffPresented`, which asks the store for
  # a verdict. Removing the handle from `given` is what spends it, so the row
  # keeps the verdict and skips the removal — the exchange still accepts, and
  # the handoff is still there afterwards.
  mutate go "the exchange mints without spending the handoff" "$SE" \
    '		ls.finishLocked(key, handoffSpent)
		return handoffAccepted' \
    '		ls.given[key] = until
		ls.finishLocked(key, handoffSpent)
		return handoffAccepted'
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
  APS=web/src/shell/appearanceState.ts
  V=web/src/routes/AdminView.tsx
  SC=web/src/admin/SourceCard.tsx
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
    '        {refused && <ConfigCue full={CONFIG_REFUSED_CUE} short={CONFIG_REFUSED_SHORT} />}' \
    '        {false && <ConfigCue full={CONFIG_REFUSED_CUE} short={CONFIG_REFUSED_SHORT} />}'
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
    '  if (unique.length > 0) return { values: undefined, problems: unique, declaredPanes }' \
    '  if (false) return { values: undefined, problems: unique, declaredPanes }'
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
  # **The theme is applied where the ladder is resolved**, which is no longer
  # `DeskConfigProvider`: the file's `appearance` is the default and the
  # viewer's own preference beats it, and that layer cannot see one. The row
  # moved with the code rather than being retired — it is the same claim, that
  # a decoded theme reaches the root element and is not merely read.
  mutate web "the configured theme is decoded and never applied" "$APS" \
    '  useAppliedTheme(theme)' \
    '  void theme'
  # **Retired, with its reason: the control it broke no longer exists.** It was
  # "the copy button reports a copy it did not make", on the paste blocks every
  # Admin section carried. The card pattern removed them — a Location line says
  # where the file is and a Content disclosure shows what is in it, so a paste
  # block was a second way to do one thing — and `adminBlocks.tsx` went with
  # them. A row whose code is deleted cannot discriminate; it is named here so
  # that its absence is a statement rather than an oversight.
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
  PFC=web/src/admin/projectFileCards.tsx
  # **Retired: `a future storage kind becomes a control`.** It added a second
  # option to a one-option Select, and there is no Select: while the union has
  # one member the card states the kind as the value it is, so the break the row
  # made is not available and the claim it stood for — that no kind but
  # `filesystem` is offered — is now the stronger one below, that nothing offers
  # a kind at all. The decoder's refusal of any other kind is unchanged and is
  # held by `a storage kind other than filesystem is accepted`.
  #
  # A Select with one option looks like a choice, reads like one to every
  # enumeration of what a reader can change, and offers none.
  mutate web "the kind rendered as a Select with one option" "$PFC" \
    '      <code>{config.storage.packs.kind}</code>' \
    '      <Select
        id="storage-kind"
        value={config.storage.packs.kind}
        onValueChange={() => {}}
        options={[{ value: '"'"'filesystem'"'"', label: '"'"'filesystem'"'"' }]}
      />'
  mutate web "an unknown storage key is accepted" "$D" \
    "          ? section(storage.packs, 'storage.packs', ['kind', 'dir', 'idBase'], problems)" \
    "          ? section(storage.packs, 'storage.packs', ['kind', 'dir', 'idBase', 'bucket'], problems)"
  mutate web "a storage kind other than filesystem is accepted" "$D" \
    "  if (value !== 'filesystem') {" \
    '  if (false) {'
  mutate web "storage defaults are not applied when the member is absent" "$D" \
    "      storage: pick('"'storage'"')" \
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
  #
  # The needle carries the opening tag because two providers are now handed the
  # same identity — the layout's and the appearance's — and a needle matching
  # both is one that silently mutates whichever comes first.
  mutate web "the layout key is not the project the chassis pinned" "$N" \
    '    <ShellStateProvider
      projectIdentity={listing.data?.root}' \
    '    <ShellStateProvider
      projectIdentity={undefined}'
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
    "    return window.localStorage.getItem(key) === null ? 'cleared' : 'refused'" \
    "    return 'cleared'"
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
    '      if (cause.status === 404) {
        return effectiveConfig(undefined, reasonFor(cause), undefined, await deskLevel)
      }' \
    '      if (true) {
        return effectiveConfig(undefined, reasonFor(cause), undefined, await deskLevel)
      }'
  mutate web "an unreadable configuration is silent outside Admin" "$S" \
    '        {!refused && unread && (' \
    '        {false && unread && ('
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
    "    const record = resetShellState(storageKey)
    if (record !== 'cleared') return record" \
    '    const record = resetShellState(storageKey)
    void record'

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
  # Two `instanceof` checks now — the desk-level read has one of its own — so
  # the needle carries the line that follows to name the project read's.
  mutate web "a browser error is attributed to the chassis" "$Z" \
    '    if (cause instanceof FileRequestError) {
      if (cause.status === 404) {' \
    '    if (false) {
      if (cause.status === 404) {'
  mutate web "Admin sources every unread reason to the chassis" "$SC" \
    "      ) : failure.source === 'chassis' ? (" \
    '      ) : true ? ('

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
  #
  # **Retired — three rows, one reason.** "Admin names no accepted range at
  # all", "Admin calls a configured number the rendered one" and "an absent pane
  # is reported as a pane of zero" were all about the Panes card, which is gone:
  # its three dimensions were a settings page editing the frame it is drawn in,
  # and the reader that measured that frame by its ids went with it. Nothing on
  # Admin prints a pane size any more, so there is no claim of that shape left to
  # break. What survives of the argument is held elsewhere and still is: the
  # decoder's bounds by `a pane dimension of zero or twenty thousand is
  # accepted`, the sheet's caps by the `shell.css` rows, and the measured-not-
  # configured rule by `the slot reports a configured width the pane does not
  # have`.

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
  mutate web "provenance is inferred from the status again" "$SC" \
    '      {!failure.responseReceived ? (' \
    '      {false ? ('
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
  # (The shaping grew a second source in the Describe chunk — a template's
  # bytes or a proposal's snapshot — so the guarded call is a branch now. The
  # row is the same claim about the same guard: the shaping runs unguarded and
  # a document that cannot be shaped is discovered after the write.)
  mutate web "a template that is not a document is sent anyway" "$X" \
    '        try {
          content = shapeTemplate(source.text, { name, description, slug, idBase })
        } catch (cause) {
          setFailure({ lead: TEMPLATE_UNUSABLE, reason: reasonOf(cause) })
          return
        }' \
    '        content = shapeTemplate(source.text, { name, description, slug, idBase })'

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
  # The needle names the *item*'s radius rather than the shorthand alone: the
  # trigger's radius came down to `--radius-sm` in chunk 6f, so the bare
  # declaration now matches twice and the row would edit whichever came first.
  mutate web "a module spells a radius of its own" "$SELCSS" \
    '  padding: 0.35rem 0.55rem;
  border-radius: var(--radius-sm);' \
    '  padding: 0.35rem 0.55rem;
  border-radius: 4px;'

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
    '      return { report: parsed, checkedBytes: documentText!, raw }' \
    "      return { report: parsed, checkedBytes: '', raw }"
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
  # Re-pinned: the two standing links and the way into edit mode moved out of
  # the document view, so they can also stand beside the raw bytes — which is
  # the only view of a file the runtime will not serve.
  mutate web "the what-if view loses its last way in" "$PV" \
    '      <Link
        className={styles.elsewhereLink}
        to={`/packs/${encodeURIComponent(packId ?? '"''"')}/evaluate`}
      >
        Try it
      </Link>' \
    '      {null}'

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
  HT=web/src/packs/edit/heldText.ts
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
  # **Repaired twice, and re-pinned.** Dropping the guard outright rebases on
  # every render and exhausts a worker's heap, which the harness reports as
  # INCONCLUSIVE: caught, and caught in a way that names no test. Keying the
  # guard on the content while still *recording* the path does the same thing,
  # because the two never match. Keying **both** on the content settles after
  # one re-seed and is the defect itself and nothing else: a watcher answer
  # carrying different bytes silently becomes the base, so the save that
  # follows overwrites a change nobody saw, without the 409 that exists to
  # prevent exactly that.
  mutate web "the base moves on a watcher refetch" "$BUF" \
    '    if (loaded === undefined) return
    if (seeded.current === loaded.path) {' \
    '    if (loaded === undefined) return
    if (seeded.current === loaded.content) {'
  # The other half of the same line, and the defect this PR's round found: the
  # buffer seeded once and never again, so another pack drew the first one.
  mutate web "the buffer stays on the pack it opened" "$BUF" \
    '    if (seeded.current === loaded.path) {' \
    '    if (seeded.current !== undefined) {'
  mutate web "the undo stack survives a change of pack" "$BUF" \
    "    // previous pack would put that pack's bytes into this one.
    setStack([])" \
    "    // previous pack would put that pack's bytes into this one."

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
    "    if (name === 'op' || members.includes(name)) continue" \
    "    if (name === 'op') continue"
  mutate web "a wrap re-serializes the child instead of moving its bytes" "$COP" \
    '  const raw = current.text.slice(span.valueStart, span.valueEnd)' \
    '  const raw = JSON.stringify(JSON.parse(current.text.slice(span.valueStart, span.valueEnd)))'

  # The page and the form are over the same bytes. This is the riskiest claim
  # in the whole change and it has its own row.
  mutate web "the page draws the served document while the form writes the buffer" "$PV" \
    '  const drawn: PackDocument | undefined = onPath
    ? isRecord(read?.index.value)' \
    '  const drawn: PackDocument | undefined = false
    ? isRecord(read?.index.value)'
  mutate web "form mode is offered over a document the two readings disagree about" "$PV" \
    '  const formAvailable = disagreement.length === 0 && isRecord(read?.index.value)' \
    '  const formAvailable = isRecord(read?.index.value)'
  mutate web "the check gates the save" "$PV" \
    '      const submitted = bufferText
      // **Which buffer this save is for**' \
    '      if ((check.data?.report.diagnostics?.length ?? 0) > 0) return
      const submitted = bufferText
      // **Which buffer this save is for**'
  mutate web "Mod+S is swallowed inside the field it exists to fire in" "$PV" \
    '      if (event.repeat || event.defaultPrevented) return
      event.preventDefault()' \
    '      if (event.repeat || event.defaultPrevented) return
      if ((event.target as HTMLElement).tagName === "TEXTAREA") return
      event.preventDefault()'
  # And the other end of the same chord: `document.body` is where focus sits
  # the moment edit mode opens, because the Edit button unmounts itself.
  mutate web "Mod+S is a subtree’s chord again, so the body reaches nothing" "$PV" \
    '      if (event.repeat || event.defaultPrevented) return
      event.preventDefault()' \
    '      if (event.repeat || event.defaultPrevented) return
      if (event.target === document.body) return
      event.preventDefault()'
  mutate web "Escape discards the buffer" "$PV" \
    '    const onKey = (event: globalThis.KeyboardEvent) => {' \
    '    const onKey = (event: globalThis.KeyboardEvent) => {
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
    '  const stale =
    attempt !== null &&' \
    '  const stale =
    false &&'
  mutate web "the foot prints the project’s decision id rather than the payload’s" "$TIP" \
    '                <code>{attempt.run.payload.packId}</code>' \
    '                <code>{packId}</code>'

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

  # ---------------------------------------------------------------------
  # The verification round. Each row is one finding, broken again.
  # ---------------------------------------------------------------------
  ECX=web/src/packs/edit/editingContext.ts
  FLD=web/src/packs/edit/fields.tsx
  MTB=web/src/packs/inspector/MemberTab.tsx
  ETB=web/src/packs/edit/EditToolbar.tsx
  SHP=web/src/packs/edit/shape.ts
  RCD=web/src/packs/document/RuleCard.tsx

  # The buffer is a document somebody is halfway through writing, and every
  # reader of it has to be able to say so.
  mutate web "a member that is not a list is mapped over anyway" "$ECX" \
    '    (Array.isArray(entries) ? (entries as { id?: unknown }[]) : [])' \
    '    ((entries ?? []) as { id?: unknown }[])'

  # The page is over the file, and everything it says about the file has to be
  # about the bytes the editor holds.
  mutate web "the provenance line survives an unsaved edit" "$MTB" \
    '  const bound =
    dirty !== true &&
    meta.sha256 !== undefined &&' \
    '  const bound =
    meta.sha256 !== undefined &&'

  # A control over a member the document does not carry writes nothing, and a
  # form that draws one is a form that takes a keystroke and loses it.
  mutate web "a field over an absent object is drawn as writable" "$FLD" \
    '  if (held !== undefined) return <>{children}</>' \
    '  if (held !== NOT_DECLARED) return <>{children}</>'
  mutate web "the blank option is the empty string ui/Select says is never offered" "$FLD" \
    "            ...(optional === true ? [{ value: NOT_DECLARED, label: 'not declared' }] : []),
            ...declared.map((word) => ({ value: word, label: word }))," \
    "            ...(optional === true ? [{ value: '', label: 'not declared' }] : []),
            ...declared.map((word) => ({ value: word, label: word })),"

  # A node the document does not carry is not a kind this desk has never seen.
  mutate web "a removed condition is called a kind this desk does not know" "$CB" \
    '  if (node === undefined) {' \
    '  if (node === undefined && depth < 0) {'
  # The unwritten operand, which both exits from the form used to lose.
  mutate web "an unwritten operand is the field’s own again" "$CB" \
    '    hold(at, { text: next, from: held, owner: ownerOf(buffer, at) })' \
    '    hold(at, null)'
  mutate web "the toolbar says nothing about a field that is not written" "$ETB" \
    '          {unwritten > 0 && (' \
    '          {unwritten > 99 && ('

  # One word, spliced, where the new kind needs no member the old node lacks.
  mutate web "a kind change re-serializes a subtree it only renamed" "$COP" \
    '  next = present.includes('"'"'op'"'"')
    ? setOp(next, pointer, kind)' \
    '  next = present.includes('"'"'op'"'"')
    ? buffered(
        current.text.slice(0, span.valueStart) +
          serialize(
            { ...(before as Record<string, unknown>), op: kind },
            layoutOf(current, pointer)
          ) +
          current.text.slice(span.valueEnd)
      )'

  # A move exchanges bodies. The whitespace belongs to the position.
  mutate web "a move drops the run in front of a comma" "$DT" \
    "      const separator = position === bodies.length - 1 ? '' : \`\${slot.trail},\`" \
    "      const separator = position === bodies.length - 1 ? '' : ','"
  mutate web "an element drags its own indentation across a move" "$DT" \
    '  const rebuilt = bodies' \
    '  const [carried] = parts.splice(from, 1)
  parts.splice(to, 0, carried!)
  const rebuilt = bodies'

  # The mirrored schema, and the objects an offer writes.
  mutate web "the mirrored nonEmptyString list loses a member" "$SHP" \
    '  /^\/metadata\/authors\/\d+$/,
' \
    ''
  mutate web "there is nothing to write for an absent required object" "$SHP" \
    "  { shape: /^\/escalation\/target$/, json: '{ \"kind\": \"human-role\", \"name\": \"\" }' }" \
    "  { shape: /^\/escalation\/nowhere$/, json: '{ \"kind\": \"human-role\", \"name\": \"\" }' }"

  # The chord is on the element the move focuses.
  mutate web "the move chord is bound inside the card again" "$RCD" \
    '      <Block pointer={at} as="li" className={styles.card} onKeyDown={order?.onKeyDown}>' \
    '      <Block pointer={at} as="li" className={styles.card}>'

  # The confirmation is about the bytes it confirmed.
  mutate web "the rehearsal confirmation is a flag again" "$TIP" \
    '  const armed = armedFor !== null && armedFor === buffer' \
    '  const armed = armedFor !== null'

  # Both digests, whole, for a reader comparing against sha256sum.
  mutate web "the stale-write alert carries only the twelve characters it prints" "$SWA" \
    '            <code title={stale.expectedSha256}>sha256 {digest(stale.expectedSha256)}</code>' \
    '            <code>sha256 {digest(stale.expectedSha256)}</code>'

  # A pattern is not something to read aloud.
  mutate web "the id hint prints the pattern at the author" "$CF" \
    "const ID_HINT = 'lowercase letters, digits and hyphens; starts with a letter.'" \
    "const ID_HINT = 'a local id — ^[a-z][a-z0-9]*(?:-[a-z0-9]+)*\$'"

  # The route: the refusal, the path, the mode, the measurement, the key.
  mutate web "a refusal replaces the page that could repair it" "$PV" \
    '  if (pack.error && file.data === undefined) {' \
    '  if (pack.error) {'
  mutate web "the path is only ever the one get_pack names" "$PV" \
    '  const path = meta?.path ?? summary?.path' \
    '  const path = meta?.path'
  mutate web "read mode hands over an editable buffer" "$PV" \
    '                  readOnly={!editing || !onPath}' \
    '                  readOnly={!onPath}'
  mutate web "the last write’s verdict follows the viewer to another pack" "$PV" \
    '    if (first) return
    reset()' \
    '    if (first) return'
  mutate web "the what-if placement measures the box it resizes" "$PV" \
    '          ref={setFrame}
          style={{ '"'"'--tryit-pane-width'"'"': `${PANE_WIDTH}px` } as CSSProperties}
        >
          <div className={styles.column}>' \
    '          style={{ '"'"'--tryit-pane-width'"'"': `${PANE_WIDTH}px` } as CSSProperties}
        >
          <div className={styles.column} ref={setFrame}>'
  mutate web "the outline is rebuilt on every keystroke" "$PV" \
    "  const documentKey = \`\${idle.checkedText ?? shownText ?? ''}|\${editing ? shape : 'read'}\`" \
    "  const documentKey = \`\${shownText ?? ''}|\${editing ? shape : 'read'}\`"
  mutate web "a deep link stops at the group instead of the control" "$PV" \
    "    const control = element.querySelector('input, textarea, [role=\"combobox\"]')" \
    "    const control = element.querySelector('nothing-at-all')"
  mutate web "a field group cannot take focus at all" "$PF" \
    '      tabIndex={-1}
      className={styles.field}' \
    '      className={styles.field}'

  # ---- the second reading: one file, one revision, one attempt ----------------
  MSH=web/src/packs/document/MisshapenMember.tsx
  RB=web/src/packs/document/RulesBlock.tsx
  AB=web/src/packs/document/ApplicabilityBlock.tsx
  MDB=web/src/packs/document/MetadataBlock.tsx
  MTB=web/src/packs/inspector/MemberTab.tsx

  # **The gap between `get_pack(B)` answering and B's bytes arriving.** The page
  # drew A's buffer under B's address, and Save combined B's path with A's bytes
  # and A's digest — which a 409 catches only where the digests differ.
  mutate web "the page draws a buffer that is another file's" "$PV" \
    "  const onPath = path !== undefined && buffer.base?.path === path" \
    "  const onPath = path !== undefined"
  # ---- the third reading: an answer that arrives after the page has moved ----
  MSHP=web/src/packs/document/MisshapenMember.tsx
  MDBLK=web/src/packs/document/MetadataBlock.tsx
  EXTB=web/src/packs/document/ExtensionsBlock.tsx
  PINS=web/src/packs/inspector/PackInspector.tsx

  # A reload is a read that takes as long as it takes. Ask for one on A,
  # navigate to B, edit B — and A's answer landed in B. The identity travels
  # with the read and **one** reader decides whether it still holds: a second
  # check at the call site was a second rule, and each masked the other's row.
  mutate web "the buffer takes a revision it is no longer about" "$BUF" \
    '      if (expect !== undefined) {
        if (expect.generation !== generationNow.current) return false
        // **An edit since the read was asked for is a refusal.** Including one
        // made by Accept, which is an edit like any other: adopting here would
        // replace it and clear the stack that could have taken it back.
        if (expect.revision !== edits.current) return false
        if (seeded.current !== undefined && seeded.current !== expect.path) return false
        if (fresh.path !== expect.path) return false
      }' \
    '      void expect'
  mutate web "an earlier reload answers over a later one" "$FE" \
    '          if (ticket !== reloads.current) return
          setReloading(false)
          // **The buffer decides first.**' \
    '          // **The buffer decides first.**'
  mutate web "a failed reload names whatever file is on screen" "$FE" \
    '          setReloadError({
            path,
            error: cause instanceof Error ? cause : new Error(String(cause))
          })' \
    '          setReloadError({
            path: "the file on disk",
            error: cause instanceof Error ? cause : new Error(String(cause))
          })'
  # Unwritten operand text is work the buffer cannot see for itself.
  mutate web "a file is adopted over text nobody has written yet" "$BUF" \
    '    if (seeded.current !== undefined && (dirtyNow.current || otherWork.current)) {' \
    '    if (seeded.current !== undefined && dirtyNow.current) {'
  mutate web "the drafts are dropped before the file is taken" "$BUF" \
    '    onAdopt.current?.()' \
    '    void onAdopt'
  # A→B→A, and the offer B left behind.
  mutate web "an offer outlives the address that produced it" "$BUF" \
    '      setWaiting((held) => (held === undefined ? held : undefined))
      return' \
    '      return'
  # **The offer is cleared in a passive effect**, so React commits the render
  # that carries the new address first: there is a committed state in which the
  # button is on screen and the offer is for a file the page has left. Saying
  # that state was unreachable was wrong — a layout effect is exactly that
  # window, and this is broken there.
  mutate web "an offer off the address may still be accepted" "$BUF" \
    '    if (waiting === undefined || loaded?.path !== waiting.path) return' \
    '    if (waiting === undefined) return'
  # A document is an object. Each of `null`, `[]`, `"a pack"` and `7` scans,
  # agrees with `JSON.parse`, and reaches `Object.keys`.
  mutate web "bytes that are not a document are handed to the readers" "$PV" \
    '    ? isRecord(read?.index.value)
      ? (read.index.value as unknown as PackDocument)
      : undefined' \
    '    ? (read?.index.value as unknown as PackDocument | undefined)'
  mutate web "a served document that is not one is handed to the readers" "$PV" \
    '    : isRecord(served)
      ? (served as PackDocument)
      : undefined' \
    '    : (served as PackDocument | undefined)'
  mutate web "the form is offered over bytes that are not a document" "$PV" \
    '  const formAvailable = disagreement.length === 0 && isRecord(read?.index.value)' \
    '  const formAvailable = disagreement.length === 0 && read?.index.value !== undefined'
  # And the same question wherever a component reaches *through* a member.
  mutate web "a nested member of any shape is reached through" "$MSHP" \
    "  const right = expects === 'list' ? Array.isArray(value) : isRecord(value)" \
    '  const right = true'
  mutate web "reviews that are not a list are mapped over" "$MDBLK" \
    '  if (!Array.isArray(metadata.reviews)) {' \
    '  if (false) {'
  mutate web "extensions that are not an object are enumerated" "$EXTB" \
    '  if (!isRecord(extensions)) {' \
    '  if (false) {'
  # Two clicks in one frame are two audit records on a runtime that does not
  # take the rehearsal declaration.
  mutate web "Try it runs twice for one press of the button" "$TIP" \
    '    if (!runnable || running.current) return' \
    '    if (!runnable) return'
  # The latch, and the two ways it has to be let go.
  mutate web "a save left in flight by a navigation wedges every later one" "$PV" \
    '    setUnaccounted(false)
    saving.current = undefined' \
    '    setUnaccounted(false)'
  mutate web "the latch is released only through the mutation observer" "$FE" \
    '        .finally(() => input.onSettled?.({ delivered }))' \
    '        .finally(() => {})'
  # The Inspector: a defined base, and no served fallback behind the buffer.
  mutate web "an absent base digest is read as a match" "$MTB" \
    '    baseSha256 === fileSha256' \
    '    (baseSha256 === undefined || baseSha256 === fileSha256)'
  mutate web "the Inspector falls back to the document the runtime served" "$PV" \
    '        document={drawn}' \
    '        document={drawn ?? pack.data.document}'
  mutate web "the Inspector says nothing about having no document" "$PINS" \
    '  if (doc === undefined) {' \
    '  if (false) {'
  # A list whose values are the author's own.
  mutate web "an author cannot be added to a list that is not there" "$FLD" \
    "          <Button variant=\"quiet\" onClick={() => put([...strings, ''])}>" \
    '          <Button variant="quiet" onClick={() => put(strings)}>'
  # A pointer is a position, not an identity.
  mutate web "a draft follows its pointer onto another rule" "$PV" \
    '          (bytesAt(read, pointer) ?? '"'"''"'"') === draft.from &&
          ownerOf(read, pointer) === draft.owner' \
    '          (bytesAt(read, pointer) ?? '"'"''"'"') === draft.from' \
  # What the page says the editor holds after a save that landed other bytes.
  mutate web "the editor is said to hold what was sent, whatever it holds" "$PV" \
    '                    {bufferText === editor.outcome.submitted' \
    '                    {true'

  # ---- the fourth reading: what happens before the next render -------------
  # A refused reload used to reset the mutation and drop the verdict *before*
  # anything asked, so a save in flight for another file was detached from its
  # observer and that file's verdict disappeared.
  mutate web "a refused reload still clears the editor it landed in" "$FE" \
    '          if (!onLoaded(fresh)) return
          write.reset()' \
    '          write.reset()
          onLoaded(fresh)'
  # `forget()` only *scheduled* the increment while the generation was state, so
  # a read resolving in the same turn still saw the old number — and
  # `seeded.current` was already cleared, which took the path check with it.
  mutate web "forgetting a buffer leaves its generation for the next render" "$BUF" \
    '    generationNow.current += 1
    setGeneration(generationNow.current)' \
    '    setGeneration((count) => count + 1)'
  # A→B→A is an ABA: the latch was the path, so the first save settling after
  # the page came back released the latch the second was holding.
  mutate web "a settling save releases whichever latch is held" "$PV" \
    '          if (saving.current === flight) saving.current = undefined' \
    '          if (saving.current !== undefined) saving.current = undefined'

  # **`save`'s own path check has no row, deliberately.** It is defence behind
  # the gate above: `bufferText` is undefined while the buffer is another file's,
  # so the first guard in `save` returns before the equality is reached and no
  # single mutation can tell the two apart. The gate has the row; the route test
  # asserts that a remap writes nothing; and the check stays, because a second
  # reader of one rule is what this whole finding was about.
  # `write.isPending` is state and arrives a render later, so two chords inside
  # one frame both read "not saving".
  mutate web "two chords in one frame issue two writes against one base" "$PV" \
    '      if (saving.current !== undefined) return' \
    '      void saving'
  mutate web "a key held down saves once a frame" "$PV" \
    '      if (event.repeat || event.defaultPrevented) return' \
    '      void event.repeat'
  # A watcher refetch moves the file query and not the base, so a clean buffer
  # can be a previous revision — and the page called it the file on disk.
  mutate web "a revision that is not on disk is called the file on disk" "$PV" \
    '  const behindDisk =
    onPath &&
    buffer.base !== undefined &&
    file.data !== undefined &&
    file.data.sha256 !== buffer.base.sha256' \
    '  const behindDisk = false
  void file.data'
  mutate web "the Inspector binds the page to a file it has not read" "$MTB" \
    '    baseSha256 === fileSha256' \
    '    true'
  # Only `StaleWrite` was rendered: every other refusal stopped the button and
  # said nothing.
  mutate web "a save refused for any other reason says nothing" "$PV" \
    '  const saveFailure = editor.write.error !== null && staleWrite === undefined ? editor.write.error : undefined' \
    '  const saveFailure = undefined as Error | undefined'
  mutate web "what a save actually did is never printed" "$PV" \
    '            {editor.outcome !== undefined && staleWrite === undefined && (' \
    '            {false && ('
  # Reload cleared the conflict before its own read had answered.
  mutate web "a failed reload leaves the page with nothing on it" "$FE" \
    '      setReloadError(undefined)' \
    '      setReloadError(undefined)
      write.reset()
      setOutcome(undefined)'
  # The author is free to keep typing while the PUT is in the air.
  mutate web "a save that lands takes the last sentence with it" "$BUF" \
    '    if (current.current !== submitted || fresh.content !== submitted) return' \
    '    void submitted'
  mutate web "a path that moves under one address replaces unsaved work" "$BUF" \
    '    if (seeded.current !== undefined && (dirtyNow.current || otherWork.current)) {
      setWaiting(loaded)
      return
    }' \
    '    void dirtyNow'
  mutate web "a pack the route comes back to has nothing to edit" "$BUF" \
    '    generationNow.current += 1
    setGeneration(generationNow.current)' \
    '    void setGeneration'
  # Valid JSON, wrong shape. `rules.map` on an object took the route down.
  mutate web "a member of the wrong shape is handed to the controls anyway" "$PDV" \
    '  if (shape !== undefined && !isShape(held, shape)) {' \
    '  if (false) {'
  mutate web "a rule that is not an object is drawn as a card" "$RB" \
    '          !isRecord(rule) ? (' \
    '          false ? ('
  mutate web "fields are drawn over a member that is not an object" "$FLD" \
    '  if (held !== undefined && (typeof held !== '"'"'object'"'"' || held === null || Array.isArray(held))) {' \
    '  if (false) {'
  mutate web "add over a conditions that is not a list does nothing" "$COP" \
    '  if (held !== undefined && !Array.isArray(held)) {' \
    '  if (false) {'
  mutate web "new bytes are written with this module's line ending" "$COP" \
    "  return text[found - 1] === '\r' ? '\r\n' : '\n'" \
    "  void found
  return '\n'"
  # Try it: one attempt, labelled from what it sent.
  mutate web "a refusal leaves the answer before it on screen" "$TIP" \
    '        onError: (error: Error) => setAttempt({ kind: '"'"'refusal'"'"', error, sent })' \
    '        onError: () => {}'
  mutate web "an answer is labelled from the toggle rather than from the run" "$TIP" \
    '              {attempt.sent.source === '"'"'pack'"'"'
                ? '"'"'from the draft in the editor'"'"'
                : '"'"'from the pack on disk'"'"'}' \
    '              {source === '"'"'pack'"'"'
                ? '"'"'from the draft in the editor'"'"'
                : '"'"'from the pack on disk'"'"'}'
  mutate web "only the bytes make an answer stale" "$TIP" \
    '    (attempt.sent.source !== source ||
      attempt.sent.facts !== facts ||
      attempt.sent.evidence !== evidence ||
      (attempt.sent.source === '"'"'pack'"'"'
        ? attempt.sent.bytes !== buffer
        : attempt.sent.packId !== packId))' \
    '    attempt.sent.bytes !== buffer'
  # What the form may reach.
  mutate web "applicability is read-only, whatever mode the page is in" "$AB" \
    '      {editing ? (
        <ConditionBuilder at={at} />
      ) : (
        <ConditionTree condition={applicability} at={at} />
      )}' \
    '      <ConditionTree condition={applicability} at={at} />'
  mutate web "the fallback outcome is a code element in a form" "$PDV" \
    '      {editing ? (
        <IdRefField pointer="/fallbackOutcome" label="fallback outcome" ids={ids.outcomes} optional />
      ) : (' \
    '      {false ? (
        <IdRefField pointer="/fallbackOutcome" label="fallback outcome" ids={ids.outcomes} optional />
      ) : ('
  mutate web "every metadata member is render-only, not only the reviews" "$MDB" \
    '  if (editing) {' \
    '  if (false) {'
  # Text that is not written yet is work.
  mutate web "unwritten text is not work, so leaving takes it" "$PV" \
    '  const hasWork = dirty || unwritten > 0' \
    '  const hasWork = dirty'
  mutate web "Discard leaves the text it did not write" "$PV" \
    '    buffer.discard()
    forgetDrafts()' \
    '    buffer.discard()'
  mutate web "a draft is masked rather than retired" "$PV" \
    '          ownerOf(read, pointer) === draft.owner
        ) {
          continue
        }
        next.delete(pointer)' \
    '          ownerOf(read, pointer) === draft.owner
        ) {
          continue
        }
        void pointer'
  # The pane's width, which the predicate and the stylesheet disagreed about.
  mutate web "the pane asks for eight pixels it does not take" "$PV" \
    'const PANE_WIDTH = 384' \
    'const PANE_WIDTH = 392'

  # ---- The assistant slot, on the page side -------------------------------
  AS=web/src/assistant/AssistantSection.tsx
  AC=web/src/assistant/client.ts
  AQ=web/src/assistant/queries.ts
  CQ=web/src/config/queries.ts

  # **Two rows are gone from here**, and the reason is worth the space. They
  # broke a helper that chose the credential sentence at the schema walk, on
  # top of the recursive pre-scan that also produces it — so breaking either
  # one left the other saying it, and both rows reported an unheld safeguard
  # while two things held it. There is one producer now (`scanForKeys`, with
  # its own two rows below) and one rule that keeps a member from being
  # refused twice, which is what this breaks.
  mutate web "a member refused as a credential is also refused as unknown" "$D" \
    '  return problems.filter(
    (problem) =>
      problem.reason === KEYS_ARE_NEVER_IN_CONFIGURATION || !credentialed.has(problem.key)
  )' \
    '  return problems'
  mutate web "the tool list is open" "$D" \
    '      if (!(ASSISTANT_TOOLS as readonly string[]).includes(tool)) {' \
    '      if (false && (ASSISTANT_TOOLS as readonly string[]).includes(tool)) {'
  # A project is a shared checkout; committing an endpoint pushes one
  # operator's model endpoint onto every clone.
  mutate web "an endpoint may be committed to a project" "$D" \
    "    if (key === 'assistant' && location === 'project') {
      problems.push({ key: 'assistant', reason: ASSISTANT_AT_PROJECT })
      continue
    }
" \
    ''
  # A defaulted tool list is a capability granted by a file that never
  # mentioned it.
  mutate web "an absent tool list grants every tool" "$D" \
    "  if (endpoint.tools === undefined) {
    problems.push({
      key: 'assistant.endpoint.tools'," \
    "  if (endpoint.tools === undefined) {
    tools = [...ASSISTANT_TOOLS]
  } else if (false) {
    problems.push({
      key: 'assistant.endpoint.tools',"
  # The more specific statement wins: a project saying something different is
  # saying it about itself.
  mutate web "the desk-level file outranks the project's own" "$D" \
    '    (values?.[key] ?? deskValues?.[key] ?? DESK_DEFAULTS[key]) as DeskConfig[K]' \
    '    (deskValues?.[key] ?? values?.[key] ?? DESK_DEFAULTS[key]) as DeskConfig[K]'
  mutate web "the desk-level file is never read" "$CQ" \
    '  const deskLevel = loadDeskLevelConfig(signal)' \
    '  const deskLevel = Promise.resolve(undefined)'
  # Either file, one cue: the argument does not care which carried the typo.
  mutate web "the strip is silent about a refused desk-level file" "$S" \
    '  const refused = problems.length > 0 || (desk?.problems.length ?? 0) > 0' \
    '  const refused = problems.length > 0'
  # Deliberately gone rather than retargeted: the row that broke the
  # success-only clearing of a *controlled* field. There is no controlled
  # field any more — React batches a `setState`, so the key was still in the
  # input when `fetch` began — and the clearing it tested is now an assignment
  # to an uncontrolled node, which "the field is cleared only once the store
  # has answered" below breaks directly.
  mutate web "the key field is an ordinary text input" "$AS" \
    '          type="password"' \
    '          type="text"'
  mutate web "removal is offered where there is nothing to remove" "$AS" \
    '      {state.present && (' \
    '      {true && ('
  # A 401 is a host that is there and a credential it will not take.
  mutate web "a refused credential is painted as reachable" "$AS" \
    "      {result.reachable ? 'reachable' : 'not reachable'}" \
    "      {result.status < 500 ? 'reachable' : 'not reachable'}"
  mutate web "a status of zero is painted as an answer" "$AS" \
    "      {result.status === 0 ? 'no answer arrived' : \`answered \${result.status}\`}" \
    "      {\`answered \${result.status}\`}"
  # A read that has not answered is not "no key": it is a page that has not
  # been told.
  mutate web "an unanswered read is reported as no key" "$AS" \
    "  if (!answered) return 'not read yet'" \
    "  if (!answered) return 'none stored on this machine'"
  # If the probe named a URL, anything holding the token could point the desk
  # — and the key it holds — at a host of its choosing.
  mutate web "the probe names its own destination" "$AC" \
    "    await deskFetch(chassisUrl('/api/assistant/probe'), { method: 'POST', signal })" \
    "    await deskFetch(chassisUrl('/api/assistant/probe'), {
      method: 'POST',
      signal,
      body: JSON.stringify({ url: 'http://127.0.0.1:1/v1' })
    })"

  # The credential scan, and the two URL members a key can hide in.
  # The browser's half of the same rule: two implementations of one contract
  # drift, and the fixtures hold them together only if both actually check.
  # The browser's half of the case rule, so the two cannot drift apart again.
  mutate web "a reserved query name in another case is accepted" "$D" \
    '        (reserved) => reserved.toLowerCase() === decoded.toLowerCase()' \
    '        (reserved) => reserved === decoded'
  mutate web "a configured query is not held to any rule" "$D" \
    '  const query = endpointQueryProblem(raw)
  if (query !== undefined) return query' \
    '  const query = undefined as string | undefined
  if (query !== undefined) return query'
  mutate web "the credential scan never runs" "$D" \
    "  scanForKeys('', parsed, problems)" \
    ''
  mutate web "the credential scan does not look inside arrays" "$D" \
    '    value.forEach((element, index) => scanForKeys(`${path}[${index}]`, element, problems))' \
    '    void value'
  mutate web "a key smuggled into the URL is accepted" "$D" \
    "  if (url.username !== '' || url.password !== '') {" \
    '  if (false) {'
  mutate web "a fragment on the endpoint is accepted" "$D" \
    "  if (url.hash !== '' || raw.includes('#')) {" \
    '  if (false) {'
  mutate web "the page accepts an engine nobody certified" "$D" \
    "        engine:
          oneOf(assistant.engine, 'assistant.engine', ASSISTANT_ENGINES, problems) ??
          DESK_DEFAULTS.assistant.engine," \
    '        engine: DESK_DEFAULTS.assistant.engine,'
  mutate web "the page accepts a thinking tier nothing implements" "$D" \
    "        thinking:
          oneOf(assistant.thinking, 'assistant.thinking', ASSISTANT_THINKING, problems) ??
          DESK_DEFAULTS.assistant.thinking" \
    '        thinking: DESK_DEFAULTS.assistant.thinking'
  # The typed key, and how long the page holds it.
  # An assignment to the node takes effect at once; a `setState` would not
  # have, which is the whole reason the field is uncontrolled.
  mutate web "the field is cleared only once the store has answered" "$AS" \
    "    if (input) input.value = ''" \
    '    void input'
  # **This row replaced one that did not discriminate.** It used to remove the
  # `store.reset()` the section called on settlement, on the belief that
  # resetting was what stopped React Query retaining the credential. It was
  # not: `reset()` clears the observer, and the mutation *cache entry* keeps
  # its variables for minutes afterwards either way — which a test that reads
  # the cache is what established. The key is not a mutation variable at all
  # now, so what is broken here is that.
  # One edit, and it is the defect exactly: the credential becomes something
  # React Query keeps. The mutation function ignores the argument either way —
  # what changes is that the cache entry now holds the key, which is what the
  # cache-reading test looks for.
  mutate web "the key is handed to the mutation as its variable" "$AQ" \
    '      mutation.mutate(undefined, {' \
    '      mutation.mutate(key as unknown as void, {'
  # Deliberately not mutated: the two places the pending ref is cleared. A ref
  # is closure state with no reader outside the hook, so breaking it produces
  # no observable difference for any test to catch — it would be a row that
  # reports "nothing failed" for ever, which reads as a missing safeguard
  # rather than an unobservable one. What *is* observable is that the cache
  # retains nothing, and the row above holds that.
  # A key is not removed by removing the endpoint, so the page must not say so.
  mutate web "no endpoint is reported as no key" "$AS" \
    "              ? 'none — no endpoint configured'" \
    "              ? 'none — no assistant, and no key'"
  mutate web "a diagnostic is rendered as the bare word" "$AS" \
    "      {result.diagnostic !== '' && (" \
    "      {false && result.diagnostic !== '' && ("

  # ---- The Admin form: what it writes, and what it will not ---------------
  #
  # Chunk 5c turns this section into a form, so the desk's *second* write is
  # now made by page code. Each row below breaks one of the bounds that makes
  # that safe, and every catcher is a test that reads the request on the wire
  # rather than the rendering — a form that showed the right thing and sent the
  # wrong one is exactly the failure these exist for.
  ED=web/src/assistant/endpointDraft.ts
  EF=web/src/assistant/EndpointForm.tsx
  MF=web/src/assistant/ModelField.tsx
  KB=web/src/assistant/keyBinding.ts

  # **The page must not compute the binding**, and it did: with the browser's
  # `URL`, which drops an explicit `:443` where Go's `url.Parse` keeps it. A
  # key stored for a host and a configuration naming the same host with its
  # default port written out showed as bound here while the relay answered
  # `assistant-key-unbound` and sent nothing. The verdict is the desk's.
  mutate web "the page decides the binding for itself again" "$KB"     '  return key.bound ? '"'"'bound'"'"' : '"'"'rebind'"'"''     '  return key.origin === key.configuredOrigin && key.kind === endpoint.kind
    ? '"'"'bound'"'"'
    : '"'"'rebind'"'"''

  # **A draft is page state, and a spread writes whatever it is carrying** into
  # the one file on this machine that names where a credential is presented.
  # The chassis would refuse a key-shaped member, which is what makes this a
  # rule rather than a hole — and the difference between a rule and a backstop
  # is that the rule is the one you can point at.
  mutate web "the written object is spread from the draft rather than named" "$ED" \
    '  return {
    endpoint: {
      url: draft.url.trim(),
      kind: draft.kind,
      model: draft.model.trim(),
      tools: ASSISTANT_TOOLS.filter((tool) => draft.tools.includes(tool))
    },
    engine: draft.engine,
    thinking: draft.thinking
  }' \
    '  return {
    ...(draft as unknown as Record<string, unknown>),
    endpoint: {
      ...(draft as unknown as Record<string, unknown>),
      url: draft.url.trim(),
      kind: draft.kind,
      model: draft.model.trim(),
      tools: ASSISTANT_TOOLS.filter((tool) => draft.tools.includes(tool))
    },
    engine: draft.engine,
    thinking: draft.thinking
  }'
  # **A write with no digest is a page overwriting whatever it found**, on the
  # file that names the endpoint a credential goes to. The empty string is not
  # "no opinion": it is the claim that there is no file.
  mutate web "the configuration write states no digest at all" "$EF" \
    '      { assistant, ifMatch: digest },' \
    "      { assistant, ifMatch: '' },"
  # **The write already answers with the slot and the digest**, and leaving the
  # tab, Describe it and the key row to a second GET meant a write that landed
  # under a read that hung left every one of them describing the endpoint that
  # had just been replaced, under a form that said "Saved".
  mutate web "the write's own answer is thrown away" "$AQ" \
    '      client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, (previous) =>
        configAfterWrite(previous, written)
      )' \
    '      void written'
  # The binding is the desk's verdict and a write can move it either way.
  mutate web "the key binding is not re-read after a write" "$AQ" \
    '      void client.invalidateQueries({ queryKey: ASSISTANT_KEY_QUERY_KEY })' \
    ''
  # A 409 says the file moved and nothing was written. Reload has to *read it
  # again*: a button that only cleared the alert would leave the next Save
  # stating the same stale digest, and the author pressing it twice.
  mutate web "Reload clears the notice without reading the file again" "$EF" \
    '                void client.refetchQueries({ queryKey: DESK_CONFIG_QUERY_KEY })' \
    '                void client'
  # **The row reads the desk's answer and nothing else.** It consulted the
  # page's copy of the configuration first — which is exactly the thing that
  # goes missing — so a desk-level read that answered with a refusal made the
  # row say "save an endpoint first" while a perfectly good key read beside it
  # named the endpoint and carried this desk's verdict about it.
  mutate web "the key row consults the page before the chassis" "$KB" \
    "  if (key.configuredOrigin === '') return 'no-endpoint'" \
    "  if (key.configuredOrigin === '' || key.origin === '') return 'no-endpoint'"
  # A read that did not produce a file is not a file that says none.
  SL=web/src/assistant/useAssistantSlot.ts
  mutate web "an unreadable configuration is reported as no assistant" "$SL" \
    "    state: unread ? 'unavailable' : endpoint === null ? 'none' : 'configured'," \
    "    state: endpoint === null ? 'none' : 'configured',"
  # The relay refuses a credential entered for another destination before it
  # opens a socket, so a listing offered here can only produce that refusal.
  mutate web "List models is offered with no key bound to the endpoint" "$MF" \
    '        <Button onClick={ask} disabled={!bound || !matchesSaved || asking}>' \
    '        <Button onClick={ask} disabled={!matchesSaved || asking}>'
  # **The gate came off the saved endpoint and the request came off the draft**,
  # so choosing Gemini without saving sent `v1beta/models` to a still-saved
  # OpenAI endpoint: a request composed for one destination and sent to another.
  mutate web "the listing is offered while the form says another endpoint" "$MF" \
    '        <Button onClick={ask} disabled={!bound || !matchesSaved || asking}>' \
    '        <Button onClick={ask} disabled={!bound || asking}>'
  # **Retired, with its reason.** It replaced the captured `saved` endpoint
  # with the draft, and nothing failed — because the row above makes the two
  # *equal* whenever the button can be pressed at all. The capture is still
  # the clearer expression of "ask the endpoint in the file", and it is the
  # second line of the same defence; what actually holds it is the gate, which
  # has its own row. A row that cannot discriminate is worse than no row: it
  # reports coverage for a safeguard nothing is measuring.
  # A picker left standing after the endpoint moved is a list of models from
  # somewhere else, offered against a form that no longer says that host — and
  # rows merely *hidden* came back when the URL was changed away and back, with
  # no request behind them. **Dropped from state**, and this is what says so:
  # the mutation keeps them and hides them, which is the arrangement that was
  # wrong rather than a weaker version of the right one.
  mutate web "the rows are hidden when the endpoint moves rather than cleared" "$MF" \
    '  const here = identityOf(draft)
  if (rows !== undefined && askedFor !== here) {
    setRows(undefined)
    setAskedFor(undefined)
    setRefusal(undefined)
  }
  const showing = rows !== undefined' \
    '  const here = identityOf(draft)
  const showing = rows !== undefined && askedFor === here'
  # **The page names a suffix; the desk builds the address.** A listing that
  # built its own URL would hold the endpoint — and, on this route, this
  # chassis' session token — in page code that no gate is on.
  mutate web "the listing address is built on the page" "$MF" \
    '    listModels(target.kind, bindModelCall(target.kind)).then(' \
    "    listModels(target.kind, async (suffix) =>
      globalThis.fetch(\`\${target.url}/\${suffix}\`, { method: 'GET' })
    ).then("
  # **The picker offers what the file's reader would take, and asks the reader
  # rather than carrying a copy of it.** A copy is how a whitespace-only id
  # came to be an option: the decoder trims and the copy did not, so the choice
  # saved cleanly into the field and produced a 422 on the next Save.
  ML=web/src/assistant/modelListing.ts
  mutate web "a listed id is offered without asking the decoder" "$ML" \
    "    const { id: raw, display_name: shown } = entry as { id?: unknown; display_name?: unknown }
    if (modelIdProblem(raw) !== undefined) continue" \
    "    const { id: raw, display_name: shown } = entry as { id?: unknown; display_name?: unknown }
    if (typeof raw !== 'string' || raw === '') continue"
  mutate web "a listed Gemini id is offered without asking the decoder" "$ML" \
    "      const raw = name.startsWith('models/') ? name.slice('models/'.length) : name
      if (modelIdProblem(raw) !== undefined) continue" \
    "      const raw = name.startsWith('models/') ? name.slice('models/'.length) : name
      if (raw === '') continue"
  # The id is what the endpoint answers to; the label is what a person reads,
  # and the two differ on two of the three protocols.
  mutate web "the model is saved from the listing label rather than its id" "$MF" \
    '              options={rows!.map((row) => ({ value: row.id, label: row.label }))}' \
    '              options={rows!.map((row) => ({ value: row.label, label: row.label }))}'
  # **The slot's other state, which the schema has.** `assistant.endpoint` is
  # one nullable field, and a form that could not write the null left a desk
  # that had configured an endpoint able to reach None only through the generic
  # file editor — while this page describes None as one of three states.
  mutate web "removing the endpoint writes an endpoint object anyway" "$ED" \
    '  return { endpoint: null, engine: draft.engine, thinking: draft.thinking }' \
    '  return assistantWrite(draft)'
  # How an assistant would run is not whether there is one, which is why the
  # schema allows both beside a null endpoint.
  mutate web "removing the endpoint discards the engine and the tier" "$ED" \
    '  return { endpoint: null, engine: draft.engine, thinking: draft.thinking }' \
    "  return { endpoint: null, engine: 'vercel', thinking: 'off' }"
  # A picker offering a fourth tier offers a configuration the decoder refuses
  # by name — and the two states it cannot express are the desk's to report.
  mutate web "the tier picker offers a value outside the union" "$EF" \
    '              options={TIER_OPTIONS}' \
    "              options={[...TIER_OPTIONS, { value: 'always', label: 'always' }]}"
  # **Deliberately not added: a second row for the key field being cleared
  # before the request.** "the field is cleared only once the store has
  # answered" above breaks exactly that assignment, and the form moved the
  # field without moving the clearing — so a row here would be the same edit
  # under a second name, and a duplicate row reports coverage twice for one
  # safeguard held once.

  # ---- The assistant's guardrails, below whatever runs the loop -----------
  #
  # ADR-0001 puts these under the engine slot on purpose: they are the desk's
  # promises and not a framework's features, so each one is broken here and the
  # test that notices is named. Every row's catcher is either the conformance
  # session — the bake-off scenario, run against the engine registry, with every
  # network global sealed for the duration of the engine's run — or a test that
  # measures at a recording transport or on the serialized bytes.
  TG=web/src/assistant/toolGate.ts
  ASN=web/src/assistant/session.ts
  BL=web/src/assistant/engines/builtin/loop.ts
  BT=web/src/assistant/engines/builtin/providers/types.ts
  BO=web/src/assistant/engines/builtin/providers/openai.ts
  AR=web/src/assistant/useAssistantRun.ts
  AP=web/src/assistant/AssistantPane.tsx
  RO=web/src/assistant/runOutcome.ts

  CT=web/src/assistant/conformance/conformance.test.ts
  EN=web/src/assistant/engines/index.ts
  EC=web/src/assistant/engines/contract.ts
  VL=web/src/assistant/engines/vercel/loop.ts
  VR=web/src/assistant/engines/vercel/relay.ts
  VC=web/src/assistant/engines/vercel/channel.ts

  # ---- Canonical bytes ----------------------------------------------------
  #
  # The lesson the chassis' relay learned over four rounds, and the one this
  # gate learned in round 2: a classification made about a mutable object is a
  # classification the object can change out from under you. Each row here
  # restores one version of that mistake.
  mutate web "the frame is classified before it is canonicalized" "$TG" \
    '  const frame = canonical(message)' \
    '  const frame = message as unknown'
  # The round-2 defect exactly: copy the caller's own properties — toJSON
  # included — and let the serializer invoke it after the member was written.
  mutate web "the arguments are copied rather than canonicalized" "$TG" \
    '  const args = (supplied ?? {}) as Record<string, unknown>' \
    '  const args = { ...((message as { params?: { arguments?: Record<string, unknown> } }).params?.arguments ?? {}) }'
  # **Two rows retired here, and a third below, because one row replaced all
  # three.** "a frame need not declare jsonrpc 2.0", "a response is exempted
  # without checking its shape" and "a frame with no method at all is passed"
  # each needled a hand-written shape rule, and those rules are gone: the
  # canonical frame is checked against the SDK's own JSONRPCMessageSchema, which
  # is stricter than the three were together — it is what refuses an id of null
  # beside a result, an error that is a string and a fractional id, none of
  # which the old rules caught. "a frame need not be a message the SDK would
  # accept" breaks that check, and the corpus it fails on is every case those
  # three used to own plus five they did not.

  # ---- The model capability's own answer ----------------------------------
  #
  # A browser Response carries the requested URL on `.url`, and that URL is the
  # relay address with this chassis' session token in it.
  mutate web "the model answer is handed back as fetch produced it" "$ASN" \
    '    return facade(answered)' \
    '    return answered'
  # A fetch TypeError quotes the URL, so the browser's own error is the token.
  # Anchored on the catch it belongs to: the opaque-redirect branch throws the
  # same sentence, so a needle naming only the throw matched the wrong one.
  mutate web "a failed model call rethrows the browser's own error" "$ASN" \
    '      throw new Error(CALL_FAILED)
    }
    // **An opaque redirect is not an answer.**' \
    '      throw cause
    }
    // **An opaque redirect is not an answer.**'
  mutate web "the answer's headers are not filtered" "$ASN" \
    '    if (MODEL_ANSWER_HEADERS.includes(name.toLowerCase())) carried.set(name, value)' \
    '    carried.set(name, value)'
  # A string-like object answers an innocuous split() while the validator looks
  # and a different toString() when the URL is built.
  mutate web "the suffix is not required to be a primitive string" "$ASN" \
    "  if (typeof suffix !== 'string') {" \
    '  if (false) {'

  # ---- The seal, which is itself a claim -----------------------------------
  #
  # **A row over the harness, deliberately.** "The engine touches no network
  # global" is a property of the conformance session, and the two certification
  # fixtures are what hold it. Moving the seal back to where round 2 found it —
  # after the import — is the defect, and the load-time fixture is what notices.
  mutate web "the seal goes up after the engine's chunk is loaded" "$CT" \
    '    seal = sealNetwork()
    tracker = trackDeferredWork()
    const engine = await load()' \
    '    const engine = await load()
    seal = sealNetwork()
    tracker = trackDeferredWork()'

  # The SDK's own message schema, which replaced this desk's hand-written shape
  # rules: an id of null beside a result, a string error and a fractional id all
  # satisfied those and none of them is a JSON-RPC message.
  mutate web "a frame need not be a message the SDK would accept" "$TG" \
    '  if (!JSONRPCMessageSchema.safeParse(frame).success) {' \
    '  if (false) {'

  # ---- The model answer, and what an engine can reach through it -----------
  #
  # A Response built from a ReadableStream keeps that very object as its body,
  # so a stream somebody decorated is reachable through the facade.
  mutate web "the facade carries the answer's own body stream" "$ASN" \
    '    empty || answered.body === null ? null : answered.body.pipeThrough(new TransformStream())' \
    '    empty ? null : answered.body'
  # A rejection named AbortError can carry the URL in its message and again in
  # its cause; only the classification may travel.
  mutate web "an aborted call rethrows the error it caught" "$ASN" \
    '        throw new DOMException(CALL_ABORTED, '"'"'AbortError'"'"')' \
    '        throw cause'

  # ---- The seal's barrier, which is itself a claim -------------------------
  #
  # A fixed wait is a delay an engine can out-wait. The fixture schedules at
  # five minutes, on an interval, and chained behind another timer.
  mutate web "the deferred barrier is a fixed wait again" "$CT" \
    '        leftPending = await drainDeferredWork(tracker)' \
    '        await new Promise((resolve) => setTimeout(resolve, 200))'

  # ---- The seal's two rules about what an engine leaves behind ------------
  #
  # An interval nobody cleared is a certification failure in its own right:
  # running it a few times and clearing it on the engine's behalf let a reach
  # hide behind a later tick and report a clean drain.
  mutate web "the drain runs an engine's interval on its behalf" "$CT" \
    '    pending: () => tracked.filter((entry) => entry.pending && !entry.repeating),' \
    '    pending: () => tracked.filter((entry) => entry.pending),'
  mutate web "a live interval is not reported" "$CT" \
    '    liveIntervals: () => tracked.filter((entry) => entry.repeating && !entry.cancelled),' \
    '    liveIntervals: () => [],'
  # A handle the engine itself cancelled must never be run on its behalf: doing
  # so reports a reach the engine had already decided not to make.
  mutate web "a cancelled handle is fired anyway" "$CT" \
    '    byHandle.get(handle)?.cancel()
    clear(handle)' \
    '    clear(handle)'
  # A deferred callback that throws used to leave the finally before the
  # sentinels and the timer wrappers came off, poisoning every later leg.
  mutate web "a throwing callback escapes the cleanup" "$CT" \
    '    } catch (cause) {
      drainThrew = `${(cause as Error).name}: ${(cause as Error).message}`
    } finally {' \
    '    } finally {'
  # **Not a row: the certification fixtures travelling `loadEngine`.** They do —
  # `fromCertification` calls it, which is why the loader takes its table as a
  # parameter — but a mutant that called the table directly would import the
  # same chunk under the same seal and be caught by the same sentinel, so the
  # row would report "nothing failed" for ever. What IS observable is the
  # loader's own contract, below.
  mutate web "an unregistered engine id loads something anyway" "$EN" \
    '  const load = loaders[id]
  if (load === undefined) throw new Error(`no engine chunk is registered for ${id}`)
  return load()' \
    '  return loaders[id]!()'

  # K3(b). Without the allow-list, write_file leaves the page and reaches the
  # runtime — which is the arrival the scripted server counts as a failure.
  mutate web "the allow-list check is removed" "$TG" \
    "  if (typeof name !== 'string' || !allowed.has(name)) {" \
    '  if (false) {'
  # K3(a) — and the one an inspect-and-forward gate cannot hold. A call that
  # already reads as rehearsed still may not be forwarded as it arrived: the
  # object can serialize without the member it appears to carry.
  mutate web "an evaluate that reads as rehearsed is forwarded unchanged" "$TG" \
    '  if (already) {
    // Nothing to report: the caller asked for exactly what it got. The frame is
    // still the canonical one, because canonical is what travels.
    return { verdict: '"'"'send'"'"', notice: null, frame: frame as unknown as JSONRPCMessage }
  }' \
    '  if (already) {
    return { verdict: '"'"'send'"'"', notice: null, frame: message }
  }'
  # **Deliberately not a row: reading each own property twice.** It was one, and
  # it reported "nothing failed" — correctly. `ownArguments` reads once so that
  # a getter cannot answer the check and the wire differently, but the value
  # that check is about is written unconditionally straight afterwards, so a
  # second read of `rehearsal` is overwritten and a second read of `pack` is
  # observed by nothing. The property is real and cheap; it is not, on its own,
  # observable, and a row that says otherwise for ever reads as a missing
  # safeguard rather than an unobservable one. What IS observable is the branch
  # above: an evaluate forwarded because it read as rehearsed.
  # Fail closed. A batch has no method, and "no method is harmless traffic" is
  # what let an array carrying write_file out whole.
  # **Explicitly `send`, not merely a disabled branch.** Round 3 pointed out that
  # `if (false)` let a batch fall through to the next check and be refused
  # there, so the row went red on a *different* corpus member throwing — a true
  # failure for the wrong reason. This mutant sends the unreadable frame, which
  # is the named defect, and the batch-carrying-write_file case is what fails.
  mutate web "a frame this gate cannot read is passed as harmless traffic" "$TG" \
    '  if (!isRecord(frame)) {
    return refuse(' \
    '  if (!isRecord(frame)) {
    return { verdict: '"'"'send'"'"', notice: null, frame: frame as unknown as JSONRPCMessage }
    return refuse('
  # (The third retired row was here — see the note above.)
  # A near-spelling some other reader folds to tools/call.
  mutate web "a near-spelling of tools/call is waved through" "$TG" \
    '    if (method.trim().toLowerCase() === TOOLS_CALL) {' \
    '    if (false) {'

  # The gate is not installed at all: the engine's calls go straight to the
  # socket. Both K3 rows fail together, which is the point of a wire-level gate.
  mutate web "the gate is not installed on the assistant's transport" "$ASN" \
    '  const gated = gateTransport(raw, { allowed, onGuardrail: notify })' \
    '  const gated = raw'
  # **The row this replaces only widened a TypeScript interface.** It added
  # `client?: unknown` to AssistantSession, which no engine could use and no
  # gate was bypassed by: the member-set test failed, and that demonstrated
  # interface-shape sensitivity rather than the guardrail it was named for.
  # This one is a real bypass — the gate is installed and reporting, on a decoy,
  # while the client the engine's caller is bound to speaks straight to the
  # socket — and the conformance server observes the ungated arrivals.
  mutate web "the engine's caller is bound to an ungated client" "$ASN" \
    '  const gated = gateTransport(raw, { allowed, onGuardrail: notify })' \
    '  const decoy: Transport = {
    start: () => raw.start(),
    close: () => raw.close(),
    send: (message, sendOptions) => raw.send(message, sendOptions)
  }
  gateTransport(decoy, { allowed, onGuardrail: notify })
  const gated = raw'
  # One shared transport instead of one per session: two sessions would share a
  # jpack mcp and a gate, and closing either would take the other's connection.
  # Repaired: the transport now takes the session id its caller awaited, so the
  # needle names the new signature. The property is unchanged.
  mutate web "the assistant reuses one shared transport" "$ASN" \
    'export function assistantTransport(id: string): Transport {
  if (id === '"'"''"'"') throw new NoSession()
  return new DeskWebSocketTransport(socketURL(), socketProtocols(id))
}' \
    'let sharedTransport: Transport | undefined
export function assistantTransport(id: string): Transport {
  if (id === '"'"''"'"') throw new NoSession()
  sharedTransport ??= new DeskWebSocketTransport(socketURL(), socketProtocols(id))
  return sharedTransport
}'
  # A setup that fails leaves a socket and a jpack mcp with nothing holding a
  # reference to either.
  mutate web "a failed setup leaves the connection open" "$ASN" \
    '      await close()
      throw cause' \
    '      throw cause'
  # The desk's model capability, which is what an engine is handed instead of a
  # URL: the address, the header allow-list and the suffix rule are all here.
  mutate web "the model call forwards every header it is handed" "$ASN" \
    '      if (MODEL_REQUEST_HEADERS.includes(name.toLowerCase())) headers[name] = value' \
    '      headers[name] = value'
  mutate web "the model call accepts any suffix at all" "$ASN" \
    '    const problem = suffixProblem(suffix, family)' \
    "    const problem = ''"

  # K1. The page holds no key; a header here is a credential it had to have got.
  mutate web "a credential header is restored to the model request" "$BT" \
    "  return { 'content-type': 'application/json', ...extra }" \
    "  return { 'content-type': 'application/json', authorization: 'Bearer x', ...extra }"
  # The whole reason session.model is a capability rather than a base URL: an
  # engine that reaches for a network global is an engine that can open its own
  # socket to /ws with this chassis' token. Every leg seals fetch, WebSocket,
  # XMLHttpRequest and EventSource for the duration of the engine's run.
  mutate web "the engine reaches for globalThis.fetch" "$BO" \
    '        options.call(SUFFIX, {' \
    "        globalThis.fetch('/api/assistant/relay/v1/chat/completions', {
          method: 'POST',"
  # `end` twice: a pane that renders "running" until it sees one would be right
  # either way, so what this breaks is the contract's own "exactly once".
  mutate web "end is emitted twice" "$BL" \
    "  yield { type: 'end' }
}" \
    "  yield { type: 'end' }
  yield { type: 'end' }
}"
  # The proposal taken from the prose rather than from the fenced block: a
  # worked example in an explanation becomes the document a person accepts.
  # **The reading moved to `engines/contract.ts` when the second engine landed**,
  # because it is the contract's rule and not either loop's — so this row now
  # breaks it for both of them at once, which is a stronger row than it was.
  mutate web "the proposal is taken from the prose" "$EC" \
    "  const blocks = [...(text ?? '').matchAll(FENCE)].map((match) => match[1] ?? '')" \
    "  const blocks = [(text ?? '').slice((text ?? '').indexOf('{'), (text ?? '').lastIndexOf('}') + 1)]"

  # The run's terminal event, normalized in the hook. Stop used to clear the
  # run's identity before the engine handled the abort, so no end was observed.
  #
  # **The catcher is `useAssistantRun.test.tsx` and not the pane's suite**, and
  # the difference is the point: the pane drives the built-in engine, which
  # honours its abort signal and yields `end` from its own `finally`, so the
  # hook could write nothing at all and those cases still pass. The hook's test
  # uses an engine that ignores the signal and never settles — nothing but the
  # hook can end that session.
  mutate web "Stop writes no terminal event" "$AR" \
    '    finish(run)
    release(run)
    setStatus((current) => (current === '"'"'running'"'"' ? '"'"'finished'"'"' : current))' \
    '    release(run)
    setStatus((current) => (current === '"'"'running'"'"' ? '"'"'finished'"'"' : current))'
  # (The event became `held` when the proposal's canonicalization landed in this
  # function; the row is the same claim about the same line.)
  mutate web "a second end is appended rather than dropped" "$AR" \
    "    if (held.type === 'end') {
      run.ended = true
      terminals.current.count += 1
    }" \
    '    void held'
  # **Both halves at once, because either alone holds it.** A connection whose
  # setup is still in flight is releasable two ways: the run records the handle
  # synchronously, and the run's signal is handed to the setup. Breaking one
  # leaves the other holding, and a row that breaks one reports "nothing
  # failed" — which is a true statement about that edit and a false impression
  # of the safeguard. So this row removes the signal AND the recording, which
  # is the state the finding described: nothing anywhere can close a hung setup.
  mutate web "a hung setup can be closed by nothing" "$AR" \
    '          const opened = openAssistantConnection({
            allowed: endpoint.tools,
            onEvent: (event) => push(run, event),
            sessionId,
            signal: run.controller.signal
          })
          run.connection = opened' \
    '          const opened = openAssistantConnection({
            allowed: endpoint.tools,
            onEvent: (event) => push(run, event),
            sessionId
          })'
  # An identical policy run twice: the run id is what makes the second press a
  # second submission rather than the same state value.
  # An identical policy run twice: the run id is what makes the second press a
  # second submission rather than the same state value. (The submission grew a
  # prompt name and its arguments when Fix landed; the id is still what this row
  # is about.)
  mutate web "a second run of the same policy is suppressed" "$AP" \
    '              id: (nextRun.current += 1),
              name: AUTHOR_PACK_PROMPT,
              args: { policy: typed }' \
    '              id: 1,
              name: AUTHOR_PACK_PROMPT,
              args: { policy: typed }'

  # ---- The `vercel` adapter: what the SDK makes the desk's business --------
  #
  # ADR-0001 lists four defects against this SDK and the desk's answer to each
  # is a line in the adapter. Every row here breaks one of them. The engine's
  # own suite is the catcher where the conformance legs cannot be — the legs
  # STAY green for the first row, which is the point of it.

  # **The refine hook, removed entirely.** The desk's gate is the layer that
  # holds, so every conformance leg stays green: the rehearsal still reaches the
  # runtime and `write_file` still never does. What is lost is the SDK's own
  # layer — the rewritten input the model is shown on its next turn — which is
  # the only observable consequence the hook has, and the only thing that can
  # discriminate this row. A row whose catcher were a leg would be a row lying
  # about which layer holds.
  # (The hook moved into a `refine` constant when the critic landed, so the
  # main loop and the critic's second stream are given the same one. The row is
  # the same claim about the same line, at the indentation it now has.)
  mutate web "the SDK's rehearsal hook never fires, and only the gate holds" "$VL" \
    '      [REHEARSAL_TOOL]: (input: unknown) => {' \
    '      [`${REHEARSAL_TOOL}_NEVER_CALLED`]: (input: unknown) => {'

  # The gate handed a call somebody already fixed reports nothing, and the
  # guardrail line in the tab is the only place a person learns the flag was
  # forced. K3a goes red on every leg of both engines' matrix.
  mutate web "the gate is handed the call the adapter already rehearsed" "$VL" \
    "      const args = (name === REHEARSAL_TOOL && asked.length > 0 ? asked.shift() : input) as Record<
        string,
        unknown
      >" \
    "      const args = input as Record<string, unknown>"

  # The whole reason `session.model` is a capability: the SDK composes an
  # absolute URL and the wrapper is what reduces it to a suffix the desk admits.
  # Passing it through hands the desk's own capability a URL, which it refuses —
  # and this row proves the wrapper is the layer doing the reducing.
  mutate web "the SDK's absolute URL is handed to the capability whole" "$VR" \
    "  const suffix = url.slice(PLACEHOLDER_ORIGIN.length + 1)
  // The query travels **inside the suffix**" \
    '  return url
  const suffix = url.slice(PLACEHOLDER_ORIGIN.length + 1)
  // The query travels **inside the suffix**'

  # K1. `createAnthropic` throws without a key, so the provider is handed a
  # placeholder — and a wrapper that forwarded whatever the SDK set would send
  # `x-api-key` to this desk's own route. The allow-list is the guard; the
  # capability's own is the second one, and this row shows the first is real.
  mutate web "the wrapper forwards every header the SDK set" "$VR" \
    '      if (PROTOCOL_HEADERS.includes(name.toLowerCase())) headers[name] = value' \
    '      if (PROTOCOL_HEADERS.length >= 0) headers[name] = value'

  # K2. The model is offered the runtime's own five and nothing else. A sixth
  # tool bound here would be a tool the session never handed over — and the gate
  # below would still refuse the call, which is why the row that matters is the
  # one about what the model was **shown** rather than what it reached.
  mutate web "a tool the session never offered is bound to the model" "$VL" \
    '  for (const tool of tools) {' \
    "  for (const tool of [...tools, { name: 'write_file', description: '' }]) {"

  # `end` exactly once, on the path an error takes.
  mutate web "the vercel engine ends twice on the error path" "$VL" \
    "  const finish = async (): Promise<void> => {
    await channel.push({ type: 'end' })" \
    "  const finish = async (): Promise<void> => {
    await channel.push({ type: 'end' })
    await channel.push({ type: 'end' })"

  # **Retired: "the rejection guard is left installed after the run".** There is
  # no guard to leave installed. Review round 1 was right that a page listener
  # keyed on an error *name* suppresses every rejection carrying it, an
  # unrelated operation's included, for as long as a run is open — so the leak
  # is closed at its cause instead, and the row that replaces this one is "the
  # SDK's result promises are read and left unclaimed", below.

  # The registry's own entry. A `vercel` that loaded `builtin` would pass every
  # leg twice over and certify nothing — which is exactly what the fallback this
  # PR removed used to do on purpose.
  mutate web "the vercel entry in the registry points at builtin" "$EN" \
    "  vercel: async () => (await import('./vercel')).vercel" \
    "  vercel: async () => (await import('./builtin')).builtin"

  # ---- What review round 1 found, each broken again ------------------------
  #
  # **The one that deadlocked.** `drain` takes an entry off the queue before
  # yielding it, so a consumer that stops at exactly that event leaves a
  # delivery nothing else can settle: not the queue, and not the loop that never
  # resumes. The producer waited on it for ever.
  mutate web "the drain does not settle the event it was yielding" "$VC" \
    '        releaseAll()
      }' \
    '        void releaseAll
      }'
  # A member of the SDK's result read and left unclaimed is a rejection nobody
  # on the page can catch. The mutant reads one and claims nothing, which is
  # the defect exactly — and the count is measured at Node's own handler,
  # because jsdom never turns such a rejection into a window event.
  mutate web "the SDK's result promises are read and left unclaimed" "$VL" \
    '    claimPromises(result)' \
    '    void claimPromises
    void (result as { text?: unknown }).text'
  # The contract has a `reasoning` event and the SDK has three part types for
  # it. An adapter that drops them decides, on the desk's behalf, that what the
  # model said about its own reasoning is not worth showing.
  # (Needled with the comment above it, because the critic's second stream now
  # reads the same part type and a bare needle would be ambiguous.)
  mutate web "the SDK's reasoning parts are dropped" "$VL" \
    "      // own reasoning is not worth showing.
      if (part.type === 'reasoning-delta') {" \
    '      // own reasoning is not worth showing.
      if (false) {'
  # K2's other half: the model is shown the contract the runtime enforces **or
  # it is shown nothing**. A permissive schema written here is this desk telling
  # the model that anything goes for a tool whose contract it does not know.
  mutate web "a tool with no served schema is given an invented one" "$EC" \
    '  if (tool.inputSchema === undefined || tool.inputSchema === null) {' \
    '  if (tool.inputSchema === undefined) return { type: 42 }
  if (false) {'
  # jsdom has no `requestIdleCallback`, so a reach scheduled on one did nothing
  # during certification and ran in Chrome after the seal lifted. The harness
  # installs one; this mutant installs it without tracking it.
  mutate web "an idle callback is scheduled but not tracked" "$CT" \
    "      return record('requestIdleCallback', label, run, schedule, clear, false, timeout ?? 1)" \
    '      void record
      void label
      void clear
      return schedule(run)'
  # A canceller that is not wrapped leaves a cancelled callback pending in the
  # bookkeeping, and the drain fires it — a reach attributed to an engine that
  # had already decided not to make it.
  mutate web "a cancelled immediate is fired by the drain anyway" "$CT" \
    "    if (typeof realClearImmediate === 'function') {
      keep('clearImmediate')
      scope.clearImmediate = (handle: unknown) => forget(handle, (inner) => realClearImmediate(inner))
    }" \
    '    void realClearImmediate'

  # ---- What review round 2 found, each broken again ------------------------
  #
  # The in-flight slot is a single slot. Two readers would overwrite each
  # other's, and the overwritten entry would be in neither the queue nor the
  # slot — a delivery nothing can ever settle.
  mutate web "a second consumer of the channel is admitted" "$VC" \
    '      if (consuming) throw new ChannelHasOneConsumer()' \
    '      void ChannelHasOneConsumer'
  # **The whole regression, not half of it.** Round 3 was right that removing the
  # channel's terminal event alone kills the mutant for the wrong reason: the
  # named claim is about `return()` semantics, so the mutant has to be the shape
  # that had them wrong — an async generator again, with `end` yielded from its
  # `finally` and nothing pushed on the channel. The return-semantics test then
  # fails for the reason it is named after: the first `return()` answers
  # `{ done: false }` carrying an event.
  mutate web "the run is a generator again, with end yielded from its finally" "$VL" \
    '  const finish = async (): Promise<void> => {
    await channel.push({ type: '"'"'end'"'"' })
    channel.close()
  }
  /**
   * The run itself starts on the consumer'"'"'s first `next()`.
   *
   * A consumer that opens a session and leaves without asking for an event has
   * asked the model nothing, and this is what makes that true.
   */
  const open = (): AsyncGenerator<AssistantEvent> => {
    const loop = drive().catch(async (cause: unknown) => {
      // A cancelled run is the viewer stopping the session, or a consumer
      // walking away: it ends the run and there is no failure to report.
      if (isCancelled(cause) || gate.signal.aborted) return
      await channel.push({ type: '"'"'error'"'"', message: describe(cause) })
    })
    // Nothing else awaits this; a rejection out of the catch above would be
    // unhandled. It cannot reject, and this is what says so.
    void loop.then(finish, finish)
    return channel.drain()
  }

  return eventIterator({ gate, open })' \
    '  const finish = async (): Promise<void> => {
    channel.close()
  }
  const open = (): AsyncGenerator<AssistantEvent> => {
    const loop = drive().catch(async (cause: unknown) => {
      if (isCancelled(cause) || gate.signal.aborted) return
      await channel.push({ type: '"'"'error'"'"', message: describe(cause) })
    })
    void loop.then(finish, finish)
    return channel.drain()
  }
  void eventIterator
  return (async function* (): AsyncGenerator<AssistantEvent> {
    try {
      yield* open()
    } finally {
      gate.close()
      yield { type: '"'"'end'"'"' }
    }
  })()'

  # `return()` has to cancel **before** it awaits: what a pending `next()` is
  # waiting on is exactly what the cancel ends, and the queued close cannot run
  # until that `next()` settles.
  mutate web "the iterator cancels only after it has waited" "$EC" \
    '      options.gate.close()
      await events?.return(undefined)' \
    '      await events?.return(undefined)
      options.gate.close()'

  # A `next()` that finds the iterator closed under it must not wait on the same
  # closing promise the `return()` is waiting on: the `return()` registered
  # first and would settle first, telling the consumer the iterator was closed
  # before the `next()` it was holding had come back.
  mutate web "a closed iterator's pending next waits on the closing too" "$EC" \
    '      if (options.gate.isClosed()) return done
      if (step.done === true) {' \
    '      if (step.done === true || options.gate.isClosed()) {'
  # The connection is closed once however many times it is released: the abort
  # closes it and the setup's own failure closes it again.
  mutate web "the connection is closed once per release, not once" "$ASN" \
    '    shutting ??= (async () => {' \
    '    shutting = (async () => {'
  # ---- What review round 5 found: the two orderings a signal cannot hold ---
  #
  # A session already aborted is a run that is over. It used to abort the run's
  # controller and nothing else, so an engine still reported a tier, still built
  # a provider, and still made a request — for a session that never happened.
  mutate web "a session aborted before the run is not closed, only aborted" "$EC" \
    "  if (session.signal.aborted) close()" \
    '  if (session.signal.aborted) stop.abort()'
  # **Closed, then aborted, then released.** When the thing an engine was
  # awaiting wins its race with the abort by a microtask, the loop resumes
  # holding a value; aborting cannot stop it delivering that, and a flag the
  # delivery path reads can — but only if it is set first.
  mutate web "the run is aborted and released before it is marked closed" "$EC" \
    "    closed = true
    session.signal.removeEventListener('abort', close)
    stop.abort()
    release()" \
    "    session.signal.removeEventListener('abort', close)
    stop.abort()
    release()
    closed = true"
  # And the delivery path is where that flag is read. A loop that resumed after
  # the run closed can yield whatever it likes; none of it is anybody's.
  mutate web "the delivery path does not read the closed gate" "$EC" \
    '      if (options.gate.isClosed()) return done
      if (step.done === true) {' \
    '      if (step.done === true) {'
  # A thunk, so a closed run starts no work at all: taking a promise meant the
  # call had already been made by the time the signal was read.
  mutate web "an await on the world is started before the signal is read" "$EC" \
    '  if (signal.aborted) return Promise.reject(new RunCancelled())
  let started: Promise<T>' \
    '  let started: Promise<T>'

  # ---- What review round 4 found: the class, broken again -----------------
  #
  # Every await on the world outside an engine is bounded by the run's own
  # signal, because aborting the awaited thing only works where the thing
  # honours a signal — a model request does, a `tools/call` over a socket does
  # not. Each row here unbinds one of them.
  mutate web "the vercel adapter's tool call is not bounded by the run" "$VL" \
    '  const callTool = guardedCallTool(session, gate.signal)' \
    '  const callTool: CallTool = (name, args) => session.callTool(name, args)'
  mutate web "the built-in engine's tool call is not bounded by the run" "$BL" \
    '    const callTool = guardedCallTool(session, signal)' \
    '    const callTool: CallTool = (name, args) => session.callTool(name, args)'
  # **Retired: "the runtime is asked without reading the run's signal first".**
  # `guardedCallTool` still reads the signal before it dispatches, and that is
  # the readable statement of the rule where the rule lives — but it is no
  # longer the line that *holds* it. `withAbort` takes a thunk and refuses a
  # closed run **without invoking it**, so removing the explicit check changes
  # nothing anyone can observe, and a row saying otherwise would read as a
  # missing safeguard rather than a doubly-held one. The row that holds it is
  # "an await on the world is started before the signal is read", above.
  # **Retired: "a session abort aborts the SDK and abandons nothing".** It was
  # discriminating when the abandon was the only thing that could settle a
  # `next()` waiting behind a tool call. It is not any more: the tool call is
  # bounded by the run's signal, so the loop unwinds on its own and its `end`
  # reaches the waiting consumer, which the closed gate then drops. What
  # `abandon()` still does is release a producer nobody will ever take from
  # after a consumer's `return()`, and the channel's own tests hold that — "the
  # drain does not settle the event it was yielding", below. Two layers, and
  # the row that broke one of them stopped saying anything.
  # The drain runs what an engine left behind in the order a browser would
  # have, not all at once: a sixty-second idle callback must not run before the
  # one-second timer that was going to cancel it.
  mutate web "the drain fires every pending handle at once again" "$CT" \
    '    tracker.advanceTo(next.dueAt)
    next.fire()' \
    '    for (const entry of tracker.pending()) entry.fire()'

  # A timeout is a deadline, not a hint: firing every positive one after a
  # millisecond credits an engine with work it would have cancelled first.
  mutate web "the idle callback is fired before its deadline" "$CT" \
    '            realSetTimeout(wrapped, timeout ?? 1)' \
    '            realSetTimeout(wrapped, timeout === undefined ? 1 : Math.min(1, timeout))'

  # A shim that answers zero to `timeRemaining()` runs the ordinary idle
  # pattern, watches it decline to do anything, and certifies a clean leg —
  # while a browser gives it a real budget and lets it reach.
  mutate web "the idle deadline reports no budget at all" "$CT" \
    '          timeRemaining: () => Math.max(0, IDLE_BUDGET_MS - (Date.now() - startedAt))' \
    '          timeRemaining: () => 0'
  # And the other half of the same object: code that waits for its own timeout.
  mutate web "the idle deadline never reports its own timeout" "$CT" \
    '        const didTimeout = deadlineAt !== undefined && now() >= deadlineAt' \
    '        const didTimeout = false'

  # ---- The thinking tier, and the refutation pass --------------------------
  #
  # ADR-0001 puts both in the desk rather than in an engine: one table per
  # endpoint family, five states of which two are the desk's to report, and a
  # refutation pass held to three rules no prototype kept on its own. Each row
  # below breaks one of them.
  TH=web/src/assistant/thinking.ts
  RF=web/src/assistant/refutation.ts
  BA=web/src/assistant/engines/builtin/providers/anthropic.ts

  # `off` is expressed by OMISSION: Anthropic rejects `{"type":"disabled"}` on
  # the models that always think and several endpoints answer 400 to
  # `reasoning_effort: "none"`, so a tier member sent at `off` is a request a
  # desk configured for no thinking never agreed to make.
  mutate web "the tier member is sent when the tier is off" "$TH" \
    "    return null
  }
  const effort = tier === 'ultra' ? 'xhigh' : 'high'" \
    "    return wireFor('on', dialect)
  }
  const effort = tier === 'ultra' ? 'xhigh' : 'high'"

  # The degrade is sticky for the session: the member the endpoint refused is
  # never sent again. Without this the retry re-sends what was just refused,
  # and every request after it does too.
  mutate web "a refused tier member is sent again on the next request" "$TH" \
    "      unavailable = true
      // The status, and the desk's own sentence. The endpoint's body is
      // matched against a closed list and never quoted past it." \
    "      // The status, and the desk's own sentence. The endpoint's body is
      // matched against a closed list and never quoted past it."

  # Anthropic spends the thinking budget out of `max_tokens`, so the table that
  # chooses the budget chooses the maximum with it. Without the allowance the
  # budget is not below the maximum and every enabled-dialect endpoint refuses
  # the request — which the conformance endpoint now enforces.
  mutate web "the enabled dialect's response allowance is dropped" "$TH" \
    "          max_tokens: budget + RESPONSE_TOKENS" \
    "          max_tokens: budget"

  # **The line, once.** A degrade said on every request is a reader learning to
  # skip it; the split-signature leg is where a second one can actually happen,
  # because a fresh block arrives on every turn.
  mutate web "the degrade is announced more than once" "$TH" \
    "    truncated(reason) {
      if (unavailable) return null
      unavailable = true
      return notice('unavailable', reason)
    }," \
    "    truncated(reason) {
      unavailable = true
      return { type: 'thinking_unavailable', detail: thinkingNotice('unavailable', reason) }
    },"

  # The slot changes at the transition and the request that leaves is already
  # the degraded one, so a notice held until the next stream part left the tab
  # saying `thinking on` about a request that carried none — for as long as it
  # took, or for ever.
  mutate web "the truncation notice waits for the next stream part" "$VL" \
    "    const said = slot.truncated(reason)
    if (said !== null) await channel.push(said)" \
    "    const said = slot.truncated(reason)
    if (said !== null) void Promise.resolve().then(() => channel.push(said))"

  # **A turn is not a capability.** One unsolicited passage at tier off used to
  # label the model "always thinks" for the whole session.
  mutate web "one reasoning turn makes a model that always thinks" "$TH" \
    "        if (always || reasoningRun < PERMANENCE) return null" \
    "        if (always || reasoningRun < 1) return null"
  # …and the other direction: one quiet turn used to strip the tier from every
  # request after it, on an endpoint that reasons perfectly well.
  mutate web "one quiet turn makes an endpoint with no thinking" "$TH" \
    "      quietRun += 1
      if (quietRun < PERMANENCE) return null" \
    "      quietRun += 1
      if (quietRun < 1) return null"
  # A turn that only called a tool is no evidence that an endpoint will not
  # reason, and counting it made a tool-first session degrade itself.
  mutate web "a tool-only turn counts as evidence of no thinking" "$TH" \
    '      if (!hadText) return null' \
    '      void hadText'
  # …and the same rule in the other tier, where it was written and not applied:
  # a reasoning answer, a tool call and a second reasoning answer never reached
  # "always", because the tool call in the middle wiped the count.
  mutate web "a tool-only turn wipes the off-tier count" "$TH" \
    "      if (!hadText) return null
      if (tier === 'off') {" \
    "      if (tier === 'off') {
        if (!hadText) reasoningRun = 0"


  # **The conjunction.** `Unsupported parameter` and `Extra inputs are not
  # permitted` are what an endpoint says about *any* member, so prose alone
  # degraded the tier on a refusal about `temperature` and hid a real failure
  # behind a misleading "thinking is unavailable" line. (This replaces the
  # "any 400" row, which did not prove the conjunction it was named for.)
  mutate web "a refusal need not name a member the desk sent" "$TH" \
    "  if (status !== 400) return false
  return namesSent(members).some((name) => message.includes(name))" \
    "  if (status !== 400) return false
  return /unsupported parameter|extra inputs are not permitted|not supported/i.test(message)"

  # vercel/ai#19663. A block whose signature came back as a fragment is removed
  # rather than sent: a malformed thinking block is a request the endpoint
  # refuses, and sending one is the defect the ledger exists for.
  mutate web "a truncated thinking signature is sent back anyway" "$VR" \
    "      if (sent === undefined || !isTruncatedSignature(sent.signature, carried)) return true" \
    "      if (true) return true"

  # **Block identity, by position.** Comparing a carried signature against every
  # signature ever ledgered threw away a later block whose own signature was
  # legitimately shorter and happened to be a prefix of an earlier one.
  mutate web "a carried signature is compared with every signature ever seen" "$VR" \
    "      const sent = signed[at]
      at += 1" \
    "      const sent = signed.find((one) => isTruncatedSignature(one.signature, String(block.signature)))
      at += 1"

  # **A filter is not a rebuild.** The body was composed before the slot
  # degraded, so filtering the damaged block out of it leaves a request that
  # still asks for thinking while no longer carrying a block the endpoint
  # signed — a continuation the split-signature endpoint refuses.
  mutate web "the degraded request is filtered rather than rebuilt" "$VR" \
    '  shape.retier(payload, membersAfter())' \
    '  void membersAfter'

  # The block ids repeat every turn on the Anthropic wire, so a ledger with no
  # turn boundary concatenates one turn's signature onto the next and reports
  # the next turn's whole signature as a fragment of the pair — degrading a
  # perfectly good session on its third turn.
  mutate web "the signature ledger has no turn boundary" "$VR" \
    "    boundary: settle," \
    "    boundary: () => {},"
  # **Two blocks signed the same are two blocks.** Collapsing them left a later
  # block compared against the wrong entry, or against none, and sent.
  mutate web "two blocks signed the same collapse into one" "$VR" \
    "      if (signature !== '') done.push({ conversation, turn, block, signature })" \
    "      if (signature !== '' && !done.some((one) => one.signature === signature)) {
        done.push({ conversation, turn, block, signature })
      }"
  # The critic's history carries none of the main loop's blocks, so comparing
  # its first block against the loop's first signature compares two different
  # conversations' positions.
  mutate web "the critic is compared against the main loop's blocks" "$VR" \
    "      return done.filter((entry) => entry.conversation === conversation)" \
    "      return [...done]"

  # **The verdict is the runtime's.** The critic's prose disagrees with the
  # runtime on purpose on both conformance legs, so a verdict read out of it is
  # wrong in both directions.
  mutate web "the refutation verdict is read out of the critic's prose" "$RF" \
    "    critique(modelText) {
      return { refuted: verdictOf(checks), checks: [...checks], text: quoteOf(checks), modelText }
    }" \
    "    critique(modelText) {
      return {
        refuted: /refut/i.test(modelText),
        checks: [...checks],
        text: quoteOf(checks),
        modelText
      }
    }"

  # A non-empty list of checks before "not refuted" is rendered. A critic that
  # talked and asked the runtime nothing has produced no evidence, and a
  # refutation line on that proposal is a clean bill of health nobody measured.
  mutate web "a proposal the critic never checked is rendered as not refuted" "$RF" \
    "  if (critique === null || critique.checks.length === 0) return {}" \
    "  if (critique === null) return {}"

  # **The separation, broken in the one way that matters.** The distinction is
  # a type: the refusal arm of `ToolOutcome` has no `report`, so a call the gate
  # refused cannot reach the recorder from the loop at all. The row that merely
  # removed the old boolean was not discriminating, because today's guardrail
  # prose happens not to parse as JSON — so this one hands the recorder the
  # refusal's words **shaped like a runtime answer**, which is the failure the
  # separation exists to make impossible.
  mutate web "the refusal continuation is handed the recorder" "$BL" \
    "      if (outcome.kind === 'answered') outcome.report(recorder, call.name)" \
    "      recorder.saw(
        call.name,
        outcome.kind === 'answered' ? outcome.text : '{\"status\":\"invalid\"}'
      )"
  # The recorder itself, which must not invent a status the runtime never
  # used for an answer that carried none.
  mutate web "an answer with no runtime status is recorded as refused" "$RF" \
    "      const said = statusOf(text)
      if (said === null) return" \
    "      const said = statusOf(text)
      if (said === null) {
        checks.push({ tool, status: 'refused', diagnostics: 0 })
        return
      }"
  # An object literal inherits `toString`, `constructor` and the rest, so `in`
  # made a tool the table does not name into a check.
  mutate web "the settled table is read through its prototype" "$RF" \
    '  return Object.hasOwn(SETTLED_STATUS, tool)' \
    '  return tool in SETTLED_STATUS'

  # The runtime has one word per command for "this went through". A rule that
  # read `status !== 'valid'` over both tools would report every session ever
  # run as refuted, because a rehearsal evaluation answers `evaluated`.
  mutate web "one settled status is used for every check" "$RF" \
    "  return checks.length > 0 && checks.some((check) => check.status !== SETTLED_STATUS[check.tool])" \
    "  return checks.length > 0 && checks.some((check) => check.status !== 'valid')"

  # The pass is gated on the tier: a desk configured for no thinking runs no
  # critic, makes no second conversation, and spends no extra call.
  mutate web "the refutation pass runs even with the tier off" "$TH" \
    "    runsRefutation() {
      if (tier === 'off') return false" \
    "    runsRefutation() {
      if (false) return false"

  # **Both prompts, or neither.** Starting on the author prompt alone handed the
  # engine an empty `testPrompt`, and the critic then ran on this desk's one
  # sentence with none of the runtime's instructions.
  # **A read that failed is a settled read.** `data` alone stays undefined for
  # ever on the error state, so a `prompts/get` the runtime refused deadlocked
  # the whole session: the authoring prompt had arrived, no engine started, and
  # the tab said nothing about why.
  mutate web "a refused testing prompt is waited for for ever" "$AP" \
    '    testPrompt.data === undefined &&
    testPrompt.error === null' \
    '    testPrompt.data === undefined'

  mutate web "the run starts before the runtime's testing prompt arrives" "$AP" \
    '    if (waitingForTest) return' \
    '    void waitingForTest'
  # …and the other half: with no testing prompt at all the pass must report that
  # it cannot run rather than putting a critic in front of a document with only
  # the desk's sentence to go on.
  mutate web "the critic runs on the desk's sentence alone" "$RF" \
    "  if (testPrompt.trim() !== '') return null" \
    "  if (true) return null"

  # The critic runs under the **run's** gate, not on the session capability
  # directly: a viewer who presses Stop during the pass must end it where they
  # would have ended the loop above.
  mutate web "the critic calls the runtime outside the run's gate" "$BL" \
    "        ? yield* refute({ session, provider, tools, callTool, slot, signal, document: proposal.document })" \
    "        ? yield* refute({ session, provider, tools, callTool: session.callTool, slot, signal, document: proposal.document })"

  # **Retired: "the critic's stream is not bounded by the run".** It reported
  # NOT DISCRIMINATING, correctly. The critic's `streamText` is given the run's
  # `abortSignal`, so the SDK ends its own stream on a cancel and the desk's
  # bound on the *read* changes nothing anyone can observe — exactly as it does
  # on the main loop's read, which has never had a row either. The bound is kept
  # because "the SDK honours the signal" is a premise and not a rule (the same
  # argument `relay.ts` makes about checking an address the SDK composed), and
  # its absence from this table is stated here rather than left as a gap. What
  # IS observable is the seam on the tool dispatch, above.

  # A signal that aborted *while* the work was running fired before the
  # listener existed, so the await hung on whatever the work returned — which
  # is exactly what a capability that ends the session from inside a request
  # does.
  mutate web "a run that closed while the work ran is never noticed" "$EC" \
    "    if (signal.aborted) cancelled()
  })" \
    "    void cancelled
  })"

  # **Reasoning is for the person reading the tab.** The runtime is asked about
  # documents, and a tool call carrying the model's own reasoning would put it
  # in a project's audit trail.
  mutate web "the model's reasoning is sent to the runtime with the tool call" "$BA" \
    "      args: (block.input ?? {}) as Record<string, unknown>," \
    "      args: {
        ...((block.input ?? {}) as Record<string, unknown>),
        reasoning: content.find((held) => held.type === 'thinking')?.thinking
      } as Record<string, unknown>,"

  # ---- The proposal as a diff, and accepting it into the draft -------------
  #
  # ADR-0001: "the desk renders the diff and applies an accepted proposal
  # through the span-preserving writer. No engine event writes anything." Each
  # row below breaks one half of that sentence.
  PD=web/src/assistant/proposalDiff.ts
  AC=web/src/assistant/acceptProposal.ts

  # **The diff is computed, never quoted.** There is no sentence from the model
  # for a mutant to quote — the contract's proposal event carries a document and
  # its unknowns, and nothing else — so what this row breaks is the computation
  # itself: the comparison stops being against the draft, and every member is
  # reported as new, which is exactly what quoting a model that says "I rewrote
  # the pack" would produce.
  mutate web "the diff is not computed against the draft" "$PD" \
    '  const draft = readDraft(draftText)' \
    '  const draft = readDraft(undefined)'

  # **A save that answers about another file.** A PUT takes as long as it takes;
  # without the ticket, A's read-back became B's base and B's identity.
  mutate web "an old save rebases whatever buffer is on screen" "$BUF" \
    '      if (expect.generation !== generationNow.current) return false
      if (seeded.current !== undefined && seeded.current !== expect.path) return false
      if (fresh.path !== expect.path) return false
    }
    seeded.current = fresh.path' \
    '      void expect
    }
    seeded.current = fresh.path'
  # Work held beside the bytes is still work: a reload asked for before it was
  # typed must go stale, exactly as a commit makes it.
  mutate web "text held beside the bytes moves no revision" "$HT" \
    '    if (heldChanged(now.current.get(pointer), draft)) touchNow.current()' \
    '    void pointer'

  # **A save that finished with nobody here to take its answer.** The per-save
  # callbacks arrive through react-query's observer, and a reload landing — or
  # leaving the pack — detaches it: the write completes on disk and the page
  # would otherwise say nothing at all about it.
  mutate web "a save that answers to nobody is not reported" "$PV" \
    '          if (!delivered && pathNow.current === path) setUnaccounted(true)' \
    '          void delivered'
  # Releasing held text is not an edit — the write that follows it is — and the
  # operand does both in one gesture, so counting both makes the revision
  # something other than a count of edits.
  mutate web "every hold counts as an edit, released or not" "$HT" \
    '    if (heldChanged(now.current.get(pointer), draft)) touchNow.current()' \
    '    touchNow.current()'

  # **A reload that lands over an edit made while it was in flight.** The
  # generation moves only where the buffer is put down, so an edit — a
  # keystroke, or an accepted proposal — left the ticket matching, and the
  # answer was adopted over the work with its undo entry.
  mutate web "a reload adopts over an edit made while it was in flight" "$BUF" \
    '        if (expect.revision !== edits.current) return false' \
    '        void expect.revision'

  # **What ingestion hands on cannot be moved afterwards.** The snapshot travels
  # to the pane on the run's event list; unfrozen, anything holding the event
  # can reach into it between the memoised diff and the accept, and the writer
  # then writes a document nobody was shown.
  mutate web "the ingested proposal is left mutable" "$AR" \
    '  return Object.freeze(value)' \
    '  return value'
  # **One canonicalization, and the guard that keeps it one.** A second round
  # trip added back "for safety" is a second reading of one proposal.
  mutate web "the writer canonicalizes the proposal again" "$AC" \
    '  const proposed = document' \
    '  const proposed = JSON.parse(JSON.stringify(document)) as unknown'

  # **A row's identity is its kind and its pointer.** A proposal that replaces
  # one rule with a rule of another id produces two rows about position 0, and
  # a list keyed on the pointer alone hands React one key for both.
  mutate web "two rows about one position share an identity" "$PD" \
    '  return { key: `${entry.status}:${entry.pointer}`, ...entry }' \
    '  return { key: entry.pointer, ...entry }'

  # A member the proposal did not move must not be written. The fixture drafts
  # are indented with four spaces, so a member written again comes back with
  # this module's own layout and the byte comparison sees it.
  mutate web "an unchanged member is written again under Accept" "$AC" \
    '    if (sameValue(before, after)) continue' \
    '    if (false) continue'

  # **The whole document, instead of a splice.** This is the row for "Accept
  # writes through something other than the editing session's writer": the pane
  # cannot reach `commit` — the context does not carry one — so the reachable
  # version of that defect is the writer being bypassed one layer down, and the
  # accept re-serializing the file it was asked to edit.
  mutate web "Accept writes the whole document instead of splicing it" "$AC" \
    '  const readable =
    current.index.parseError === undefined &&
    agreesWithParse(current.text, current.index).length === 0' \
    '  const readable = false'

  # One accept is one undo entry. A second write with its own key is a second
  # entry, so the first Undo leaves the author where the accept put them.
  mutate web "Accept pushes a second undo entry" "$AP" \
    '      { coalesceKey: `assistant-accept:${(accepts.current += 1)}` }
    )
    setAccepted(landed.text)' \
    '      { coalesceKey: `assistant-accept:${(accepts.current += 1)}` }
    )
    write((current) => applyProposal(current, proposed), {
      coalesceKey: `assistant-accept:${(accepts.current += 1)}`
    })
    setAccepted(landed.text)'

  # **A proposal is an edit of the draft it was given.** The run captures its
  # baseline where it starts; the comparison against the bytes on the page now
  # is what refuses to write a document the author has typed past.
  mutate web "the draft may move under a proposal and still be accepted" "$AP" \
    '    baseline.bytes === draft &&' \
    '    true &&'
  # And the diff has to be about the same bytes Accept would apply, or what is
  # on screen is an edit nobody proposed.
  mutate web "the diff follows the live buffer instead of the baseline" "$AP" \
    '    () => (proposed === undefined ? undefined : diffProposal(baseline?.bytes, proposed)),' \
    '    () => (proposed === undefined ? undefined : diffProposal(draft, proposed)),'
  # The save latch is claimed synchronously and reported to React a render
  # later, so the click has to ask again.
  mutate web "Accept trusts the rendered busy state at the click" "$AP" \
    "    if (busyNow.current() !== '' || !onBaseline) return" \
    '    void onBaseline'
  # "Accepted" is a comparison. Stored, it outlived the bytes it was about:
  # Undo put the draft back and the pane still said the proposal was in it.
  mutate web "an accepted proposal stays accepted after Undo" "$AP" \
    "    : accepted !== null && draft === accepted
      ? 'accepted'
      : 'open'" \
    "    : accepted !== null
      ? 'accepted'
      : 'open'"

  # The reading route has no buffer a save can reach, so it is offered one line
  # rather than a control.
  mutate web "Accept is drawn on the reading route" "$AP" \
    '            {editing ? (' \
    '            {true ? ('

  # A run still in flight is about to replace the events the proposal is on.
  mutate web "Accept is enabled while the session is still running" "$AC" \
    '  if (input.running) {
    return { enabled: false, why: '"'"'The session is still running. Stop it or wait for it to end.'"'"' }
  }' \
    '  if (false) {
    return { enabled: false, why: '"'"''"'"' }
  }'

  # The draft in the first message is what makes the proposal an edit of this
  # document rather than a document about nothing.
  mutate web "the draft is left out of the first message" "$AP" \
    '    startRun(withDraft(prompt.data.text, draftNow.current))' \
    '    startRun(prompt.data.text)'

  # **One canonicalization, where the event arrives.** An engine may put a live
  # object on `document`, and three readings of one getter are three documents:
  # the diff describes A, the pane displays B, the writer writes C. The row
  # takes the canonicalization out of the hook, so the pane's own readers each
  # take their own.
  mutate web "the proposal reaches the pane uncanonicalized" "$AR" \
    "    const held = event.type === 'proposal' ? canonicalProposal(event) : event" \
    '    const held = event'

  # `fix_pack` works from the validator's report, and what it is given is the
  # runtime's own bytes: a re-serialization of a parse of them is this desk's
  # spelling of a refusal it did not write.
  # ---- Describe it, and Create writing what it proposed --------------------
  #
  # ADR-0001 again, in this chunk's form: nothing is written until Create is
  # pressed, the name field is the authority over identity, and what is written
  # is the canonical frozen snapshot that was on screen. Each row below breaks
  # one half of one of those.
  DI=web/src/shell/DescribeIt.tsx

  # **The desk's shaping, skipped.** The proposal's own `id` and `title` are a
  # model's statement about a document nobody has named yet; writing them is a
  # pack arriving under an identity the person creating it never chose.
  mutate web "Create writes the proposal without shaping it" "$X" \
    '      return { text: packFromProposal(source.document, { name, description, slug, idBase }) }' \
    '      return { text: `${JSON.stringify(source.document, null, 2)}\n` }'

  # And the narrower half of the same claim: the shaping runs, and the name it
  # is given comes from the proposal instead of from the field above it.
  mutate web "the name field loses to the name the proposal gave itself" "$X" \
    '{ name, description, slug, idBase }) }' \
    '{
        name: String((source.document as { title?: unknown }).title ?? name),
        description,
        slug,
        idBase
      }) }'

  # A run still in flight is about to replace the events the proposal is on,
  # and Create with a half-finished session behind it writes a document nobody
  # has seen the end of.
  mutate web "Create is enabled while the assistant is still running" "$X" \
    '    describe.blocking === '"'"''"'"' &&' \
    '    true &&'

  # **The snapshot, not the event.** An engine may put a live object on
  # `document`; the run hook ingests it once and hands on plain frozen data, and
  # this row makes the canonical event carry the live one instead — so the
  # document the dialog displayed and the document Create writes are two
  # readings of one getter.
  mutate web "the proposal is written from event.document, not the snapshot" "$AR" \
    "    type: 'proposal',
    document," \
    "    type: 'proposal',
    document: event.document,"

  # The section renders as a control only where there is an assistant to run.
  # An endpoint with no key on this machine is a session that cannot start, and
  # a control that would refuse is worse than a sentence saying where the key
  # goes.
  mutate web "Describe is drawn with no key stored on this machine" "$DI" \
    "  const usable = slot.state === 'configured' && slot.endpoint !== null && slot.keyPresent" \
    "  const usable = slot.state === 'configured' && slot.endpoint !== null"
  # **Aimed at the sentence, which is what differs.** Breaking `usable` cannot
  # discriminate: a configuration this desk could not read falls through to the
  # defaults, so the endpoint is null and the control is withheld by that
  # clause anyway. What is not the same is what a reader is told — "no
  # assistant is configured" is an absence this page did not establish about a
  # file it could not open, and it offers a repair that sends them to a form
  # which will not write either.
  # **Admin is where a reader goes to find out why**, so it was the worst place
  # to be still asserting an absence: it printed "none — no endpoint
  # configured" over a form painted as editable, on the same page as its own
  # notice saying the file could not be read.
  AS2=web/src/assistant/AssistantSection.tsx
  mutate web "Admin claims no endpoint from a file it could not read" "$AS2" \
    "              {unavailable
                ? 'this desk could not read its own configuration'
                : endpoint === null" \
    "              {false
                ? 'this desk could not read its own configuration'
                : endpoint === null"
  # And the fields with it: they are the built-in defaults there, and typing
  # into them would compose a write over a file nobody has seen.
  mutate web "the form is editable over a file this desk could not read" "$EF" \
    '      <fieldset disabled={busy || unavailable}>' \
    '      <fieldset disabled={busy}>'
  # The tab's own half of the same sentence: it renders the state directly
  # rather than through `unusableBecause`, so breaking one does not break the
  # other and each has its own row.
  mutate web "the tab describes an unreadable configuration as having no assistant" "$AP" \
    "        {slot.state === 'unavailable'" \
    '        {false'
  mutate web "an unreadable configuration is described as having no assistant" "$DI" \
    "      slot.state === 'unavailable'
        ? UNREAD_CONFIGURATION" \
    "      false
        ? UNREAD_CONFIGURATION"

  # Closing the dialog ends the session, and it has to end it **through the run
  # hook**: an unmount alone aborts the iterator and closes the socket without
  # ever writing the run's terminal event, so the next session cannot start and
  # the contract's one `end` is nowhere.
  mutate web "the run is left open when the dialog closes" "$X" \
    '    if (!next) describe.discard()' \
    '    void next'

  # **The desk owns four members, and `specVersion` is not one of them.** It was
  # for one round: the version a model wrote was replaced from the schema, so
  # the bytes the runtime checked were not the bytes the assistant proposed on
  # the one member that says what the document is, and `specVersion: "99"` was
  # repaired into something creatable instead of being refused.
  mutate web "the desk rewrites the format version the proposal wrote" "$NP" \
    '  return serialise(shapePack(document as Record<string, unknown>, fields))
}' \
    "  return serialise({
    ...shapePack(document as Record<string, unknown>, fields),
    specVersion: '0.2.0-draft'
  })
}"

  # **One reading of "is this run clean", for both consumers.** The tab used to
  # find the proposal on the event list itself, so a run that failed after
  # proposing was offered for accepting into a draft with no reason shown —
  # while the dialog, reading the same run, had already withdrawn it.
  mutate web "the Assistant tab ignores what the run failed with" "$AP" \
    '    failure: outcome.failure,' \
    "    failure: '',"

  # And the selector itself: a failure reported after the terminal event
  # withdraws the proposal for everybody that reads it.
  mutate web "a post-end failure does not withdraw the proposal" "$RO" \
    '  const spoiled =
    run.failure ?? (proposedAt === -1 ? said(events) : said(events.slice(proposedAt + 1)))' \
    '  const spoiled = proposedAt === -1 ? said(events) : said(events.slice(proposedAt + 1))'

  # **A failure after the terminal event is still a failure.** One `end` is the
  # contract, so a throw while an engine unwinds cannot go on the stream — and
  # dropping it made `proposal -> end -> throw` read as a clean run to
  # everything downstream, which offered the proposal for writing.
  mutate web "a failure after the run's terminal event is dropped" "$AR" \
    '            setFailure(said)
            if (!run.ended) push(run, { type: '"'"'error'"'"', message: said })' \
    '            if (!run.ended) push(run, { type: '"'"'error'"'"', message: said })'

  # **Every diagnostic, or the author cannot see what is wrong.** The refusal
  # prevents the page that would have shown the rest from existing, so this is
  # the only place the runtime's whole answer can be read.
  mutate web "the refusal shows only the first diagnostic" "$X" \
    '              diagnostics={anchor(refused, new Set())}' \
    '              diagnostics={anchor(refused, new Set()).slice(0, 1)}'

  # ---- Round 1: what the review found, and the rows that hold the answers ---
  #
  # A proposal belongs to a submission; an error after one withdraws it; the
  # runtime says whether a proposal is a pack before either write; losing the
  # slot ends the session; a route change is a dismissal; and an unmount
  # accounts for the run's terminal event.

  # **Withdrawn at the press, not at the start.** `run.events` is cleared when a
  # run *starts*, one effect later, and the id is what says whose events those
  # are: a submission that reuses the previous id is a second Propose reading
  # the first one's proposal — and, since the start effect keys on the same id,
  # never running at all.
  #
  # (A `setRanId(null)` in `propose` was the first spelling of this row. The
  # harness reported it NOT DISCRIMINATING, correctly: the ids are strictly
  # increasing, so the comparison below already held it and the statement was
  # doing nothing. It is gone, and the row names what actually holds it.)
  mutate web "the previous proposal is not withdrawn at the press" "$DI" \
    '    setSubmitted({ id: (nextRun.current += 1), args: { policy: typed } })' \
    '    setSubmitted({ id: nextRun.current, args: { policy: typed } })'

  # And the gate that makes the id mean anything: events belonging to an older
  # submission are not this section's to read.
  mutate web "a proposal from an earlier submission is read anyway" "$DI" \
    '  const events =
    discarded || submitted === null || ranId !== submitted.id ? EMPTY : run.events' \
    '  const events = discarded ? EMPTY : run.events'

  # The contract does not make `proposal` an engine's last non-terminal event.
  # One that proposes and then fails has said the work does not stand.
  # (The rule moved into `runOutcome.ts` when the Assistant tab was made to read
  # the same one; the row names it where it lives.)
  mutate web "an error after a proposal leaves it on offer" "$RO" \
    '? said(events) : said(events.slice(proposedAt + 1)))' \
    '? said(events) : undefined)'

  # **The runtime is what says a document is a pack.** Without this term Create
  # is offered on a document nobody checked, which is how a `specVersion` a
  # model invented and a top-level member nobody declared get written and
  # registered.
  mutate web "the check on a proposed document is not required" "$X" \
    '    proposalRefusal === undefined &&' \
    '    true &&'

  # And the reading of the answer: a report that is not `valid` is a refusal,
  # not a formality.
  mutate web "a document the runtime refused is treated as valid" "$X" \
    '              : checked.data.report.status === '"'"'valid'"'"'
                ? undefined
                : `The runtime will not call this document a pack — ${
                    layersReached(checked.data.report).text
                  }`' \
    '              : undefined'

  # Losing the slot used to hide the controls and leave the session running.
  mutate web "the assistant going away only hides the controls" "$DI" \
    '    discardNow.current()
    setLost(SLOT_LOST)' \
    '    void SLOT_LOST'

  # The rail mounts this dialog above the route, so a Back leaves it standing
  # over another page with its run alive.
  mutate web "a route change is not a dismissal" "$X" \
    '    closeNow.current(false)' \
    '    void closeNow'

  # And the other half: a fragment is not a page. Keyed on `location.key` this
  # closes over a hash the document's own outline writes, which this desk
  # defines as not a navigation.
  mutate web "a fragment counts as a navigation" "$X" \
    '  const page = `${location.pathname}${location.search}`' \
    '  const page = location.key'

  # **The unmount's `finish`, now that it can be observed.**
  #
  # Round 1 asked for this row; it reported NOT DISCRIMINATING and was retired
  # with the reason — after the component is gone, `setEvents` is a no-op and
  # nothing renders the array it would have made. Round 2 asked for the
  # observation instead of the retirement, and the hook now counts its terminal
  # events in an object whose identity is stable for its lifetime, so a test
  # takes the reference while the hook is alive and reads it after it is not.
  mutate web "an unmount releases the run without finishing it" "$AR" \
    '      const run = active.current
      if (run !== null) finish(run)
      release(run)' \
    '      release(active.current)'

  mutate web "Fix re-serializes the runtime's diagnostics" "$CK" \
    '  const span = spanAt(indexDocument(raw), '"'"'/diagnostics'"'"')
  return span === undefined ? undefined : raw.slice(span.valueStart, span.valueEnd)' \
    '  const parsed = JSON.parse(raw) as { diagnostics?: unknown }
  return parsed.diagnostics === undefined ? undefined : JSON.stringify(parsed.diagnostics, null, 2)'

  # ---- The native Gemini wire, on both engines ------------------------------
  #
  # Two closed exceptions and one closed removal list. Each row below opens one
  # of them, or breaks the property that makes it safe to have opened it at all.
  BG=web/src/assistant/engines/builtin/providers/gemini.ts
  GS=web/src/assistant/geminiSchema.ts

  # **The page's mirror is only worth having if it is the chassis' rule.** A
  # method the page admits and the relay does not is a refusal after the request
  # left; the enforcement test reads the Go list and holds the two equal.
  mutate web "the page's mirror admits a method the chassis does not" "$ASN" \
    '      if (index !== segments.length - 1 || name === '"'"''"'"' || !RELAY_PATH_METHODS.includes(method)) {' \
    '      if (index !== segments.length - 1 || name === '"'"''"'"') {'
  # And only in the **final** segment: round 1 of chunk 5a found
  # `v1beta/a:countTokens/b` forwarded with the credential, because the rule was
  # written per segment and never asked where the segment was.
  mutate web "the page's mirror admits a colon method in a middle segment" "$ASN" \
    '      if (index !== segments.length - 1 || name === '"'"''"'"' || !RELAY_PATH_METHODS.includes(method)) {' \
    '      if (name === '"'"''"'"' || !RELAY_PATH_METHODS.includes(method)) {'
  # The one pair is admitted for one wire and refused for the other two, which
  # carry streaming in the request body and need none.
  mutate web "the page's mirror admits the stream pair on every kind" "$ASN" \
    "  return family === 'gemini' ? RELAY_STREAM_PAIR : ''" \
    '  return RELAY_STREAM_PAIR'
  # Byte equality against one fixed literal is the whole of the exception: a
  # comparison with a second reading is the class the refusal exists to keep out.
  mutate web "the page's mirror compares the pair by name rather than by bytes" "$ASN" \
    "    if (rest.length > 1 || rest[0] !== admitted || admitted === '') {" \
    "    if (rest.length > 1 || !(rest[0] ?? '').startsWith('alt=') || admitted === '') {"

  # **The model turn goes back as it came, or the signature does not.** The
  # scripted endpoint refuses a continuation that dropped a signed part, so this
  # fails at the wire rather than at an assertion about the page.
  # **Retargeted twice, and both reasons are worth keeping.** It first broke the
  # carry-over of a signature onto a *joined* part — the very thing round 1
  # found was wrong, so there is no such carry-over to break. It then broke
  # `signaturesOf`, which turned out to feed nothing the wire can see: reported
  # NOT DISCRIMINATING, and correctly. What actually decides whether a signature
  # goes back is the part this accumulator keeps, so that is what this breaks.
  mutate web "a Gemini thought signature is dropped on the way back" "$BG" \
    '  parts.push({ ...arriving })' \
    '  parts.push({ ...arriving, thoughtSignature: undefined })'
  mutate web "the Gemini turn is rebuilt rather than echoed back" "$BG" \
    '    messages.push(
      turn.assistant ?? {' \
    '    messages.push(
      undefined ?? {'
  # **Reasoning is for the person reading the tab.** The runtime is asked about
  # documents, and a tool call carrying the model'"'"'s own thought summary would put
  # it in a project'"'"'s audit trail. The Anthropic row above is the same property
  # on the other signing wire.
  mutate web "the model's thought summary is sent to the runtime with the tool call" "$BG" \
    '      args: args !== null && typeof args === '"'"'object'"'"' ? args : {},' \
    '      args: {
        ...(args !== null && typeof args === '"'"'object'"'"' ? args : {}),
        reasoning: parts.find((held) => held.thought === true)?.text
      } as Record<string, unknown>,'
  # The pair is how this wire asks for a stream, and there is nowhere else to
  # put it. Dropped, the request is a unary call the desk then reads as a
  # stream; asked for in any other spelling, the desk'"'"'s own mirror refuses it
  # and nothing is sent.
  mutate web "the streaming Gemini call drops the pair the wire needs" "$BG" \
    '      ? `${VERSION}/models/${model}:streamGenerateContent?alt=sse`' \
    '      ? `${VERSION}/models/${model}:streamGenerateContent`'
  mutate web "the streaming Gemini call asks for a framing the relay refuses" "$BG" \
    '      ? `${VERSION}/models/${model}:streamGenerateContent?alt=sse`' \
    '      ? `${VERSION}/models/${model}:streamGenerateContent?alt=json`'

  # **The removal list is closed, or it is not a ruling.** Opened, the endpoint
  # refuses the declaration; the leg fails at the wire.
  mutate web "the removal list loses the keyword every runtime schema carries" "$GS" \
    "  'additionalProperties',
" \
    ""
  # …and applied at every depth, because a schema'"'"'s keywords live inside
  # `properties`, `items` and `$defs` as much as at the top.
  mutate web "the removal list is applied at the top level only" "$GS" \
    '    const walked = withoutKeywords(value, removals, inNameMap ? false : namesUnder(key))' \
    '    const walked = value'
  # A keyword the list does not name is **reported**, never stripped: a desk
  # that widened its idea of the runtime'"'"'s contract on being refused would show
  # the model a contract nobody wrote down.
  mutate web "the built-in engine swallows a refused schema keyword" "$BL" \
    '          if (refusal.kind === '"'"'other'"'"') throw schemaRefusal(cause, session) ?? cause' \
    '          if (refusal.kind === '"'"'other'"'"') throw cause'
  mutate web "the SDK-backed engine swallows a refused schema keyword" "$VL" \
    "        if (said.kind === 'other') throw schemaRefusal(cause) ?? cause" \
    "        if (said.kind === 'other') throw cause"

  # **Off is a member on this wire and an omission on the others**, because
  # omission here means thinking. Take the member away and the endpoint that
  # cannot be turned off has nothing to refuse, so the desk never learns it.
  mutate web "off is expressed by omission on the Gemini wire too" "$TH" \
    "    if (dialect === 'gemini-budget') {
      return {
        members: { thinkingConfig: { thinkingBudget: 0 } }," \
    "    if (false) {
      return {
        members: { thinkingConfig: { thinkingBudget: 0 } },"
  # The refused member is never sent again — the same rule the degrade follows,
  # and the reason `always` reached by a refusal withdraws it while `always`
  # reached from absence does not.
  mutate web "the refused tier member is sent again after the endpoint refused it" "$TH" \
    '  const wire = () => (unavailable || offRefused ? null : wireFor(tier, dialect))' \
    '  const wire = () => wireFor(tier, dialect)'
  # A 400 at `off` is equally "cannot be turned off" and "spells it the other
  # way", and only trying the other spelling tells them apart.
  mutate web "a first refusal at off is read as a model that always thinks" "$TH" \
    '      const next = nextDialect(dialect)
      if (next !== null) {' \
    '      const next = nextDialect(dialect)
      if (next !== null && tier !== '"'"'off'"'"') {'

  # The SDK'"'"'s translation, and the two rows that hold it: the members the table
  # asked for must reach the wire, and the signature this SDK surfaces under a
  # different name must still be ledgered.
  mutate web "the Gemini tier is not translated into the SDK's provider option" "$VL" \
    '    return { providerOptions: { [GOOGLE_OPTIONS]: { thinkingConfig: members.thinkingConfig } } }' \
    '    return {}'
  mutate web "the SDK's Gemini signature is ledgered under the wrong provider" "$VR" \
    "  gemini: { provider: 'google', member: 'thoughtSignature' }" \
    "  gemini: { provider: 'anthropic', member: 'signature' }"
  # The tier lives one level down on this wire, so the rebuild after a
  # truncation has to reach it there.
  mutate web "the Gemini rebuild strips the tier from the top level" "$VR" \
    '      const config = (payload.generationConfig ?? {}) as Record<string, unknown>
      for (const member of TIER_MEMBERS) delete config[member]' \
    '      const config = (payload.generationConfig ?? {}) as Record<string, unknown>
      for (const member of TIER_MEMBERS) delete payload[member]'
  # This wire asks to stream in the **address**; a reader that looked in the
  # body would answer "no" to every streamed call and never re-frame a whole
  # answer the SDK asked to have streamed.
  mutate web "the Gemini re-framing decides from the body rather than the address" "$VR" \
    "  if (family === 'gemini') return suffix.includes(':streamGenerateContent')" \
    '  if (false) return false'

  # ---- Round 1's five findings, each broken again ---------------------------
  #
  # A row per ruling, and each one restores the exact shape the review found.
  SM=web/src/assistant/conformance/scriptedModel.ts

  # **A signature certifies the exact bytes it came with.** Merging a signed
  # part with an unsigned one produces a signature over text the endpoint never
  # signed; merging two signed parts throws one away.
  mutate web "a signed Gemini part is merged with the one beside it" "$BG" \
    '    !signed(last) &&
    !signed(arriving) &&' \
    ''
  # **Retired, with its reason: NOT DISCRIMINATING, and unavoidably so.** It was
  # "the joined part takes the later signature", the other half of Google's rule
  # — and with the fix in place a signed part is never joined at all, so the
  # branch the mutation adds is unreachable code that no test can provoke. The
  # row above breaks the guard itself, and both halves of the rule fall with it:
  # its failures name the signed-plus-unsigned case and the two-signed case by
  # name. One row, one guard.

  # **A signed thought part with no text is the endpoint reasoning.** The wire
  # emits one when a summary is empty, and counting only readable passages let a
  # model think through every turn at tier off without the desk noticing.
  mutate web "an empty signed thought part is not reasoning seen" "$BG" \
    '    (part) => part.thought === true && (signed(part) || (part.text ?? '"'"''"'"') !== '"'"''"'"')' \
    "    (part) => part.thought === true && (part.text ?? '') !== ''"

  # **A name is not a keyword.** A definition called `const` was deleted as
  # though it were the keyword, leaving a `$ref` pointing at nothing.
  mutate web "the walker knows only two of the schema name maps" "$GS" \
    "  'properties',
  '\$defs',
  'definitions',
  'dependentSchemas',
  'dependentRequired',
  'patternProperties'
]" \
    "  'properties',
  '\$defs'
]"
  # And the second layer of the same mistake, found by writing the test: a key
  # *inside* a name map is a name, so its value is an ordinary schema.
  mutate web "a name inside a name map is read as a name-map keyword" "$GS" \
    '    const walked = withoutKeywords(value, removals, inNameMap ? false : namesUnder(key))' \
    '    const walked = withoutKeywords(value, removals, namesUnder(key))'

  # **Gemini signs the first functionCall part**, and the fixture's validator
  # called every placement outside a thought part malformed — which would have
  # refused the shape the wire actually sends.
  mutate web "a signature on a function call is called malformed" "$SM" \
    '      const onCall = part.functionCall !== undefined' \
    '      const onCall = false'
  # The desk's own half of it: a scanner that reads only thought parts never
  # compares the signature function calling actually carries.
  mutate web "the Gemini scanner reads a signature only off a thought part" "$VR" \
    '      typeof block?.thoughtSignature === '"'"'string'"'"' &&
      (block.thought === true || block.functionCall !== undefined)' \
    "      typeof block?.thoughtSignature === 'string' && block.thought === true"
  # **Retired, with its reason: NOT DISCRIMINATING, and the reason is worth
  # writing down.** It was "the SDK-backed engine ledgers no signature from a
  # function call". The ledger's only observable consequence is the truncation
  # comparison, and the pinned SDK carries a call signature back whole — so with
  # the recording removed the wire is byte-identical and no leg can tell. The
  # recording is defence for the day that provider truncates a call signature
  # the way `vercel/ai#19663` truncates a summary one, and there is no way to
  # provoke it while the SDK is correct. The half that *can* be observed is the
  # scanner, and the row below breaks that.

  # **The declaration is derived, or it is a sentence that rots.** Widening it by
  # hand is the failure this leg exists for.
  mutate web "the declared SDK removal set is widened by hand" "$GS" \
    "  'uniqueItems',
  'writeOnly'
]" \
    "  'uniqueItems',
  'writeOnly',
  'format'
]"
  # …and narrowing it is the same failure the other way.
  mutate web "the declared SDK removal set loses a keyword" "$GS" \
    "  'pattern',
  'prefixItems'," \
    "  'prefixItems',"
  # The empty-object omission is not a keyword and is declared separately: a
  # tool with no properties is declared to the model with no parameters at all.
  mutate web "the empty-schema omission is not declared" "$GS" \
    "  if (engine === 'vercel' && isEmptyObjectSchema(served)) return undefined" \
    '  if (false) return undefined'
  # **Deep equality, or the claim is about a handful of words.** Retargeted from
  # the assertion onto the code it holds: a row that weakens a *test* cannot
  # discriminate, because a weakened test is exactly a test that does not fail.
  # What the leg has to catch is the desk saying one thing and the engine doing
  # another, and this is that — the declared narrowing not applied at all.
  mutate web "the declared narrowing is not applied to what the model is shown" "$GS" \
    '  if (engine === '"'"'vercel'"'"' && isEmptyObjectSchema(served)) return undefined
  return withoutKeywords(served, removals)' \
    '  if (engine === '"'"'vercel'"'"' && isEmptyObjectSchema(served)) return undefined
  return served'
  # **Never silent.** An author reading a proposal should not have to discover
  # that the model saw a wider contract than the runtime enforces.
  mutate web "the narrowing is not reported to the author" "$EC" \
    '    events.push({ type: '"'"'guardrail'"'"', tool: tool.name, action: '"'"'narrowed'"'"', detail: narrowingNotice(lost) })' \
    '    void narrowingNotice(lost)'

  # ---- Round 2's four findings, each broken again ---------------------------

  # **A notice is a loss beyond the ruling, never the ruling itself.** Comparing
  # the runtime's raw schema against a set that includes the desk's own six made
  # every tool produce a notice about `additionalProperties` — the desk warning
  # about the thing it wrote down, five times a run.
  mutate web "the narrowing notice is measured against the raw schema" "$GS" \
    '  const desk = withoutUnsupportedKeywords(served)
  const shown = schemaShown(engine, family, desk)' \
    '  const desk = served
  const shown = schemaShown(engine, family, desk)'

  # **The fixture the declaration is derived over is the runtime's vocabulary,
  # or it is a schema somebody made up.** Aimed at the fixture and not at the
  # assertion: a row that weakens a check cannot discriminate, because a
  # weakened check is exactly one that does not fail. A fixture that has quietly
  # stopped carrying a keyword the runtime emits is the failure this lock is
  # for, and it is what this restores.
  mutate web "the derivation fixture drops a keyword the runtime emits" "$CT" \
    "      d: { type: 'object', propertyNames: { type: 'string' } }," \
    "      d: { type: 'object' },"
  # …and the other half: a vocabulary computed from the tool schemas alone
  # leaves out the pack schema, which is where most of the keywords are.
  mutate web "the runtime vocabulary ignores the recorded pack schema" "$CT" \
    '  keywordsSent(JSON.parse(answered.content[0]!.text), found)' \
    '  void answered'

  # **A rule about one wire's schema dialect belongs to that wire.** Installed
  # on every family, a 400 saying "unsupported response type" names `type` and
  # the real failure was rewritten into a sentence about a removal list.
  mutate web "the built-in engine classifies a schema refusal on every family" "$BL" \
    "  if (session.model.family !== 'gemini') return null" \
    '  if (false) return null'
  mutate web "the SDK-backed engine classifies a schema refusal on every family" "$VL" \
    "    if (session.model.family !== 'gemini') return null" \
    '    if (false) return null'

  # **An empty signed thought is a shape the wire sends**, so a validator that
  # calls it malformed refuses a client that replayed it faithfully.
  mutate web "an empty signed thought is called malformed" "$SM" \
    '      const onThought = part.thought === true' \
    "      const onThought = part.thought === true && typeof part.text === 'string' && part.text !== ''"
  # And the desk's own half: the parts that arrived are the parts that go back,
  # empty ones included.
  mutate web "an empty Gemini part is dropped from the turn that goes back" "$BG" \
    '  parts.push({ ...arriving })' \
    "  if ((arriving.text ?? '') === '' && arriving.functionCall === undefined) return
  parts.push({ ...arriving })"

  # ---- Chunk 6a: the card, and what it may not invent --------------------
  SCD=web/src/admin/SourceCard.tsx
  ADV=web/src/routes/AdminView.tsx
  NR=web/src/admin/narration.ts

  # **A location comes from the chassis or it is a guess.**
  #
  # **This row replaced one that did not discriminate.** The first version
  # composed `${chassis.projectDir}/${effective.path}` — which is the same
  # string the chassis reports in every ordinary case, because the chassis
  # composes it the same way out of the root it resolved. A mutation whose
  # output is byte-identical to the correct one cannot be caught by anything,
  # and a fixture built to make it differ would be a state no chassis produces.
  #
  # What is actually load-bearing is that the page reads the chassis' answer
  # at all rather than falling back to its own project-relative constant, and
  # that is what this breaks: the name a file is read by is not a location on
  # a filesystem, and printing it as one is how Admin would name a path on a
  # machine whose layout it never learned.
  #
  # **Retargeted.** The fallback itself is gone — where the chassis has not
  # answered the row says so — so the mutation is now the constant *restored*,
  # which is the same claim over the stronger code.
  mutate web "the constant fallback restored (a location the page composed)" "$ADV" \
    '  const chassis = effective.desk?.chassis
  if (chassis === undefined) return <span className="quiet">the desk has not said</span>
  return <code>{chassis.projectFile}</code>' \
    '  return <code>{effective.path}</code>'

  # **The narration guard, broken by putting narration back.** A sweep that
  # only ever passed over a clean page would prove nothing about the sweep.
  mutate web "a paragraph is reintroduced above the cards" "$ADV" \
    '      <header className="detail-head">
        <h1>Admin</h1>
      </header>' \
    '      <header className="detail-head">
        <h1>Admin</h1>
        <p className="quiet">
          This page shows the desk configuration for this machine and for this project, section
          by section, with the file each value came from named beside it.
        </p>
      </header>'

  # **Retired, with its reason: the code it broke left the card.** It was "the
  # content disclosure re-serialises instead of quoting the file", against
  # `Content` in `SourceCard.tsx`. The disclosure is gone from the main column
  # — the bytes are in the right pane — and the claim it held is the same one
  # word for word, so it is one row further down against `ConfigPane`: "the
  # pane shows a re-serialisation instead of the member's bytes". Retired here
  # rather than deleted, because a row that vanishes reads as a claim somebody
  # decided to stop making.

  # **A refused file's bytes are the thing the refusal is about.** Rendering
  # them puts the credential-shaped member into the DOM of the surface that
  # reported the refusal. The rule has **one** spelling — `showsContent` is
  # exported from the card and imported by the pane — so this one mutation is
  # felt everywhere the bytes can be shown.
  mutate web "a refused file's bytes are rendered anyway" "$SCD" \
    "  return status.state !== 'refused' && status.state !== 'unread'" \
    '  return true'
  # **Retired, with its reason: NOT DISCRIMINATING, and the code went with
  # it.** It was "the disclosure quotes a file the decoder did not accept",
  # breaking a second gate inside `Content` that repeated what `showsContent`
  # already decides — the card renders no disclosure at all in exactly the
  # states that check would have caught, so removing it changed nothing any
  # test could see. Two spellings of one rule are invisible to a harness that
  # breaks one of them, so the rule now has one spelling and one row.

  # **A paragraph split into short spans is still a paragraph.** The rule this
  # replaces measured single text nodes, and JSX produces two of them whenever
  # a sentence carries an inline element.
  mutate web "the narration sweep measures only single text nodes" "$NR" \
    '  for (const block of container.querySelectorAll(BLOCKS)) {' \
    '  for (const block of [] as Element[]) {'

  # ---- Chunk 6b: the Save on the project-file cards ----------------------
  PFS=web/src/admin/useProjectFileSave.ts
  PFF=web/src/admin/ProjectFileForm.tsx

  # **A form over a parsed object re-serialises to save.** The file then arrives
  # as a diff of every line — indentation, member order, the author's own
  # alignment — and `1e2` quietly becomes `100`. The splice is what stops it,
  # and this is the whole-file rewrite it replaced.
  mutate web "a card's Save rewrites the whole file instead of one member" "$PFS" \
    '  return { text: next.text, problems: [] }' \
    '  return { text: JSON.stringify(JSON.parse(next.text), null, 2), problems: [] }'

  # **There is no row for `noValidate`, and the reason is the row's own rule.**
  # A number field carries the decoder's bounds as `min` and `max`, and without
  # `noValidate` the browser refuses the submit before the form sees it — so the
  # decoder's sentence is never shown and nothing is written. jsdom performs no
  # constraint validation, so a mutation removing it leaves the whole suite
  # green: a row for it would be a claim of coverage nothing holds. It is held
  # by the live drive instead, which is where it was found.

  # **The chassis watches the project and invalidates every query when this
  # file changes.** A revision read off that query moves onto bytes nobody saw,
  # and the Save that follows overwrites somebody's edit with no refusal at all
  # — the exact failure the conditional commit exists to prevent, and the rule
  # `useFileEditing` already carries for the pack editor.
  mutate web "the revision follows the watcher while a value is unsaved" "$PFS" \
    '  if (live !== undefined && live.sha256 !== base?.sha256 && (base === undefined || !unsaved)) {' \
    '  if (live !== undefined && live.sha256 !== base?.sha256) {'

  # **A write states the bytes it replaces.** The empty string is not a missing
  # digest — it is a claim that there is no file — so this is the page asserting
  # the state of a file it never saw, and a change made between the read and the
  # save is lost rather than refused.
  mutate web "a card's Save is sent without the identity it read against" "$PFS" \
    '      { path: PROJECT_CONFIG_PATH, content: composed.text, baseSha256: base.sha256 },' \
    "      { path: PROJECT_CONFIG_PATH, content: composed.text, baseSha256: '' },"

  # **The file API offers an override, and a configuration card offers none.**
  # A client that always sent it would have no concurrency story, only an
  # unstated one: the stale answer this whole path is built around never
  # arrives, because nothing is ever refused.
  mutate web "a card's Save overwrites whatever it finds" "$PFS" \
    '      writeFile(input),' \
    '      writeFile({ ...input, override: true }),'

  # **Nothing was written, so there is nothing to take back.** A form whose
  # fresh seed won over the fields would answer a refusal by discarding the work
  # the refusal protected.
  mutate web "Reload after a refused write takes the file over the unsaved values" "$PFF" \
    '  const draft = { ...seed, ...held } as D' \
    '  const draft = { ...held, ...seed } as D'

  # **A reload that failed changed nothing, so nothing it was about may go.**
  # Clearing the refusal on the button press leaves a card whose read then
  # failed with only that read's error — no digests, no Reload — while the
  # revision behind it has not moved, so the next Save is refused again for a
  # reason nothing on screen still says.
  mutate web "the refusal is cleared when Reload is pressed rather than when it lands" "$PFS" \
    '    const ticket = (reloads.current += 1)
    setProblems([])' \
    '    const ticket = (reloads.current += 1)
    write.reset()
    setProblems([])'

  # **A configuration that is accepted and cannot work is worse than one
  # refused where it was written.** The chassis refuses a NUL in any path
  # outright, so a `dir` carrying one is advertised by Admin as the pack
  # location and makes every later create fail with a sentence about a path
  # nobody chose to look at — the same defect `dist` had.
  mutate web "a control character is accepted in a pack location" "$D" \
    '  if (CONTROL_CHARACTER.test(value)) {
    return bad(`${NO_CONTROL_CHARACTERS}; found ${describe(value)}`)
  }' \
    ''

  # **A rule that says every control character has to be asked before anything
  # is removed.** `String.trim` takes U+0009 through U+000D off, so a check
  # behind one accepts a leading tab and a trailing newline by trimming them —
  # which is the half of the rule round 2 found missing.
  mutate web "the control-character rule is asked after the trim has hidden the edges" "$D" \
    '  if (CONTROL_CHARACTER.test(value)) {
    return bad(`${NO_CONTROL_CHARACTERS}; found ${describe(value)}`)
  }
  const trimmed = value.trim().replace(/\/+$/, '"'"''"'"')' \
    '  const trimmed = value.trim().replace(/\/+$/, '"'"''"'"')
  if (CONTROL_CHARACTER.test(trimmed)) {
    return bad(`${NO_CONTROL_CHARACTERS}; found ${describe(value)}`)
  }'

  # **A save that lands is over.** The decoder normalises what it accepts — an
  # `idBase` gains the separator it was missing, a `dir` loses the one it ended
  # with — so a form still holding the raw input stays dirty for ever over a save
  # that succeeded, offering to write again what the file already says.
  mutate web "a form goes on holding what it typed after the save landed" "$PFF" \
    '    submit: () => save.save(edits, () => setTouched({})),' \
    '    submit: () => save.save(edits),'

  # **A field the file has caught up with is not one anybody is holding.** Hold
  # `B`, take a 409, Reload finds `B` and the form goes clean — and then another
  # writer makes it `C`. Without the pruning the retained entry resurfaces as
  # dirty against the newer seed and offers to write `B` over `C`, with nobody
  # having typed anything since `B` became the accepted value.
  mutate web "a held field survives the seed catching up with it" "$PFF" \
    '  const held = identity === seen ? touched : agreeing(touched, seed)' \
    '  const held = touched'

  # **A whole draft is not a record of what anybody edited**, and round 1 of the
  # review found the lost edit: edit Name while another writer adds a mark, take
  # the 409, Reload, Save — and a form that treats every field as touched writes
  # `mark: null` over a change nobody here ever saw, under a digest that is now
  # perfectly true.
  mutate web "every field is recorded as touched, not only the one that changed" "$PFF" \
    '    if (!Object.is(value, (seed as Record<string, unknown>)[name])) held[name] = value' \
    '    held[name] = value'

  # **The file API forms no opinion about what a file means**, so it would write
  # an appearance this desk then refuses to read. The opinion is this page's,
  # and it is asked before the request rather than after it.
  mutate web "the decoder is not asked before a card's Save is sent" "$PFS" \
    '  if (decoded.problems.length > 0) return { problems: decoded.problems }' \
    '  if (decoded.problems.length > 9999) return { problems: decoded.problems }'

  # **The answer moves the cache and the re-read only confirms it.** Left to the
  # invalidation alone, the header goes on showing the name that was just
  # replaced for as long as the second read takes — and for ever where it never
  # answers.
  mutate web "a landed write waits for a second read before the shell reflects it" "$PFS" \
    '          client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, (previous) =>
            configAfterProjectFileWrite(previous, landed)
          )' \
    '          void configAfterProjectFileWrite'

  # ---- Chunk 6c: two groups, a status line, and the settings that left ----
  AS=web/src/routes/adminSections.ts

  # **The location is stated once per file, not once per card.** Three cards
  # writing three members of one file printed that file's path three times and
  # its read status three times, which reads as three files. `under` is the
  # group's own status: its presence says the header has already said where the
  # file is, and its value is what a card's status is compared against.
  mutate web "a location repeated on a card under a group header" "$SC" \
    '        location={grouped ? undefined : location}' \
    '        location={location}'

  # **The Runtime card was status rather than settings**, and a settings page
  # carries settings: its four slots held the binary the chassis was launched
  # with, the connection, and what the tool listing said, and not one of them was
  # editable. The break is the declaration, which is what the page renders from
  # and what the rail's section menu links to — a section declared and not
  # rendered is a menu entry pointing at a heading that is not there.
  mutate web "the Runtime card back" "$AS" \
    "    sections: [
      { id: 'organization', title: 'Organization' }," \
    "    sections: [
      { id: 'runtime', title: 'Runtime' },
      { id: 'organization', title: 'Organization' },"

  # **A removed control's write path is removed with it.** The Panes card is
  # gone — the dimensions are the shell's, and the reset of this browser's own
  # record of the layout moved to the shell's menu — and a Save with no control
  # behind it is a write path nothing offers. The composer keeps `/panes`,
  # because it is a general splicer with its own tests; the settings page has no
  # member to hand it.
  #
  # **The mutation routes a real Save, not the expectation.** The first version
  # added `/panes` to the list the test compares against, which fails a
  # comparison against itself and says nothing about the write path — the review
  # was right about that. This points an existing Admin Save at `/panes`, which
  # is a production write path, and the test that drives every Save and reads
  # the wire catches it: the file that Save would compose carries an
  # `organization` shape under `panes`, the decoder refuses it, and nothing is
  # written where a member was promised. The expectation is never touched.
  mutate web "the Panes Save back (a write to /panes from Admin)" "$PFC" \
    "  const state = useProjectFileDraft('/organization', seed, (draft, from) => {" \
    "  const state = useProjectFileDraft('/panes', seed, (draft, from) => {"

  # **The reset lives where the panes are.** It was a button on Admin › Panes —
  # a settings page reaching into a browser's own storage — and it is now an
  # action in the shell's own user menu, which already carried the two per-viewer
  # settings links. Removing the element is the break; what the provider does
  # when it is pressed is held by `shellState.test.tsx` and its own rows.
  mutate web "the reset action missing from the shell" "$U" \
    '          <ResetPanesItem />' \
    '          {null}'

  # **Eight hex digits collide.** The key was a 64-character slug plus an
  # FNV-1a hash, and the review found two roots differing only past the 81st
  # character that produced one key: one project's reset then removed the
  # other's record. Percent-encoding the whole path is injective, so distinct
  # roots are distinct keys by construction; truncating it is exactly the class
  # of key this replaced.
  mutate web "the short hash restored (a truncated project key)" "$P" \
    '    return encodeURIComponent(path)' \
    '    return encodeURIComponent(path).slice(0, 24)'

  # **Only a record this shell wrote.** The key is derived from a path the
  # viewer never chose, on an origin this desk shares with whatever else has
  # been served from it, so removing whatever is there would be a reset deleting
  # somebody else's value under a name it merely computed.
  mutate web "the reset removes whatever is under the key" "$P" \
    "  if (raw !== null && readShellState(key) === undefined) return 'foreign'" \
    '  void raw'

  # **A card's own write is part of what its Status says.** The group's status
  # is the *file's* read state, and a card under it drops a status that says the
  # same thing — so a card writing, refused, or holding a stale write showed no
  # Status at all while the group said `read`.
  mutate web "write state dropped from the card's status comparison" "$SCD" \
    '  const says = write ?? status' \
    '  const says = status'

  # ---- Chunk 6d: appearance is a preference, and leaves Admin ----------
  #
  # **Retired: none, and that is worth stating rather than leaving to be
  # noticed.** The Appearance card had no row of its own — the two Selects on
  # it were held by the primitive's rows (`the select reports back a value
  # nobody offered`) and by the write path's (`the decoder is not asked before
  # a card's Save is sent`), both of which are still driven by the two cards
  # that remain. The one appearance row there was is `the configured theme is
  # decoded and never applied`, and it **moved** with the code rather than
  # retiring: it is the same claim about the same attribute, now broken where
  # the ladder is resolved.

  # **The preference is what this viewer chose, and it beats the file.** Theme
  # and density are a person's and not an organization's: `appearance` in
  # `jpack-desk.json` is a file in the project's repository, so a desk that read
  # the file over the preference is the defect this whole chunk is about — one
  # person's dark, for everybody who ever cloned it.
  mutate web "the preference does not override the project default" "$APS" \
    '    theme: preference?.theme ?? projectDefault?.theme,
    density: preference?.density ?? projectDefault?.density' \
    '    theme: projectDefault?.theme,
    density: projectDefault?.density'

  # **And the file is still the default.** The other half of the same ladder:
  # a viewer who has chosen nothing gets what the project asked for, not what
  # the schema falls back to — otherwise `appearance` is a member that is
  # decoded, validated, shown on no page and honoured by nothing.
  mutate web "the project default is not applied when no preference exists" "$APS" \
    '    theme: preference?.theme ?? projectDefault?.theme,
    density: preference?.density ?? projectDefault?.density' \
    "    theme: preference?.theme ?? 'system',
    density: preference?.density ?? 'comfortable'"

  # **Retargeted, not retired: `an invalid stored appearance is applied`.** The
  # claim survived the code it was made against. It used to break the two guards
  # that filtered an out-of-union value out of a record this desk owned; round 2
  # made such a value a reason to disown the record entirely, so the guards and
  # the ownership test are one pass and the claim is broken where that pass now
  # lives — below, as `the union check removed from ownership`. Validating and
  # extracting separately is what let the lenient half take whatever the strict
  # half had accepted, twice.

  # **A reset that reports a removal it did not make is worse than none.** The
  # record is still there to come back on the next load, and the menu says the
  # project's default is in force again — so `removeItem` is called, the key is
  # read back, and only then is anything said.
  mutate web "the appearance reset does not remove the key" "$APS" \
    '    window.localStorage.removeItem(key)' \
    '    void key'

  # **Nothing is written under the provisional key.** Until the chassis names
  # the project, the key is the literal `default`: a preference stored there is
  # one project's, under a name that belongs to whichever project answers slowly
  # next. The choice itself is still honoured on screen and is written when the
  # key resolves — what the gate stops is the storing, not the choosing.
  mutate web "an appearance is written under the provisional key" "$APS" \
    '    if (!keyResolved) return
    if (chosen.theme === undefined && chosen.density === undefined) return' \
    '    if (chosen.theme === undefined && chosen.density === undefined) return'

  # ---- Codex round 1 -------------------------------------------------------

  # **A choice belongs to the project it was made in.** It was visit-wide: one
  # tab whose chassis reconnects reports a different root, the member chosen
  # under root A survived, and it was then written into root B's record — one
  # project's preference in another project's key, permanently, over a record B
  # may never have had. Only a choice made under the *provisional* key carries
  # forward, because that key names no project.
  mutate web "a choice survives the chassis naming a different project" "$APS" \
    '  return choice.resolved && choice.key !== key' \
    '  return false'

  # ---- Codex round 2 -------------------------------------------------------

  # **A choice left behind is discarded, not hidden.** Round 1 filtered it at
  # the point of use, and round 2 found what that left standing: the value
  # stayed in state, so A → B → A brought it back ahead of A's own record — and
  # where another tab had changed A's preference meanwhile, the write effect put
  # the resurrected value over it. This restores exactly that filter.
  mutate web "the discard replaced by the filter (a choice hidden, not cleared)" "$APS" \
    '  if (leftBehind(choice, storageKey)) {
    setChoice({ key: storageKey, resolved: keyResolved, value: NOTHING_CHOSEN })
  }
  const chosen = choice.value' \
    '  const chosen = leftBehind(choice, storageKey) ? NOTHING_CHOSEN : choice.value'

  # **Retired, unrun-and-then-run: `a choice is re-stamped onto whatever key is
  # current`.** It removed the condition on the re-stamp, and the suite stayed
  # green — correctly, because that condition is not a safeguard. The write
  # effect has already returned unless the key is resolved and something was
  # chosen under a stamp that still applies, so an unconditional re-stamp can
  # only ever write back the stamp that is already there. What the condition
  # saves is a render, not a rule, and the code says so where it is. A row that
  # cannot discriminate is named here rather than dropped, because its absence
  # would otherwise read as an oversight.

  # **The whole path means the bytes the chassis reported.** The key trimmed
  # before it encoded, and a POSIX filesystem permits a trailing space: two
  # directories, one key, one project's record restored and reset for the other
  # — which is the collision percent-encoding replaced, reintroduced one line
  # above it. This is the key both records share, so the row is in `paneState`.
  mutate web "the trim restored (two roots that differ only in whitespace)" "$P" \
    '  const path = projectRoot ?? '"'"''"'"'
  if (!identityIsResolved(path)) return '"'"'default'"'"'' \
    '  const path = (projectRoot ?? '"'"''"'"').trim()
  if (path === '"'"''"'"') return '"'"'default'"'"''

  # **Ownership is the whole member set, not the version number.**
  # `localStorage` is one namespace shared with everything this origin has ever
  # served, and the key is derived from a path the viewer never chose — so
  # reading `v === 1` alone applied `{"v":1,"writer":"another-app",…}` to the
  # page as this desk's preference and let the reset delete it.
  mutate web "unknown members ignored again (ownership read off the version)" "$APS" \
    '      preference.density = value
    } else {
      return undefined
    }' \
    '      preference.density = value
    }'

  # **And ownership is the members' values, not only their names.** Round 2
  # found what the name check still admitted: `{"v":1,"theme":17}` is not
  # something this writer can emit, and it was owned all the same — read as a
  # record with nothing usable in it, and deleted by "Use the project's
  # default", which is a control removing somebody else's bytes under a key this
  # desk merely computed.
  mutate web "the union check removed from ownership (an impossible value owned)" "$APS" \
    '      if (!isTheme(value)) return undefined
      preference.theme = value
    } else if (member === '"'"'density'"'"') {
      if (!isDensity(value)) return undefined
      preference.density = value' \
    '      preference.theme = value as ThemeChoice
    } else if (member === '"'"'density'"'"') {
      preference.density = value as Density'

  # **A record exists because somebody chose something.** A bare `{"v":1}` is
  # not one this writer produces, so treating it as owned is the reset deleting
  # a value it cannot account for — and it is the shape a *different* writer's
  # empty record most plausibly takes.
  mutate web "a record with nothing chosen in it is owned again" "$APS" \
    '  if (preference.theme === undefined && preference.density === undefined) return undefined' \
    '  void preference'

  # **Nothing this desk has not established is applied.** The record is
  # unreadable until the chassis names the project and the default is the
  # schema's until the file has been read, so a provider that fell back to
  # `projectDefault` before both had answered applied `system`, then the file's
  # value, then the stored preference — three applications for one load, two of
  # them values nobody chose, and a visible flash the day a dark palette exists.
  mutate web "the provisional application restored (a default nobody read)" "$APS" \
    '  const knownDefault = keyResolved && projectDefaultKnown ? projectDefault : undefined' \
    '  const knownDefault = projectDefault'

  # **A removed control's write path is removed with it**, exactly as the Panes
  # card's was. The Appearance card is gone from Admin — a person's theme is not
  # an administrator's setting, and it is held in that person's browser now —
  # and a Save with no control behind it is a write path nothing offers. The
  # composer keeps `/appearance`, because it is a general splicer with its own
  # tests; the settings page has no member to hand it.
  #
  # The mutation routes a **real** Save, not the expectation: pointing an
  # existing Admin Save at `/appearance` composes a file carrying an
  # `organization` shape under `appearance`, which the decoder refuses, so
  # nothing is written where a member was promised. The test that drives every
  # Save and reads the wire catches it, and the expected list is never touched.
  mutate web "the Appearance Save back (a write to /appearance from Admin)" "$PFC" \
    "  const state = useProjectFileDraft('/organization', seed, (draft, from) => {" \
    "  const state = useProjectFileDraft('/appearance', seed, (draft, from) => {"

  # **The verdict is the status, and the name is the metadata.** `server` is
  # retained across a reconnect — the provider spreads the previous state — so
  # `server !== null` means "this page has met a runtime", which is not "this
  # page is connected to one". Every surface that read a verdict off it said
  # `connected` while the banner said the connection was lost.
  mutate web "the connection verdict read from the runtime it last met" "$V" \
    "  if (status !== 'ready' || server === null) return says" \
    '  if (server === null) return says'
  # The one producer, broken where it is produced: three surfaces read it.
  mutate web "one connection word, whatever the socket is doing" "$M" \
    "  if (status === 'ready') return 'connected'" \
    "  return 'connected'"

  # ---- Chunk 6e: a dark palette, and a consumer for density ----------------
  #
  # **Retired: none.** Nothing here replaces a row. `the configured theme is
  # decoded and never applied` still holds the theme's own attribute, one
  # chunk further down the same ladder, and is untouched.

  CSSP=web/src/styles.css
  CSSH=web/src/shell.css
  THM=web/src/config/theme.ts

  # **Every colour token has a dark value, in *both* blocks.** They cannot be
  # written once — one is inside `@media (prefers-color-scheme: dark)` and the
  # other is not, and CSS has no way to share a declaration block across that
  # boundary — so a token dropped from one of them is a colour that follows the
  # OS and not the toggle, or the toggle and not the OS. The needle is the
  # media block's copy, which its four-space indent makes unique.
  mutate web "a colour token dropped from one dark block" "$CSSP" \
    '    --ink-soft: #c2c2b8;
    --ink-faint: #a0a096;' \
    '    --ink-faint: #a0a096;'

  # **Contrast is measured, and the measurement is load-bearing.** A dark
  # palette that was chosen by eye is a palette nobody checked: this pushes the
  # muted ink onto the surface it is read against at 1.4:1, which is a value
  # that looks plausible in a diff and is unreadable on a screen.
  #
  # It trips two rules and not one, and that is stated rather than tidied away:
  # a value edited in the attribute block alone also breaks `carries the same
  # value in both blocks`. The two cannot be separated — one `apply` edits one
  # block — and the row's own claim is the contrast one, which is in the list.
  mutate web "a dark text/background pair pushed under AA" "$CSSP" \
    '  --ink: #f0f0ea;
  --ink-soft: #c2c2b8;' \
    '  --ink: #f0f0ea;
  --ink-soft: #3d3d38;'

  # **No sheet but `styles.css` spells a colour.** `shell.css` was spelling
  # three, and the modules' rule never reached it: a literal here is a colour
  # the theme attribute cannot reach, which is invisible until there is a
  # second palette to reach it with. This puts the scrim back as a literal.
  mutate web "a colour literal back in shell.css" "$CSSH" \
    '    background: var(--overlay);' \
    '    background: rgb(0 0 0 / 25%);'

  # **And the same rule inside the token file, outside its token blocks.** A
  # literal in `:root` is the palette; a literal in a *rule* is a fourth
  # palette that no selector reaches — which is exactly what `#fbfbf9` was, in
  # two rules, for as long as this sheet has existed.
  mutate web "a colour literal back in a rule of styles.css" "$CSSP" \
    '  background: var(--code-surface);
  overflow: hidden;' \
    '  background: #fbfbf9;
  overflow: hidden;'

  # **The density is applied, and not merely decided.** It was decoded,
  # validated, stored per browser and offered in the menu, and read by nothing:
  # the whole of this half of the chunk is the attribute this writes. Without
  # it `compact` resolves through the ladder, shows as chosen in the menu, and
  # changes no pixel — which is indistinguishable from the state it replaces.
  mutate web "the density attribute is never written" "$THM" \
    "  if (density === 'comfortable') {
    root.removeAttribute(DENSITY_ATTRIBUTE)
    return
  }
  root.setAttribute(DENSITY_ATTRIBUTE, density)" \
    "  root.removeAttribute(DENSITY_ATTRIBUTE)
  void density"

  # **And compact is strictly smaller.** A compact value equal to its
  # comfortable one is the same defect one token at a time: the attribute is
  # written, the selector matches, the sheet resolves — and nothing moves. This
  # one also unpins the windowed list's row height from the sheet, which is the
  # second thing that number is held to.
  mutate web "a compact spacing token equal to its comfortable value" "$CSSP" \
    '  --density-row: 32px;' \
    '  --density-row: 40px;'

  # ---- Chunk 6f: Admin, flat ----------------------------------------------
  #
  # **Retired: none.** Nothing here replaces a row. The rows above hold what
  # Admin *says*; these hold what it looks like, which was held by nothing —
  # every one of them was true of the page as merged and invisible to the
  # suite.

  SC=web/src/admin/SourceCard.module.css
  AS=web/src/assistant/AssistantSection.tsx
  DP=web/src/admin/DefaultProject.tsx

  # **A box back on a member.** The shape of the page before this chunk: a
  # group frame around card frames, hierarchy drawn rather than typeset. It is
  # invisible to a render — vitest runs with `css: false` — so it is held by
  # reading the sheet, and this is the row that says the reading is real.
  mutate web "a box back on a member (border and background)" "$SC" \
    '.member {
  padding-block: var(--density-block);
  border-top: 1px solid var(--border);
}' \
    '.member {
  padding-block: var(--density-block);
  border: 1px solid var(--border);
  background: var(--bg);
}'

  # **The fieldset reset removed.** The third frame, and the one no sheet drew:
  # the browser's own groove around the element every form on this desk uses to
  # group what a `disabled` applies to. Removing the `border` line alone is the
  # exact regression — the grouping stays, the frame comes back.
  mutate web "the fieldset reset removed, so the browser frames every form" "$CSSP" \
    'fieldset {
  border: 0;' \
    'fieldset {'

  # **A bare `<button>` back on Admin.** It renders as the browser's own
  # control beside three that do not, and no stylesheet is missing — there is
  # no stylesheet at all. The test names the class each button came out
  # carrying, which is a fact a `css: false` run still has.
  mutate web "a bare button back on Admin (Check reachability)" "$AS" \
    '<Button onClick={() => probe.mutate()}>Check reachability</Button>' \
    '<button type="button" onClick={() => probe.mutate()}>Check reachability</button>'

  # **The nomination back to primary.** A filled accent button in a group's
  # head, above the two Saves that are the writes — the loudest control on the
  # page pointing at the thing it is least about.
  mutate web "the default-project nomination back to primary" "$DP" \
    '          <Button
            variant="secondary"
            disabled={blocked}
            onClick={() => commit(chassis?.projectFile ?? null, SET)}
          >' \
    '          <Button
            variant="primary"
            disabled={blocked}
            onClick={() => commit(chassis?.projectFile ?? null, SET)}
          >'

  # **Tracked capitals back on a section title.** Two label styles on one page,
  # which is what made `Location` and `ORGANIZATION` read as two pages joined
  # at a heading.
  mutate web "uppercase back on an Admin section title" "$SC" \
    '.title {
  margin: 0 0 var(--density-gap);
  font-size: 0.9rem;' \
    '.title {
  margin: 0 0 var(--density-gap);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-size: 0.9rem;'

  # ---- Chunk 6g: every pane is a containing block --------------------------
  #
  # **Retired: four, one reason.** `a later rule unpositions .desk-main again`,
  # `the frame unpositioned inside a media block`, `a descendant selector takes
  # .desk-main's position back` and `a late rule takes .desk-console's position
  # back` were the override rows. They existed because the guard claimed to
  # catch a cascade override from source, and that claim is withdrawn: three
  # review rounds each found a construction past it — selector text compared as
  # a string, a first-match lookup, a nested `&` prelude — and a source reader
  # that nearly emulates the cascade is worse than one that does not try,
  # because it is believed. `scripts/containment-check.sh` holds that half now,
  # in a browser, by reading the computed `position` of the frame and the four
  # panes on 242 configurations a build. Three of the four constructions do still
  # trip the same-selector clause the reader kept (each is in the PR's
  # construction table, applied by hand and shown failing); they are retired
  # anyway, because a row is a claim about what the suite holds, and holding
  # them here would re-assert the one that was refuted. The fourth,
  # `.desk > .desk-main`, is green in the suite and red in the script, which is
  # exactly the division of labour this chunk ends with.
  #
  # **Retired: two more.** `position dropped from the textarea primitive` and
  # `position dropped from the raw code editor` were the user-agent scroller
  # rows. A `<textarea>` renders no element children, so no positioned
  # descendant can exist inside it and a `position` on it holds nothing —
  # measured in Chrome: a child with `position: absolute; top: 3000px` appended
  # to a textarea renders nothing and extends nothing. The list and the two
  # declarations were removed with these rows.
  #
  # **Retained: three.** Two break the pair inside the rule that declares it;
  # one writes a scroller that never had it.

  CHK=web/src/packs/CheckStrip.module.css

  # **The pane stops being a containing block.** `overflow` clips and scrolls
  # only the descendants whose containing block is inside the scroller, so a
  # static `.desk-main` sends every absolutely positioned descendant to the
  # *initial* containing block: not scrolled with the pane, not clipped by the
  # frame, and counted into the document's own scrollable overflow. jsdom lays
  # nothing out and vitest runs with `css: false`, so this is held by reading
  # the sheet.
  mutate web "position dropped from .desk-main, so the pane contains nothing" "$CSSH" \
    '    overflow: auto;
    position: relative;
    scrollbar-gutter: stable;' \
    '    overflow: auto;
    scrollbar-gutter: stable;'

  # **And the frame stops clipping what it does not contain.** `.desk` is
  # `overflow: hidden` on purpose — the scrolling belongs to the panes — which
  # is worth nothing against a descendant whose containing block is outside it.
  # The skip link at `left: -9999px` and the three hidden selects are exactly
  # such descendants.
  mutate web "position dropped from .desk, so the frame clips nothing" "$CSSH" \
    '    height: 100dvh;
    overflow: hidden;
    position: relative;' \
    '    height: 100dvh;
    overflow: hidden;'

  # **A scroller written next month, in a module nobody thought about.** This
  # is the row that says the test is a sweep and not a list of five names: the
  # rule added here is in a module this chunk never edited, and the invariant
  # has to reach it by shape.
  mutate web "a new module scroller that positions nothing" "$CHK" \
    '.check {
  margin: 0;
  font-size: 0.82rem;
}' \
    '.check {
  margin: 0;
  font-size: 0.82rem;
}

.checkScroll {
  max-height: 12rem;
  overflow-y: auto;
}'

  # ---- Chunk 6h: the page measure -----------------------------------------
  #
  # **Retired: none.** Nothing here replaces a row. The containing-block rows
  # above hold where a pane's content is clipped; these hold where a page's
  # content starts and stops, which was held by nothing — `.desk-measure` was
  # `margin: 0 auto` for as long as it has existed and no test looked at it.

  GRV=web/src/routes/GraphView.tsx

  # **The measure centred again.** The whole visible claim of this chunk is one
  # declaration: content beside a persistent rail starts at a fixed gutter, and
  # `auto` puts it wherever the window happens to be wide. On a 1893px Admin
  # that is dead space on both sides and a left edge that agrees with nothing —
  # and it is invisible to a render, because vitest runs with `css: false` and
  # jsdom lays nothing out, so it is held by reading the sheet.
  mutate web "the page measure centred again (margin: 0 auto)" "$CSSH" \
    '    max-width: var(--measure-wide);
    margin: 0;' \
    '    max-width: var(--measure-wide);
    margin: 0 auto;'

  # **The gutter that is not a density.** The gutter is on the `--density-`
  # scale so that a compact desk tightens the edge of the page as it tightens
  # everything inside it. A compact value equal to its comfortable one is the
  # scale's own defect one token at a time: the attribute is written, the
  # selector matches, the sheet resolves, and the left edge does not move.
  mutate web "the page measure's gutter: compact equal to comfortable" "$CSSP" \
    '  --density-gutter: 1.25rem;' \
    '  --density-gutter: 2rem;'

  # **A route that states no kind.** The measure declares `wide` as its default,
  # so a route with no attribute renders at 72rem and looks entirely correct —
  # which is why "every route states its kind" has to be a test and not a
  # convention. This takes the attribute off the graphs page, whose kind is
  # `full`: the page that most wants every pixel silently becomes a column.
  mutate web "the page measure unstated by a route (graphs takes the default)" "$GRV" \
    '    <article className="detail" data-measure="full">' \
    '    <article className="detail">'

  # **And the rule that reads the attribute.** The other half: a route may
  # state `form` and be given the wide measure anyway, because the `:has` rule
  # it is read by is gone. Admin then renders its label-and-value columns at
  # 72rem, which is the form whose labels and values are a screen apart that
  # this chunk exists to stop.
  mutate web "the page measure's form cap removed from its :has rule" "$CSSH" \
    '  .desk-measure:has([data-measure="form"]) {
    max-width: var(--measure-form);
  }

' \
    ''

  # ---- Chunk 6i: Admin as an overview, and the file in the right pane -----

  ADMV=web/src/routes/AdminView.tsx
  CFP=web/src/admin/ConfigPane.tsx
  SSUM=web/src/admin/sectionSummary.ts
  ISLOT=web/src/shell/InspectorSlot.tsx

  # **The disclosure back in the main column.** The whole of this chunk is that
  # the bytes are context and belong beside the form rather than under it: a
  # `details` on the page is the stack it took apart, one section at a time,
  # and it is invisible to every other test because the pane goes on being
  # published correctly beside it.
  mutate web "a Content disclosure back in Admin's main column" "$ADMV" \
    '      {pane}
      <header className="detail-head">' \
    '      {pane}
      <details>
        <summary>Content</summary>
        <pre>
          <code>{effective.text}</code>
        </pre>
      </details>
      <header className="detail-head">'

  # **The pane re-serialising the decode.** `idBase` gains its separator at
  # decode, `1e2` is not `100`, and an integer past a float64 is not what it
  # round-trips to — so a pane that stringified the value would be showing a
  # reader a file that is not on disk, beside a Location row saying where that
  # file is.
  mutate web "the pane shows a re-serialisation instead of the member's bytes" "$CFP" \
    '      ? text
      : memberBytes(text, member)' \
    '      ? text
      : JSON.stringify((JSON.parse(text) as Record<string, unknown>)[member], null, 2)'

  # **The safety rule, one pane over.** The decoder refuses a whole file for one
  # credential-shaped member, and the point of refusing it is that the desk will
  # not act on it; quoting it in the Inspector puts the member the refusal is
  # about into the DOM of the surface that reported it. This is why
  # `showsContent` is exported rather than spelled again here.
  mutate web "the pane quotes a file the decoder refused" "$CFP" \
    '  const quotable = showsContent(status) && text !== undefined' \
    '  const quotable = text !== undefined'

  # **A summary the decoder did not say.** The overview is a list of what each
  # setting currently is, and a row that fell back to a name this page composed
  # — the project it happens to be open on — is a value nobody wrote that a
  # reader cannot tell from one that is in the file.
  mutate web "an Admin row composes a summary the decoder did not say" "$SSUM" \
    "  organization: (config) => config.organization.name ?? 'none'," \
    "  organization: (config) => config.organization.name ?? 'this project',"

  # **The claim outliving the route.** Admin publishes the file into the
  # Inspector for as long as it is mounted; a claim that is never released
  # leaves the pane suppressing its own empty state for every route after it,
  # and the panel Admin published standing over a page it is not about.
  mutate web "the Inspector claim is never released, so Admin's pane outlives it" "$ISLOT" \
    '    if (!publishing) return
    return claim()' \
    '    if (!publishing) return
    claim()'

  # ---- The page's one actor -----------------------------------------------
  #
  # The four rows below break the property this whole design is: **one**
  # bootstrap, awaited by everything, and a refusal that is terminal. The
  # branch this replaced had several actors that could each read or replace the
  # id, and five review rounds closed the races between them one pair at a
  # time.
  MS=web/src/mcp/session.ts
  ASN2=web/src/assistant/session.ts
  FC=web/src/files/client.ts
  # **Memoised, and never reset.** Ten components mounting at once must spend
  # one handoff; the handoff is single use, so a second exchange is a `401` and
  # a page that half works.
  mutate web "the bootstrap is no longer memoised" "$MS" \
    '  bootstrapping ??= beginSession()
  return bootstrapping' \
    '  return beginSession()'
  # **Awaiting it is what makes "one credential, one holder" true of the code.**
  # A transport that reads ahead of the exchange sends an unauthorized request
  # and gets a 401 for a session that was about to exist.
  #
  # The edit necessarily drops the bearer too, because the bearer *is* what the
  # await produces — there is no synchronous source for it, which is the point.
  # The discriminating test is the ordering one: "sends nothing until the
  # exchange has answered".
  mutate web "a chassis call does not await the bootstrap" "$FC" \
    '  const id = await sessionBearer()' \
    "  const id = ''"
  # **The relay is a gated chassis route like any other.** It carried no bearer
  # on the branch this replaces, so every model listing and every generation
  # turn answered 401 the moment the session stopped being a cookie — and the
  # builder's own drive never reached either path.
  mutate web "the assistant's relay sends no bearer" "$ASN2" \
    '        headers: { ...headers, Authorization: `Bearer ${id}` },' \
    '        headers,'
  # **A response nobody reads is a request nobody closes.** A body stream that
  # is neither consumed nor cancelled leaves the request in flight for the life
  # of the page: invisible in using the desk, and caught by the containment
  # gate as a page that never reaches `networkidle`.
  # Repaired: the release moved into `refusedExchange` when the exchange grew
  # two codes to tell apart. The property is unchanged.
  # Repaired: the release moved past the spent branch, which now reads a body
  # of its own.
  mutate web "a refused answer's body is left in flight" "$MS" \
    '  await discardBody(answered)
  if (code === '"'"'handoff-expired'"'"') {' \
    '  if (code === '"'"'handoff-expired'"'"') {'
  # And the count read's own release, which is a second call on the same route:
  # a test that recorded whichever `/api/session` answer came last measured this
  # one while naming the other.
  mutate web "a refused count read's body is left in flight" "$MS" \
    '    // A refused read is not this function'"'"'s business: the id is dead, and the
    // call that meets it next says so. The body is let go of either way.
    await discardBody(answered)' \
    '    // A refused read is not this function'"'"'s business: the id is dead, and the
    // call that meets it next says so. The body is let go of either way.
    void answered'
  # **A theft read as a reload.** `handoff-spent` is what tells an
  # authenticated tab that its launch link was used by something else; treating
  # it like `no-handoff` keeps a session the person was never told about.
  mutate web "a spent handoff is treated as a reload" "$MS" \
    "    forgetSession(HANDOFF_SPENT_MESSAGE)
    return null
  }
  await discardBody(answered)" \
    "    return stored
  }
  await discardBody(answered)"
  # **The terminal state has to reach the sockets.** A connection established
  # before the refusal notices nothing on its own, and went on carrying frames
  # for a session the chassis had refused.
  mutate web "a session's end is published to nobody" "$MS" \
    '  for (const listener of [...ending]) {' \
    '  for (const listener of [] as (() => void)[]) {'
  # **And the provider has to be listening.**
  mutate web "the desk's connection ignores the session ending" "$M" \
    '    const stopWatching = whenSessionEnds(() => {
      if (timer !== undefined) clearTimeout(timer)' \
    '    const stopWatching = whenSessionEnds(() => {
      if (true) return
      if (timer !== undefined) clearTimeout(timer)'
  # **A relay 401 is classified by the mark and not by the status.** Reading the
  # status alone ends a desk session over an expired model key.
  mutate web "any relay 401 ends the session" "$ASN2" \
    "    if (answered.status === 401 && refusalCode(answered) === 'unauthorized') {" \
    '    if (answered.status === 401) {'
  # **A theft after the handoff's own lifetime.** How many other sessions this
  # desk is serving is the only signal that survives the ring forgetting the
  # handoff, and a reload hours later has nothing else to read.
  mutate web "the page never asks how many other sessions there are" "$MS" \
    '  if (stored !== null) await askAboutSessions(stored)
  return stored
}' \
    '  return stored
}'
  # **Derived, never remembered.** A page that stored what it last saw had to
  # store before it painted, and a reload in between silenced it for ever.
  mutate web "the other-sessions line is never derived" "$MS" \
    '  noticed = others > 0 ? otherSessionsMessage(others) : null' \
    '  noticed = null'
  # **A page's own spent link is not a theft.** The clear reaches `Path=/`, so a
  # copy planted at a longer path survives a tab's own exchange.
  mutate web "a page's own spent handoff reads as a theft" "$MS" \
    '    const echo =
      produced === 0 || (produced !== null && mine !== null && produced === mine)' \
    '    const echo = false'
  # **And somebody else's spend is not an echo.**
  mutate web "any spent handoff reads as this page's own" "$MS" \
    '    const echo =
      produced === 0 || (produced !== null && mine !== null && produced === mine)' \
    '    const echo = true'
  # **An opaque redirect is not an answer**, and an engine handed one is handed
  # nothing it can read.
  mutate web "an opaque redirect is delivered to the engine" "$ASN2" \
    "    if (answered.type === 'opaqueredirect' || (answered.status === 0 && answered.redirected)) {
      throw new Error(CALL_FAILED)
    }" \
    '    if (false) {
      throw new Error(CALL_FAILED)
    }'
  # **The dialog's proposal goes with the session.** Stopping the run leaves
  # `offered` true and Create willing to write it.
  mutate web "the Describe-it proposal survives the session's end" "$DI" \
    '      whenSessionEnds(() => {
        if (!hadSession.current) return
        discardNow.current()
        setLost(SLOT_LOST)
      }),' \
    '      whenSessionEnds(() => {
        if (true) return
        discardNow.current()
        setLost(SLOT_LOST)
      }),'
  # **`unauthorized` and no other code.** A 307 into the exchange answers a
  # marked `no-handoff`, and a classifier that read the mark alone turned that
  # into a forgotten valid session.
  mutate web "any marked relay 401 ends the session" "$ASN2" \
    "    if (answered.status === 401 && refusalCode(answered) === 'unauthorized') {" \
    '    if (answered.status === 401 && refusalCode(answered) !== null) {'
  # **A result that arrived after the end is not delivered.** Closing the socket
  # does not unmake a call that was already in flight.
  mutate web "a tool result in flight is delivered after the end" "$ASN2" \
    '          const answered = (await client.callTool({ name, arguments: args })) as McpToolResult
          if (ended !== null) throw new NoSession(ended)
          return answered' \
    '          return (await client.callTool({ name, arguments: args })) as McpToolResult'
  # **A refusal is the end of the road.** An id the chassis has rejected that
  # stays in storage is an id every later call re-sends, and a page that never
  # says the one sentence a person can act on.
  mutate web "a 401 does not forget the id" "$FC" \
    '  forgetSession()
  throw new NoSession()' \
    '  throw new NoSession()'
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
