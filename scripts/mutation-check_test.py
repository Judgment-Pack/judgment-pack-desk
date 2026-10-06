"""Run the harness's own selection, fallback and record against stand-in suites.

The rows below are the harness's real driver, its preamble and its tail copied
whole, around rows of a disposable repository. `go` and `npm` are stand-ins on
a PATH that holds nothing else of the kind: each runs a pretend suite whose
tests fail when the source holds a marker, and logs how it was called, so a
test can say which runs a row cost as well as what the table says.
"""
import json
import pathlib
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
HARNESS = ROOT / "scripts/mutation-check.sh"
ANCHOR = 'if [ "$which" = all ] || [ "$which" = go ]; then\n'

# The pretend Go suite: each test fails when internal/desk/a.go holds its
# marker. TestAlone also fails whenever TestSetup is not run before it, as a
# test that leans on another's leftovers would.
GO_TESTS = {
    "TestSetup": [],
    "TestOne": ["broken one"],
    "TestTwo": ["broken two"],
    "TestBoth": ["broken one", "broken two"],
    "TestAlone": ["broken alone"],
}

FAKE_GO = r'''#!PYTHON
import json, os, re, sys
TESTS = json.loads(os.environ["FAKE_GO_TESTS"])
with open(os.environ["FAKE_LOG"], "a") as log:
    log.write("go " + " ".join(sys.argv[1:]) + "\n")
def failing(selected):
    source = open(os.path.join(os.environ["FAKE_ROOT"], "internal/desk/a.go")).read()
    out = [t for t in selected if any(m in source for m in TESTS[t])]
    if "TestAlone" in selected and "TestSetup" not in selected:
        out.append("TestAlone")
    return out
def run(pattern):
    selected = [t for t in TESTS if pattern is None or re.search(pattern, t)]
    if not selected:
        print("testing: warning: no tests to run")
        print("ok  \tdesk\t0.01s [no tests to run]")
        return 0
    bad = failing(selected)
    for t in bad:
        print("--- FAIL: %s (0.00s)" % t)
    print("FAIL" if bad else "ok  \tdesk\t0.01s")
    return 1 if bad else 0
args = sys.argv[1:]
if args[:2] == ["test", "-c"]:
    out = args[args.index("-o") + 1]
    # A binary of the tree as it is now: what fails is fixed at build time.
    snapshot = open(os.path.join(os.environ["FAKE_ROOT"], "internal/desk/a.go")).read()
    with open(out, "w") as f:
        f.write("#!PYTHON\nimport re, sys\n")
        f.write("TESTS = %r\nSOURCE = %r\n" % (TESTS, snapshot))
        f.write(
            "argv = sys.argv[1:]\n"
            "pattern = argv[argv.index('-test.run') + 1]\n"
            "selected = [t for t in TESTS if re.search(pattern, t)]\n"
            "bad = [t for t in selected if any(m in SOURCE for m in TESTS[t])]\n"
            "if 'TestAlone' in selected and 'TestSetup' not in selected: bad.append('TestAlone')\n"
            "[print('--- FAIL: %s (0.00s)' % t) for t in bad]\n"
            "print('FAIL' if bad else 'PASS')\n"
            "sys.exit(1 if bad else 0)\n"
        )
    os.chmod(out, 0o755)
    sys.exit(0)
pattern = args[args.index("-run") + 1] if "-run" in args else None
sys.exit(run(pattern))
'''

# The pretend web suite: a test file fails when a source under web/src holds
# the marker its first line names.
FAKE_NPM = r'''#!PYTHON
import os, re, sys
root = os.environ["FAKE_ROOT"]
with open(os.environ["FAKE_LOG"], "a") as log:
    log.write("npm " + " ".join(sys.argv[1:]) + "\n")
filters = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
files = sorted(
    os.path.relpath(os.path.join(d, n), os.path.join(root, "web"))
    for d, _, names in os.walk(os.path.join(root, "web/src")) for n in names if n.endswith(".test.ts")
)
if filters:
    files = [f for f in files if any(x in f for x in filters)]
if not files:
    print("No test files found, exiting with code 1")
    sys.exit(1)
source = "".join(
    open(os.path.join(d, n)).read()
    for d, _, names in os.walk(os.path.join(root, "web/src")) for n in names if not n.endswith(".test.ts")
)
bad = []
for f in files:
    marker = open(os.path.join(root, "web", f)).readline().split("fails-on:")[1].strip()
    if marker in source:
        bad.append(f)
for f in bad:
    print("   × %s holds 1ms" % f)
for f in bad:
    print(" FAIL  %s > the suite > %s holds" % (f, f))
passed = len(files) - len(bad)
print(" Test Files  %s" % ("%d failed | %d passed (%d)" % (len(bad), passed, len(files)) if bad else "%d passed (%d)" % (passed, passed)))
print("      Tests  %s" % ("%d failed | %d passed (%d)" % (len(bad), passed, len(files)) if bad else "%d passed (%d)" % (passed, passed)))
sys.exit(1 if bad else 0)
'''

WEB_FILES = {
    "web/src/a.ts": "export const a = 'guard w1 guard w2 guard w3'\n",
    "web/src/c.ts": "import { a } from './a'\nexport const c = a\n",
    "web/src/d.ts": "import { c } from './c'\nexport const d = c\n",
    # Imports the mutated file: chosen.
    "web/src/a.test.ts": "// fails-on: broken w1\nimport { a } from './a'\n",
    # Through one module between: chosen.
    "web/src/c.test.ts": "// fails-on: broken w1\nimport { c } from './c'\n",
    # Two modules away: not chosen, so only the whole suite or a record finds it.
    "web/src/far.test.ts": "// fails-on: broken w2\nimport { d } from './d'\n",
    # Imports nothing of it.
    "web/src/b.test.ts": "// fails-on: never\n",
    # Imported by nothing, and read only as text by a test that never names it
    # in a way the walk can see.
    "web/src/lonely.ts": "export const lonely = 'guard lonely'\n",
    "web/src/static.test.ts": "// fails-on: broken lonely\n",
}

ROWS = [
    ("go", "go caught by its entry", "internal/desk/a.go", "guard one", "broken one"),
    ("go", "go entry stale, another test catches", "internal/desk/a.go", "guard two", "broken two"),
    ("go", "go nothing catches", "internal/desk/a.go", "guard three", "broken three"),
    ("go", "go entry fails alone", "internal/desk/a.go", "guard alone", "broken nothing"),
    ("go", "go no entry", "internal/desk/a.go", "guard nothing", "broken one"),
    ("web", "web caught by its walk", "web/src/a.ts", "guard w1", "broken w1"),
    ("web", "web caught two modules away", "web/src/a.ts", "guard w2", "broken w2"),
    ("web", "web nothing catches", "web/src/a.ts", "guard w3", "broken w3"),
    ("web", "web entry gone, whole suite catches", "web/src/lonely.ts", "guard lonely", "broken lonely"),
]

EXPECTS = {
    ("go", "go caught by its entry"): "TestOne",
    ("go", "go entry stale, another test catches"): "TestOne",
    ("go", "go nothing catches"): "TestOne",
    ("go", "go entry fails alone"): "TestAlone",
    # A test file since renamed: the selection runs no test at all.
    ("web", "web entry gone, whole suite catches"): "src/gone.test.ts",
}


def harness_around(rows):
    """The real harness with its rows replaced by these."""
    source = HARNESS.read_text()
    start = source.index(ANCHOR)
    tail = source.rindex("\nrestore\necho\n")
    body = []
    for lang in ("go", "web"):
        body.append('if [ "$which" = all ] || [ "$which" = %s ]; then' % lang)
        body += ["  mutate " + " ".join(map(shlex.quote, row)) for row in rows if row[0] == lang]
        body.append("fi")
    return source[:start] + "\n".join(body) + "\n" + source[tail:]


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.dir = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.dir)
        self.root = self.dir / "repo"
        self.log = self.dir / "calls.log"
        bin_dir = self.dir / "bin"
        bin_dir.mkdir()
        for name, text in (("go", FAKE_GO), ("npm", FAKE_NPM)):
            path = bin_dir / name
            path.write_text(text.replace("#!PYTHON", "#!" + sys.executable))
            path.chmod(0o755)
        # Only what the harness needs, by full path: no installed `go` or
        # `npm` can be reached from this PATH.
        for name, target in (("python3", sys.executable), ("git", shutil.which("git"))):
            (bin_dir / name).symlink_to(target)
        self.env = {
            "PATH": "%s:/usr/bin:/bin" % bin_dir,
            "HOME": str(self.dir),
            "TMPDIR": str(self.dir),
            "FAKE_ROOT": str(self.root),
            "FAKE_LOG": str(self.log),
            "FAKE_GO_TESTS": json.dumps(GO_TESTS),
            "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_CONFIG_SYSTEM": "/dev/null",
        }
        files = {
            "scripts/mutation-check.sh": harness_around(ROWS),
            "scripts/mutation-expects.tsv": "".join(
                "%s\t%s\t%s\n" % (k, n, t) for (k, n), t in sorted(EXPECTS.items())
            ),
            "internal/desk/a.go": "guard one\nguard two\nguard three\nguard alone\nguard nothing\n",
            **WEB_FILES,
        }
        for name, text in files.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        self.git("init", "-q")
        self.git("add", "-A")
        self.git("-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "fixture")

    def git(self, *args):
        subprocess.run(["git", *args], cwd=self.root, env=self.env, check=True, capture_output=True)

    def harness(self, *args):
        self.log.write_text("")
        result = subprocess.run(
            ["bash", "scripts/mutation-check.sh", *args],
            cwd=self.root, env=self.env, capture_output=True, text=True, timeout=600,
        )
        self.assertEqual(
            subprocess.run(["git", "status", "--porcelain", "internal", "web"], cwd=self.root, env=self.env,
                           capture_output=True, text=True).stdout, "", "the tree was not restored")
        return result

    def table(self, result):
        rows = {}
        for line in result.stdout.splitlines():
            if line.startswith("| ") and not line.startswith("| mutation") and not line.startswith("| ---"):
                name, verdict = [part.strip() for part in line.strip("|").split("|", 1)]
                rows[name] = verdict.strip(" |")
        return rows

    def how(self, result):
        decided = {}
        for line in result.stderr.splitlines():
            parts = line.split(None, 2)
            if len(parts) == 3 and parts[0].endswith("s") and parts[0][:-1].isdigit():
                decided[parts[2]] = parts[1]
        return decided

    def expects(self):
        lines = (self.root / "scripts/mutation-expects.tsv").read_text().splitlines()
        return {(k, n): t for k, n, t in (line.split("\t") for line in lines)}

    def test_selection_decides_only_a_catch_and_the_whole_suite_decides_the_rest(self):
        result = self.harness("all", "--record")
        table, how, calls = self.table(result), self.how(result), self.log.read_text()
        self.assertNotEqual(result.returncode, 0, "two rows survive, so the run fails")

        # Caught by the test its entry names, and by that run alone.
        self.assertEqual(table["go caught by its entry"], "TestOne")
        self.assertEqual(how["go caught by its entry"], "selected")
        self.assertIn("-run ^(TestOne)$", calls)

        # The entry's test passes, so the whole package decides, and finds the
        # tests that do catch it.
        self.assertEqual(how["go entry stale, another test catches"], "fallback")
        self.assertIn("TestTwo", table["go entry stale, another test catches"])

        # Nothing catches it: called a survivor only after the whole package.
        self.assertEqual(table["go nothing catches"], "**NOT DISCRIMINATING — nothing failed**")
        self.assertEqual(how["go nothing catches"], "fallback")

        # Its test fails by itself on the unmutated tree, so the selection is
        # not trusted: the whole package runs, and nothing catches the row.
        self.assertEqual(how["go entry fails alone"], "set-aside")
        self.assertEqual(table["go entry fails alone"], "**NOT DISCRIMINATING — nothing failed**")

        self.assertEqual(how["go no entry"], "no-selection")
        self.assertIn("TestOne", table["go no entry"])

        # The walk: the file's own importer, and one through a module between;
        # not the file two modules away, and not one that imports nothing of it.
        selected = [line for line in calls.splitlines() if line.startswith("npm") and "--" in line.split()]
        self.assertTrue(selected, calls)
        first = selected[0].split()
        self.assertIn("src/a.test.ts", first)
        self.assertIn("src/c.test.ts", first)
        self.assertNotIn("src/far.test.ts", first)
        self.assertNotIn("src/b.test.ts", first)
        self.assertEqual(how["web caught by its walk"], "selected")
        self.assertEqual(table["web caught by its walk"], "src/a.test.ts holds,src/c.test.ts holds")

        self.assertEqual(how["web caught two modules away"], "fallback")
        self.assertEqual(table["web caught two modules away"], "src/far.test.ts holds")
        self.assertEqual(how["web nothing catches"], "fallback")
        self.assertEqual(table["web nothing catches"], "**NOT DISCRIMINATING — nothing failed**")

        # A selection that ran nothing is INCONCLUSIVE, which a selection never
        # reports: the whole suite runs, and catches it.
        self.assertIn("npm --prefix web test -- src/gone.test.ts", calls)
        self.assertEqual(how["web entry gone, whole suite catches"], "fallback")
        self.assertEqual(table["web entry gone, whole suite catches"], "src/static.test.ts holds")

        # The record: what caught each row this time; a survivor's line gone.
        self.assertEqual(self.expects(), {
            ("go", "go caught by its entry"): "TestOne",
            ("go", "go entry stale, another test catches"): "TestBoth,TestTwo",
            ("go", "go no entry"): "TestBoth,TestOne",
            ("web", "web caught by its walk"): "src/a.test.ts,src/c.test.ts",
            ("web", "web caught two modules away"): "src/far.test.ts",
            ("web", "web entry gone, whole suite catches"): "src/static.test.ts",
        })

        # Run again from that record, uncommitted: the far file is now chosen
        # by its line, and the stale Go row is decided by its new line.
        again = self.harness("all")
        self.assertIn("mutation check against", again.stdout, again.stderr)
        how = self.how(again)
        self.assertEqual(how["web caught two modules away"], "selected")
        self.assertEqual(how["go entry stale, another test catches"], "selected")

    def test_without_record_the_file_is_untouched(self):
        before = (self.root / "scripts/mutation-expects.tsv").read_text()
        result = self.harness("go", "--rows", str(self._rows("go entry stale, another test catches")))
        self.assertEqual((self.root / "scripts/mutation-expects.tsv").read_text(), before)
        self.assertIn("1 rows found other than their line", result.stderr)

    def test_whole_runs_no_selection(self):
        result = self.harness("go", "--whole", "--rows", str(self._rows("go caught by its entry")))
        self.assertEqual(self.how(result)["go caught by its entry"], "whole")
        self.assertEqual(self.table(result)["go caught by its entry"], "TestBoth,TestOne")
        self.assertNotIn("-run ^(", self.log.read_text())

    def test_shards_split_the_rows_between_them(self):
        seen = []
        for shard in ("1/3", "2/3", "3/3"):
            seen.append(set(self.table(self.harness("web", "--shard", shard))))
        web = {r[1] for r in ROWS if r[0] == "web"}
        self.assertEqual(sum(len(s) for s in seen), len(web), "a row ran in two shards")
        self.assertEqual(set().union(*seen), web)
        self.assertTrue(all(seen), "a shard ran nothing")

    def test_a_listed_name_that_matches_no_row_fails_the_run(self):
        result = self.harness("go", "--rows", str(self._rows("go caught by its entry", "a row that was renamed")))
        self.assertEqual(result.returncode, 2)
        self.assertIn("a row that was renamed", result.stderr)
        self.assertEqual(self.table(result)["go caught by its entry"], "TestOne")

    def test_only_the_record_may_be_left_uncommitted(self):
        (self.root / "scripts/mutation-expects.tsv").write_text("")
        result = self.harness("go", "--rows", str(self._rows("go caught by its entry")))
        self.assertEqual(result.returncode, 0, result.stderr)
        (self.root / "internal/desk/a.go").write_text("guard one\nwork nobody committed\n")
        result = subprocess.run(["bash", "scripts/mutation-check.sh", "go"], cwd=self.root, env=self.env,
                                capture_output=True, text=True, timeout=600)
        self.assertEqual(result.returncode, 2)
        self.assertIn("the tree is not clean", result.stderr)
        self.assertIn("work nobody committed", (self.root / "internal/desk/a.go").read_text())

    def _rows(self, *names):
        path = self.dir / "rows.txt"
        path.write_text("".join(n + "\n" for n in names))
        return path


if __name__ == "__main__":
    unittest.main()
