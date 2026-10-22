"""Run the harness's own selection, fallback and record against stand-in suites.

The rows below are the harness's real driver, its preamble and its tail copied
whole, around rows of a disposable repository. `go` and `npm` are stand-ins on
a PATH that holds nothing else of the kind: each runs a pretend suite whose
tests fail when the source holds a marker, and logs how it was called, so a
test can say which runs a row cost as well as what the table says.
"""
import json
import pathlib
import re
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
args = sys.argv[1:]
package = next((a for a in args if a.startswith('./internal/')), './internal/desk')
TESTS = json.loads(os.environ.get('FAKE_GO_PACKAGES', '{}')).get(package, json.loads(os.environ["FAKE_GO_TESTS"]))
if os.environ.get('FAKE_GO_COMPILE_PACKAGE') == package:
    print('FAIL %s [build failed]' % package)
    sys.exit(1)
source_path = os.path.join(os.environ["FAKE_ROOT"], os.environ.get("FAKE_GO_SOURCE", package[2:] + "/a.go"))
with open(os.environ["FAKE_LOG"], "a") as log:
    log.write("go " + " ".join(sys.argv[1:]) + "\n")
def failing(selected):
    source = open(source_path).read()
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
    snapshot = open(source_path).read()
    with open(out, "w") as f:
        f.write("#!PYTHON\nimport os, re, sys\n")
        f.write("TESTS = %r\nSOURCE = %r\n" % (TESTS, snapshot))
        f.write(
            "argv = sys.argv[1:]\n"
            "if '-test.list' in argv:\n"
            "    pattern = argv[argv.index('-test.list') + 1]\n"
            "    [print(t) for t in TESTS if re.search(pattern,t)]\n"
            "    sys.exit(0)\n"
            "with open(os.environ['FAKE_LOG'], 'a') as log:\n"
            "    log.write('go-test-binary ' + ' '.join(argv) + '\\n')\n"
            "pattern = argv[argv.index('-test.run') + 1]\n"
            "selected = [t for t in TESTS if re.search(pattern, t)]\n"
            "if not selected: print('testing: warning: no tests to run')\n"
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
if os.environ.get("FAKE_NPM_SURVIVE"):
    source = ""
bad = []
for f in files:
    marker = open(os.path.join(root, "web", f)).readline().split("fails-on:")[1].strip()
    if marker in source:
        bad.append(f)
# FAKE_NPM_COLOR: colour the lines the harness reads, the way vitest does on
# GitHub's runner (codes between "Tests" and its count, around the cross, the
# FAIL badge and each " > ").
paint = "FAKE_NPM_COLOR" in os.environ
def c(code, text, close):
    return "\x1b[%sm%s\x1b[%sm" % (code, text, close) if paint else text
def counts(failed, passed, total):
    if failed:
        return c("1", c("31", "%d failed" % failed, "39"), "22") + c("2", " | ", "22") + c("1", c("32", "%d passed" % passed, "39"), "22") + c("90", " (%d)" % total, "39")
    return c("1", c("32", "%d passed" % passed, "39"), "22") + c("90", " (%d)" % total, "39")
for f in bad:
    print(c("31", "   " + c("31", "×", "39") + " %s holds" % f, "39") + c("2", " 1ms", "22"))
for f in bad:
    print(c("41", c("1", " FAIL ", "22"), "49") + " %s%sthe suite%s%s holds" % (f, c("2", " > ", "22"), c("2", " > ", "22"), f))
passed = len(files) - len(bad)
print(c("2", " Test Files ", "22") + " " + counts(len(bad), passed, len(files)))
print(c("2", "      Tests ", "22") + " " + counts(len(bad), passed, len(files)))
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
        selected = ["  mutate " + " ".join(map(shlex.quote, row)) for row in rows if row[0] == lang]
        body += selected or ["  :"]
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

    def test_new_mapping_row_reaches_the_real_driver_and_controls_its_final_exit(self):
        # Keep the entire real row layout, not harness_around's replacement
        # block: the selected row must be reachable at its actual location.
        name = "assistant dialect mapping: finite argument admission omitted"
        test = "TestAssistantDialectMappingArguments"
        target = "internal/desk/assistant_dialect_mapping_tools.go"
        (self.root / "scripts/mutation-check.sh").write_text(HARNESS.read_text())
        (self.root / target).write_text("if len(raw) > 1<<20 ||\n")
        (self.root / "scripts/mutation-expects.tsv").write_text("go\t%s\t%s\n" % (name, test))
        self.env["FAKE_GO_SOURCE"] = target
        self.git("add", "-A")
        self.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "-qsm", "real driver row fixture")

        for mode in ("caught", "survived", "failed-build", "failed-baseline"):
            with self.subTest(mode=mode):
                self.env.pop("FAKE_GO_COMPILE_PACKAGE", None)
                markers = {"caught": ["if true { _ = json.Unmarshal"],
                           "survived": [], "failed-build": [],
                           "failed-baseline": ["if len(raw) > 1<<20 ||"]}[mode]
                self.env["FAKE_GO_TESTS"] = json.dumps({test: markers})
                if mode == "failed-build":
                    self.env["FAKE_GO_COMPILE_PACKAGE"] = "./internal/desk"
                # Named-list CI and the operator's substring filter must both
                # encounter this row before reporting or deciding the result.
                for selection in (("--rows", str(self._rows(name))), (name,)):
                    result = self.harness("go", *selection)
                    self.assertEqual(set(self.table(result)), {name}, result.stdout + result.stderr)
                    self.assertNotIn("matching no row", result.stderr)
                    self.assertEqual(result.returncode, 0 if mode == "caught" else 1,
                                     result.stdout + result.stderr)
                    if mode == "caught":
                        self.assertEqual(self.table(result)[name], test)
                        self.assertEqual(self.how(result)[name], "selected")
                        self.assertIn("-run ^(%s)$" % test, self.log.read_text())
                        self.assertIn("discriminating: 1    not discriminating: 0", result.stdout)
                    elif mode == "survived":
                        self.assertEqual(self.table(result)[name],
                                         "**NOT DISCRIMINATING — nothing failed**")
                        self.assertEqual(self.how(result)[name], "fallback")
                        self.assertIn("discriminating: 0    not discriminating: 1", result.stdout)
                    else:
                        self.assertIn("BASELINE NOT GREEN", self.table(result)[name])
                        self.assertIn("discriminating: 0    not discriminating: 1", result.stdout)

    def test_new_record_row_reaches_the_real_driver_and_controls_its_final_exit(self):
        # Keep the entire real row layout, not harness_around's replacement
        # block: the selected row must be reachable at its actual location.
        name = "assistant dialect records: nested raw member types omitted"
        test = "TestAssistantDialectRecordFiniteArguments"
        target = "internal/desk/assistant_dialect_action_tools.go"
        (self.root / "scripts/mutation-check.sh").write_text(HARNESS.read_text())
        (self.root / target).write_text("readAssistantDialectCustody(raw, reflect.TypeOf(a)) != nil\n")
        (self.root / "scripts/mutation-expects.tsv").write_text("go\t%s\t%s\n" % (name, test))
        self.env["FAKE_GO_SOURCE"] = target
        self.git("add", "-A")
        self.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "-qsm", "real driver row fixture")

        for mode in ("caught", "survived", "failed-build", "failed-baseline"):
            with self.subTest(mode=mode):
                self.env.pop("FAKE_GO_COMPILE_PACKAGE", None)
                markers = {"caught": ["false"],
                           "survived": [], "failed-build": [],
                           "failed-baseline": ["readAssistantDialectCustody(raw, reflect.TypeOf(a)) != nil"]}[mode]
                self.env["FAKE_GO_TESTS"] = json.dumps({test: markers})
                if mode == "failed-build":
                    self.env["FAKE_GO_COMPILE_PACKAGE"] = "./internal/desk"
                # Named-list CI and the operator's substring filter must both
                # encounter this row before reporting or deciding the result.
                for selection in (("--rows", str(self._rows(name))), (name,)):
                    result = self.harness("go", *selection)
                    self.assertEqual(set(self.table(result)), {name}, result.stdout + result.stderr)
                    self.assertNotIn("matching no row", result.stderr)
                    self.assertEqual(result.returncode, 0 if mode == "caught" else 1,
                                     result.stdout + result.stderr)
                    if mode == "caught":
                        self.assertEqual(self.table(result)[name], test)
                        self.assertEqual(self.how(result)[name], "selected")
                        self.assertIn("-run ^(%s)$" % test, self.log.read_text())
                        self.assertIn("discriminating: 1    not discriminating: 0", result.stdout)
                    elif mode == "survived":
                        self.assertEqual(self.table(result)[name],
                                         "**NOT DISCRIMINATING — nothing failed**")
                        self.assertEqual(self.how(result)[name], "fallback")
                        self.assertIn("discriminating: 0    not discriminating: 1", result.stdout)
                    else:
                        self.assertIn("BASELINE NOT GREEN", self.table(result)[name])
                        self.assertIn("discriminating: 0    not discriminating: 1", result.stdout)

    def test_new_research_row_reaches_the_real_driver_and_controls_its_final_exit(self):
        # Keep the entire real row layout, not harness_around's replacement
        # block: the selected row must be reachable at its actual location.
        name = "assistant dialect research: queued complete source comparison omitted"
        test = "TestAssistantDialectResearchQueuedAuthority"
        target = "internal/desk/assistant_dialect_research_tools.go"
        (self.root / "scripts/mutation-check.sh").write_text(HARNESS.read_text())
        (self.root / target).write_text("if !assistantRunFreshSourceEqual(e.source, fresh) {\n")
        (self.root / "scripts/mutation-expects.tsv").write_text("go\t%s\t%s\n" % (name, test))
        self.env["FAKE_GO_SOURCE"] = target
        self.git("add", "-A")
        self.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "-qsm", "real driver row fixture")

        for mode in ("caught", "survived", "failed-build", "failed-baseline"):
            with self.subTest(mode=mode):
                self.env.pop("FAKE_GO_COMPILE_PACKAGE", None)
                markers = {"caught": ["false"],
                           "survived": [], "failed-build": [],
                           "failed-baseline": ["!assistantRunFreshSourceEqual(e.source, fresh)"]}[mode]
                self.env["FAKE_GO_TESTS"] = json.dumps({test: markers})
                if mode == "failed-build":
                    self.env["FAKE_GO_COMPILE_PACKAGE"] = "./internal/desk"
                # Named-list CI and the operator's substring filter must both
                # encounter this row before reporting or deciding the result.
                for selection in (("--rows", str(self._rows(name))), (name,)):
                    result = self.harness("go", *selection)
                    self.assertEqual(set(self.table(result)), {name}, result.stdout + result.stderr)
                    self.assertNotIn("matching no row", result.stderr)
                    self.assertEqual(result.returncode, 0 if mode == "caught" else 1,
                                     result.stdout + result.stderr)
                    if mode == "caught":
                        self.assertEqual(self.table(result)[name], test)
                        self.assertEqual(self.how(result)[name], "selected")
                        self.assertIn("-run ^(%s)$" % test, self.log.read_text())
                        self.assertIn("discriminating: 1    not discriminating: 0", result.stdout)
                    elif mode == "survived":
                        self.assertEqual(self.table(result)[name],
                                         "**NOT DISCRIMINATING — nothing failed**")
                        self.assertEqual(self.how(result)[name], "fallback")
                        self.assertIn("discriminating: 0    not discriminating: 1", result.stdout)
                    else:
                        self.assertIn("BASELINE NOT GREEN", self.table(result)[name])
                        self.assertIn("discriminating: 0    not discriminating: 1", result.stdout)

    def test_rows_precede_terminal_restore_and_no_rows_follow_final_reporting(self):
        source = HARNESS.read_text()
        terminal = source.rindex("\nrestore\necho\n")
        mapping = list(re.finditer(r"(?m)^  mutate go 'assistant dialect mapping: [^']+'", source))
        self.assertEqual(len(mapping), 26, "every new mapping/publication safeguard needs a live row")
        self.assertTrue(all(row.start() < terminal for row in mapping),
                        "mapping/publication rows must execute before terminal restore and selection reporting")
        declarations = list(re.finditer(r"(?m)^[ \t]*mutate (?:go|web) ", source))
        self.assertTrue(all(row.start() < terminal for row in declarations),
                        "no mutation row may be declared inside or after terminal restore/refusal/reporting")
        closing = source.rindex("\nselection_report\n")
        self.assertNotRegex(source[closing:], r"(?m)^\s*mutate (?:go|web) ",
                            "rows appended after the final report cannot affect its verdict")

    def test_graph_row_three_web_rows_reach_real_driver_and_keep_final_verdicts(self):
        names = [
            "graph row 3: partial writes leave queries stale",
            "graph row 3: a write without a lock describes a reviewed set",
            "graph row 3: edit provider disclosure omits current graph",
        ]
        source = HARNESS.read_text()
        (self.root / "scripts/mutation-check.sh").write_text(source)
        graph_author = self.root / "web/src/graphs/GraphAuthor.tsx"
        graph_author.parent.mkdir(parents=True, exist_ok=True)
        graph_author.write_text(
            "await queries.invalidateQueries()\n"
            "setSaved(accepted.hasLock)\n"
            "<p>{graphId ? msg('current graph document') : msg('prompt and tool answers')}</p>\n"
        )
        tests = {
            "partial.test.ts": "if (false) await queries.invalidateQueries()",
            "unlocked.test.ts": "setSaved(true)",
            "disclosure.test.ts": "<p>{false ? msg(",
        }
        for filename, marker in tests.items():
            path = self.root / "web/src/graphs" / filename
            path.write_text("// fails-on: %s\nimport './GraphAuthor'\n" % marker)
        rows = self._rows(*names)
        self.git("add", "-A")
        self.git("-c", "user.name=t", "-c", "user.email=t@example.invalid",
                 "commit", "-qsm", "graph row three real driver fixture")

        self.env.pop("FAKE_NPM_SURVIVE", None)
        caught = self.harness("web", "--rows", str(rows))
        table = self.table(caught)
        self.assertEqual(caught.returncode, 0, caught.stdout + caught.stderr)
        self.assertEqual(set(table), set(names), caught.stdout + caught.stderr)
        self.assertEqual(table, {
            names[0]: "src/graphs/partial.test.ts holds",
            names[1]: "src/graphs/unlocked.test.ts holds",
            names[2]: "src/graphs/disclosure.test.ts holds",
        })
        self.assertEqual(self.how(caught), {name: "selected" for name in names})
        self.assertIn("discriminating: 3    not discriminating: 0", caught.stdout)
        calls = self.log.read_text().splitlines()
        for filename in tests:
            self.assertTrue(any(filename in call for call in calls), calls)

        self.env["FAKE_NPM_SURVIVE"] = "1"
        survived = self.harness("web", "--rows", str(rows))
        self.assertEqual(survived.returncode, 1, survived.stdout + survived.stderr)
        self.assertEqual(self.table(survived), {
            name: "**NOT DISCRIMINATING — nothing failed**" for name in names
        })
        self.assertEqual(self.how(survived), {name: "fallback" for name in names})
        self.assertIn("discriminating: 0    not discriminating: 3", survived.stdout)

        missing = self._rows("graph row 3: absent row")
        refused = self.harness("web", "--rows", str(missing))
        self.assertEqual(refused.returncode, 2, refused.stdout + refused.stderr)
        self.assertIn("matching no row", refused.stderr)
        self.assertEqual(self.table(refused), {})

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

    def test_coloured_output_is_read_as_plain_text(self):
        # As vitest prints on GitHub's runner: the first nightly run read a
        # green suite as one that ran no tests, and stopped every web shard.
        self.env["FAKE_NPM_COLOR"] = "1"
        raw = subprocess.run([str(self.dir / "bin/npm"), "--prefix", "web", "test"], cwd=self.root, env=self.env,
                             capture_output=True, text=True).stdout
        self.assertIn("\x1b[", raw)
        self.assertNotRegex(raw, r"Tests +[0-9]+ passed", "the stand-in must colour what the harness reads")
        result = self.harness("web", "--record")
        table, how = self.table(result), self.how(result)
        self.assertNotIn("BASELINE NOT GREEN", result.stdout, result.stderr)
        self.assertEqual(table["web caught by its walk"], "src/a.test.ts holds,src/c.test.ts holds")
        self.assertEqual(how["web caught by its walk"], "selected")
        self.assertEqual(table["web caught two modules away"], "src/far.test.ts holds")
        self.assertEqual(table["web nothing catches"], "**NOT DISCRIMINATING — nothing failed**")
        self.assertEqual(self.expects()[("web", "web caught by its walk")], "src/a.test.ts,src/c.test.ts")

    def package_fixture(self, rows, entries, packages):
        self.env["FAKE_GO_PACKAGES"] = json.dumps(packages)
        for package in packages:
            path = self.root / package[2:] / "a.go"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("guard one\nguard bridge\n")
        (self.root / "scripts/mutation-check.sh").write_text(harness_around(rows))
        (self.root / "scripts/mutation-expects.tsv").write_text("".join(
            "go\t%s\t%s\n" % (name, tests) for name, tests in entries.items()))
        self.git("add", "-A")
        self.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qsm", "package fixture")

    def test_non_desk_package_baseline_selection_and_fallback(self):
        rows = [("go", "bridge selected", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge"),
                ("go", "bridge fallback", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge")]
        self.package_fixture(rows, {"bridge selected": "TestBridge", "bridge fallback": "TestOther"},
                             {"./internal/bridge": {"TestBridge": ["broken bridge"], "TestOther": []}})
        result = self.harness("go")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.table(result), {"bridge selected": "TestBridge", "bridge fallback": "TestBridge"})
        self.assertEqual(self.how(result)["bridge fallback"], "fallback")
        calls = self.log.read_text()
        self.assertIn("./internal/bridge", calls)
        self.assertNotIn("./internal/desk", calls)
        self.assertEqual(calls.count("go test -c"), 1, calls)

    def test_go_baseline_selected_and_full_runs_share_ordered_timeout_budgets(self):
        rows = [
            ("go", "bridge selected budget", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge"),
            ("go", "bridge full budget", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge"),
        ]
        self.package_fixture(rows, {
            "bridge selected budget": "TestBridge",
            "bridge full budget": "TestOther",
        }, {"./internal/bridge": {"TestBridge": ["broken bridge"], "TestOther": []}})

        source = HARNESS.read_text()
        self.assertIn("GO_TEST_WATCHDOG=480", source)
        self.assertIn("GO_TEST_BACKSTOP=540s", source)
        self.assertLess(480, 540, "the process-tree watchdog must fire before Go's panic timeout")
        self.assertIn('bounded 1800 "$log" go test -c', source, "build timeout must remain independent")
        self.assertIn('bounded "$GO_TEST_WATCHDOG" "$log" go test "$package" -count=1 -timeout "$GO_TEST_BACKSTOP" -run "$1"', source)
        self.assertIn('bounded "$GO_TEST_WATCHDOG" "$log" go test "$package" -count=1 -timeout "$GO_TEST_BACKSTOP"', source)
        self.assertIn('bounded "$GO_TEST_WATCHDOG" "$log" bash -c', source)

        result = self.harness("go")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.log.read_text().splitlines()
        executions = [line for line in calls if line.startswith("go test ./internal/bridge -count=1")]
        self.assertTrue(any("-timeout 540s -run ^(TestBridge)$" in line for line in executions), calls)
        self.assertTrue(any("-timeout 540s" in line and "-run" not in line for line in executions), calls)
        self.assertTrue(any(line.startswith("go-test-binary ") and "-test.timeout=540s" in line for line in calls), calls)
        self.assertEqual(self.how(result), {"bridge selected budget": "selected", "bridge full budget": "fallback"})

    def test_clean_listing_uses_package_directory_for_initialization(self):
        row = ("go", "package-relative initialization", "internal/bridge/a.go",
               "guard bridge", "broken bridge", "./internal/bridge")
        self.package_fixture([row], {row[1]: "TestBridge"},
                             {"./internal/bridge": {"TestBridge": ["broken bridge"]}})
        # Like TestMain, initialization runs before listing as well as tests.
        # Require the package-local fixture in the actual generated clean binary.
        fake_go = self.dir / "bin/go"
        source = fake_go.read_text()
        needle = "import os, re, sys\\n"
        self.assertEqual(source.count(needle), 1, "initialization guard must be injected exactly once")
        source = source.replace(
            needle, "import re, sys, os\\nif not os.path.isfile('a.go'): sys.exit(91)\\n")
        fake_go.write_text(source)
        result = self.harness("go")
        self.assertIn("unmutated Go baseline passed: ./internal/bridge", result.stderr)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.table(result), {row[1]: "TestBridge"})
        self.assertEqual(self.how(result), {row[1]: "selected"})

    def test_same_named_tests_do_not_share_package_selection_cache(self):
        rows = [("go", "desk shared", "internal/desk/a.go", "guard one", "broken desk"),
                ("go", "bridge shared", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge")]
        self.package_fixture(rows, {"desk shared": "TestShared", "bridge shared": "TestShared"},
                             {"./internal/desk": {"TestShared": ["broken desk"]},
                              "./internal/bridge": {"TestShared": ["broken bridge"]}})
        result = self.harness("go")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.how(result), {"desk shared": "selected", "bridge shared": "selected"})
        self.assertEqual(self.log.read_text().count("go test -c"), 2)

    def test_missing_or_empty_go_selection_is_no_verdict(self):
        for entry in ["", "TestMissing", "TestBridge,TestMissing", "TestBridge.*"]:
            with self.subTest(entry=entry):
                row=("go", "bad selection", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge")
                self.package_fixture([row], {"bad selection":entry}, {"./internal/bridge":{"TestBridge":["broken bridge"]}})
                result=self.harness("go")
                self.assertNotEqual(result.returncode,0)
                self.assertIn("INCONCLUSIVE",self.table(result)["bad selection"])
                self.assertNotIn("-count=1 -timeout",self.log.read_text(),"missing selection must not fall back to unrelated tests")

    def test_unsupported_and_missing_go_packages_are_no_verdict(self):
        for package in ["", "./internal/missing", "./...", "./internal/../desk", "github.invalid/pkg", "-race"]:
            with self.subTest(package=package):
                row=("go", "bad package", "internal/desk/a.go", "guard one", "broken one", package)
                self.package_fixture([row], {}, {})
                result=self.harness("go")
                self.assertNotEqual(result.returncode,0)
                self.assertIn("INCONCLUSIVE",self.table(result)["bad package"])
                self.assertEqual(self.log.read_text(),"")

    def test_failed_package_baseline_or_build_never_measures_a_mutant(self):
        row=("go", "bad baseline", "internal/bridge/a.go", "guard bridge", "broken bridge", "./internal/bridge")
        for build in [False,True]:
            with self.subTest(build=build):
                self.package_fixture([row], {"bad baseline":"TestBridge"}, {"./internal/bridge":{"TestBridge":["guard bridge"]}})
                if build:self.env["FAKE_GO_COMPILE_PACKAGE"]="./internal/bridge"
                result=self.harness("go")
                self.assertNotEqual(result.returncode,0)
                self.assertIn("BASELINE NOT GREEN",self.table(result)["bad baseline"])
                self.assertNotIn("-count=1 -timeout",self.log.read_text())
                self.env.pop("FAKE_GO_COMPILE_PACKAGE",None)

    def test_go_non_verdicts_never_become_caught_or_survived(self):
        source=HARNESS.read_text();functions=source[source.index('plain() {'):source.index('# run_web [test-file...]:')]
        for code,output in [(124,'--- FAIL: TestBridge (0.00s)\n'),(1,'--- FAIL: TestBridge (0.00s)\npanic: fixture\n'),(1,'--- FAIL: TestBridge (0.00s)\nfatal error: fixture\n'),(1,'FAIL fixture [build failed]\n'),(0,'ok fixture [no tests to run]\n'),(2,'unavailable\n')]:
            with self.subTest(code=code,output=output):
                path=self.dir/'verdict.log';path.write_text(output)
                result=subprocess.run(['bash','-c',functions+'\nwork="$1"; go_verdict "$2" "$3"','fixture',str(self.dir),str(code),str(path)],env=self.env,capture_output=True,text=True)
                self.assertTrue(result.stdout.startswith('INCONCLUSIVE'),result.stdout)

    def _rows(self, *names):
        path = self.dir / "rows.txt"
        path.write_text("".join(n + "\n" for n in names))
        return path


class DiagnosticLogsTests(unittest.TestCase):
    def test_late_failure_keeps_original_full_output_without_replay_or_environment_dump(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory);logs=root/"logs";counter=root/"calls"
            probe=root/"failing-check.py"
            probe.write_text("from pathlib import Path\np=Path("+repr(str(counter))+")\np.write_text(p.read_text()+'x' if p.exists() else 'x')\nfor i in range(1000): print('fixture line',i)\nprint('ORIGINAL ASSERTION after long output')\nraise SystemExit(37)\n")
            manifest=root/"manifest.json"
            manifest.write_text(json.dumps({"checks":[{"name":"late-failure","run":shlex.quote(sys.executable)+" "+shlex.quote(str(probe))}]}))
            env={"PATH":"/usr/bin:/bin","JPDEV_ROOT":str(root),"JPDEV_LOGDIR":str(logs),"PRIVATE_DIAGNOSTIC_TOKEN":"secret-fixture-never-printed"}
            result=subprocess.run(["bash",str(ROOT/"scripts/dev/check.sh"),"--manifest",str(manifest)],env=env,capture_output=True,text=True)
            self.assertEqual(result.returncode,1,result.stdout+result.stderr)
            self.assertIn("exit 37",result.stdout)
            self.assertNotIn("ORIGINAL ASSERTION",result.stdout)
            retained=(logs/"late-failure.log").read_text()
            self.assertIn("ORIGINAL ASSERTION after long output",retained)
            self.assertIn("fixture line 999",retained)
            self.assertNotIn(env["PRIVATE_DIAGNOSTIC_TOKEN"],retained)
            self.assertEqual(counter.read_text(),"x","diagnostic must retain the failed execution without replay")
            self.assertEqual([p.name for p in logs.iterdir()],["late-failure.log"])

    def test_split_jobs_upload_only_their_logs_on_all_outcomes_with_distinct_attempt_names(self):
        workflow=(ROOT/".github/workflows/ci.yml").read_text()
        commands={
            job: ['python3 scripts/dev/ci-ownership.py run --owner '+job+' --evidence "$RUNNER_TEMP/ci-ownership"']
            for job in ('go','web','integration')
        }
        for job,next_job in [("go","web"),("web","local-gateway"),("integration","release-archives")]:
            block=workflow.split("\n  "+job+":",1)[1].split("\n  "+next_job+":",1)[0]
            header,steps=block.split("\n    steps:",1)
            # runner context is unavailable in job.env; the helper's setup and checks
            # inherit the log directory from their command step's environment.
            self.assertNotIn("runner.temp",header)
            self.assertNotIn("JPDEV_LOGDIR",header)
            run_steps=[step for step in steps.split("\n      - ")[1:]
                       if step.startswith("run:") or "\n        run:" in step]
            run_lines=[next(line.strip() for line in step.splitlines() if line.strip().startswith("run:"))
                       for step in run_steps]
            self.assertEqual(run_lines,["run: "+command for command in commands[job]])
            logdir="check-logs" if job=="integration" else job+"-check-logs"
            for step in run_steps:
                self.assertEqual(step.count("JPDEV_LOGDIR:"),1,step)
                self.assertIn("\n        env:\n          JPDEV_LOGDIR: ${{ runner.temp }}/"+logdir+"\n",step)
            self.assertEqual(block.count("JPDEV_LOGDIR:"),len(commands[job]))
            label="Upload the step logs" if job=="integration" else "Upload the "+job+" step logs"
            upload=block.split("- name: "+label,1)[1]
            self.assertIn("if: always()",upload)
            self.assertIn("name: "+logdir+"-${{ github.run_id }}-${{ github.run_attempt }}",upload)
            self.assertIn("path: ${{ runner.temp }}/"+logdir+"/",upload)
            self.assertIn("retention-days: 7",upload)
            self.assertIn("if-no-files-found: ignore",upload)
            self.assertNotIn("github.workspace",upload)


if __name__ == "__main__":
    unittest.main()
