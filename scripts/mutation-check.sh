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
  # **Repaired**: the answer is built by `keyState` now that it carries the
  # binding too. The mutation is the same defect — the value where the
  # fingerprint belongs.
  mutate go "the key is answered to the page instead of its fingerprint" "$A" \
    '		Fingerprint: fingerprint(stored.key),' \
    '		Fingerprint: stored.key,'
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
		writeJSON(w, http.StatusOK, DeskLevelConfig{Path: path, Present: false})
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
  mutate go "a configured query is read as bytes the browser cannot read" "$DF" \
    '			if err != nil || !utf8.ValidString(decoded) {' \
    '			if err != nil {'
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
    '	if !deskConfigUnmoved(req.IfMatch, actual) {
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
  # This desk's own credential, in the place it is easiest to forget.
  # **Two rows are gone and this one replaced them.** They broke a *strip* —
  # the page's query forwarded with this chassis' token taken out of it — and
  # that arrangement leaked the token three times, three ways, to three
  # reviewers: `%74oken` (the guard decodes names and a raw compare did not),
  # `;` (Go rejects such a pair and some servers split on it), and `Token`
  # (this desk compared case-sensitively and ASP.NET Core folds case). Each fix
  # was a better comparison and the next parser disagreed somewhere else, so
  # there is no strip any more: a relayed request carries the session token and
  # nothing else, and nothing of the page's query is forwarded. One rule, one
  # row.
  mutate go "a page query parameter is forwarded" "$MR" \
    '		if err != nil || decoded != sessionTokenParameter {' \
    '		if false && (err != nil || decoded != sessionTokenParameter) {'
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
  # Two parsers disagreed about `;` and the desk's own session token went to the
  # endpoint: Go reads `x=1;token=T&token=T` as one `token` parameter and
  # accepts it, while a strip that removed the pair it could see preserved the
  # first one for a server that does split on `;`.
  mutate go "a query two parsers read differently is forwarded" "$MR" \
    "$(printf '\tif strings.ContainsRune(raw, %s) {' "';'")" \
    '	if false {'
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
  mutate go "a second copy of the stream pair is admitted" "$MR" \
    '				parameter == relayStreamPair && extra == "" {' \
    '				parameter == relayStreamPair {'
  mutate go "the relay's log line carries the whole address" "$MR" \
    '	s.log.Printf("desk: assistant relay %s answered %d", loggableOrigin(endpoint.url), status)' \
    '	s.log.Printf("desk: assistant relay %s %s answered %d", endpoint.url, suffix, status)'
  mutate go "the relay runs without the session guard" "$MR" \
    '	if !s.guard(w, r) {
		return
	}' \
    ''
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
  VB=web/src/routes/adminBlocks.tsx
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
  mutate web "the configured theme is decoded and never applied" "$W" \
    '  useAppliedTheme(value.config.appearance.theme)' \
    '  void value.config.appearance.theme'
  # The paste block and the source badge moved into adminBlocks.tsx when the
  # Assistant section came to need them too.
  mutate web "the copy button reports a copy it did not make" "$VB" \
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
  # Two `instanceof` checks now — the desk-level read has one of its own — so
  # the needle carries the line that follows to name the project read's.
  mutate web "a browser error is attributed to the chassis" "$Z" \
    '    if (cause instanceof FileRequestError) {
      if (cause.status === 404) {' \
    '    if (false) {
      if (cause.status === 404) {'
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
    '        {state.present && (' \
    '        {true && ('
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
    "    await fetch(chassisUrl('/api/assistant/probe'), { method: 'POST', signal })" \
    "    await fetch(chassisUrl('/api/assistant/probe'), {
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
    "          {endpoint === null ? 'none — no endpoint configured' : 'a model endpoint'}" \
    "          {endpoint === null ? 'none — no assistant, and no key' : 'a model endpoint'}"
  mutate web "a diagnostic is rendered as the bare word" "$AS" \
    "      {result.diagnostic !== '' && (" \
    "      {false && result.diagnostic !== '' && ("

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
  mutate web "a failed model call rethrows the browser's own error" "$ASN" \
    '      throw new Error(CALL_FAILED)' \
    '      throw cause'
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
  mutate web "the assistant reuses one shared transport" "$ASN" \
    'export function assistantTransport(): Transport {
  return new DeskWebSocketTransport(socketURL(sessionToken()))
}' \
    'let sharedTransport: Transport | undefined
export function assistantTransport(): Transport {
  sharedTransport ??= new DeskWebSocketTransport(socketURL(sessionToken()))
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
    '    const problem = suffixProblem(suffix)' \
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
    '        options.call(openai.suffix, {' \
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
            signal: run.controller.signal
          })
          run.connection = opened' \
    '          const opened = openAssistantConnection({
            allowed: endpoint.tools,
            onEvent: (event) => push(run, event)
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
  return suffix === '' ? undefined : suffix" \
    '  return url'

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
    "  if (tier === 'off') return null
  const effort = tier === 'ultra' ? 'xhigh' : 'high'" \
    "  const effort = tier === 'ultra' ? 'xhigh' : 'high'"

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
    "      if (sent === undefined || !isTruncatedSignature(sent.signature, block.signature)) return true" \
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
    "  for (const member of TIER_MEMBERS) delete payload[member]
  Object.assign(payload, membersAfter() ?? {})" \
    "  void TIER_MEMBERS
  void membersAfter"

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
    '  const usable = slot.endpoint !== null && slot.keyPresent' \
    '  const usable = slot.endpoint !== null'

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
