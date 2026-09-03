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
		return http.StatusForbidden, errorBody(err)
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
    '		case derr == nil || !req.CreateParents:' \
    '		case derr == nil:'
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
    '			if strings.Count(parent, "/")+1 > maxWalkDepth {' \
    '			if false {'
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
  JC=web/src/packs/jpackConfig.ts
  NP=web/src/packs/newPack.ts
  UI=web/src/ui/Button.tsx
  FD=web/src/ui/Field.tsx
  SEL=web/src/ui/Select.tsx
  DG=web/src/ui/Dialog.tsx
  AL=web/src/ui/Alert.tsx
  SELCSS=web/src/ui/Select.module.css
  ST=web/src/mcp/starters.ts

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
    '    <aside className="desk-inspector" aria-label="Inspector" id="desk-inspector" hidden={!open}>' \
    '    <aside className="desk-inspector" aria-label="Inspector" id="desk-inspector">'
  mutate web "the file channel is fed by nothing" "$M" \
    "        recordFileChange(String((notification.params as { path?: unknown })?.path ?? ''))" \
    '        void recordFileChange'
  mutate web "a config problem no longer refuses the whole file" "$D" \
    '  if (problems.length > 0) return { values: undefined, problems }' \
    '  if (false) return { values: undefined, problems }'
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
    '      {creating && <CreatePackDialog open onOpenChange={setCreating} />}' \
    '      <CreatePackDialog open={creating} onOpenChange={setCreating} />'
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
  mutate web "a partial listing is reported as an empty project" "$A" \
    '            {files.length === 0 && partial.length === 0 ? (' \
    '            {files.length === 0 ? ('
  mutate web "the partial warning is not shown" "$A" \
    '            {partial.length > 0 && (' \
    '            {false && (' 
  mutate web "the save read-back installs over a newer read" "$A" \
    '          if (state !== undefined && state.dataUpdatedAt > startedAt) return' \
    '          void state'
  mutate web "reload trusts refetch rather than its own read" "$A" \
    '    void readFile(path)' \
    '    void loaded.refetch().then((r) => r.data!)
      .then((fresh) => { setBase(fresh); setBuffer(fresh.content) })
      .catch(() => {})
    void Promise.resolve(base)'
  # Create a pack: the sequence, the entry it writes, and the name it refuses.
  mutate web "create writes the pack and never registers it" "$X" \
    '        await writeFile({
          path: PROJECT_FILE,
          content: serialiseProjectConfig(read.content, amended),
          baseSha256: read.sha256
        })' \
    '        void amended'
  mutate web "the registration writes a digest it did not read" "$X" \
    '          baseSha256: read.sha256' \
    "          baseSha256: ''"
  mutate web "createParents is not sent" "$X" \
    "        await writeFile({ path, content, baseSha256: '', createParents: true })" \
    "        await writeFile({ path, content, baseSha256: '', createParents: false })"
  mutate web "the pack is written before the project is known to have a jpack.json" "$X" \
    '      if (!files.includes(PROJECT_FILE)) {' \
    '      if (false) {'
  mutate web "a 409 on jpack.json is reported as an ordinary failure" "$X" \
    '          reason: cause instanceof StaleWrite ? STALE_PROJECT_FILE : reasonOf(cause)' \
    '          reason: reasonOf(cause)'
  mutate web "the form error is not a live region" "$X" \
    '          <p role="alert">' \
    '          <p>'
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
    '  if (project.files.includes(project.path)) {' \
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
    '  const holdsFiles = (listing.data?.files ?? []).some((file) =>
    file.path.startsWith(`${packDir}/`)
  )' \
    '  const holdsFiles = true
  void packDir'
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
    "            style={{ '--drawer-w': \`\${width}px\` } as CSSProperties}" \
    '            '

  # 4. One global touched bit made a single toggle speak for all three panes:
  # the other two were serialized from the built-in defaults, and a stored
  # record outranks the configuration file for ever after.
  mutate web "an untouched pane is persisted along with the touched one" "$P" \
    '  if (touched.left) record.left = state.left
  if (touched.inspector) record.inspector = state.inspector
  if (touched.console) record.console = state.console' \
    '  record.left = state.left
  record.inspector = state.inspector
  record.console = state.console'
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
    '    if (cause instanceof FileRequestError && cause.status === 404) {' \
    '    if (true) {'
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
  # it is up to four times looser on a mark that is not ASCII.
  mutate web "the mark's size bound is counted in code units" "$D" \
    '  const bytes = new TextEncoder().encode(value).length' \
    '  const bytes = value.length'

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
    '      let read: FileContent
      let current: ProjectConfig
      try {
        read = await readFile(PROJECT_FILE)
        current = parseProjectConfig(read.content)
      } catch (cause) {
        setFailure({ lead: UNREADABLE_PROJECT_FILE, reason: reasonOf(cause) })
        return
      }' \
    '      let read: FileContent = { path: PROJECT_FILE, bytes: 0, sha256: "", content: "{}" }
      let current: ProjectConfig = {}
      try {
        read = await readFile(PROJECT_FILE)
        current = parseProjectConfig(read.content)
      } catch {
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
    '          reason: cause instanceof StaleWrite ? PACK_FILE_TAKEN : reasonOf(cause)' \
    '          reason: reasonOf(cause)'
  mutate web "the amended configuration is left in the cache as it was" "$X" \
    "      invalidate([['desk-files'], ['desk-file', PROJECT_FILE], ['list_packs'], ['desk-config']])" \
    "      invalidate([['desk-files'], ['list_packs'], ['desk-config']])"

  # The templates: what is offered, and what is said about it.
  mutate web "a runtime that has not answered is reported as one with no templates" "$X" \
    "      ? status === 'ready'" \
    '      ? true'
  mutate web "the empty pack is offered with no schema to derive it from" "$X" \
    '      ...(schemaSupported ? [{ value: EMPTY, label: EMPTY_LABEL }] : [])' \
    '      ...[{ value: EMPTY, label: EMPTY_LABEL }]'
  mutate web "the empty pack is offered as a finished document" "$X" \
    '          hint={isEmpty && template !== undefined ? EMPTY_IS_A_START : undefined}' \
    '          hint={undefined}'
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
    '  if (project.paths.includes(project.path)) {' \
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
