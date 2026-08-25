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

if [ -n "$(git status --porcelain)" ]; then
  echo "the tree is not clean; commit before mutating (this restores with git checkout)" >&2
  exit 2
fi

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

restore() { git checkout -- internal web/src 2>/dev/null; }

report() { # report <name> <result-line>
  printf '| %-52s | %s |\n' "$1" "$2"
}

run_go() {
  go test ./internal/desk -count=1 -timeout 120s 2>&1 |
    grep -E '^--- FAIL|^    --- FAIL' | sed 's/^ *--- FAIL: //;s/ (.*//' | sort -u | paste -sd', ' -
}

run_web() {
  npx --prefix web vitest run --root web 2>&1 |
    grep -E '^ *× ' | sed 's/^ *× //;s/ [0-9]*ms$//' | sort -u | paste -sd', ' -
}

mutate() { # mutate <lang> <name> <file> <needle> <replacement>
  local lang="$1" name="$2" file="$3" needle="$4" replacement="$5"
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
  if [ -z "$failures" ]; then
    report "$name" "**NOT DISCRIMINATING — nothing failed**"
    fail=$((fail + 1))
  else
    report "$name" "$failures"
    pass=$((pass + 1))
  fi
}

echo "| mutation | test that failed |"
echo "| --- | --- |"

if [ "$which" = all ] || [ "$which" = go ]; then
  F=internal/desk/files.go
  S=internal/desk/server.go

  mutate go "lexical path guard: dot-dot allowed" "$F" \
    '	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {' \
    '	if clean == "." || false {'
  mutate go "lexical path guard: backslash allowed" "$F" \
    '	if strings.ContainsRune(rel, '"'"'\\\\'"'"') {' \
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
  mutate go "read is bounded by the stat, not the reader" "$F" \
    '	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))' \
    '	data, err := io.ReadAll(f)'
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
    '	release := s.writes.acquire(clean)' \
    '	release := func() {}; _ = s.writes; _ = clean'
  mutate go "the save is a direct write, not a replace" "$F" \
    '	dir := path.Dir(clean)
	name, err := s.createStaging(dir)' \
    '	if true {
		f, ferr := s.root.OpenFile(osPath(clean), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if ferr != nil {
			return ferr
		}
		defer f.Close()
		_, werr := f.Write(data)
		return werr
	}
	dir := path.Dir(clean)
	name, err := s.createStaging(dir)'
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
    '		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), stagingPrefix) {' \
    '		if !d.Type().IsRegular() {'
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
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}' \
    ''
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
