"""Run mutation-rows-changed.sh on a disposable repository's two versions of a harness."""
import pathlib
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent / "mutation-rows-changed.sh"

BASE = '''#!/usr/bin/env bash
echo "a preamble the rows never see"
if [ "$which" = all ] || [ "$which" = go ]; then
  F=internal/a.go
  mutate go "kept as it was" "$F" 'one' 'ONE'
  mutate go "its needle moves" "$F" 'two' 'TWO'
  mutate go "its file's variable changes" "$F" 'three' 'THREE'
  mutate go "removed" "$F" 'four' 'FOUR'
  mutate go "renamed" "$F" 'five' 'FIVE'
  mutate go "one name in both halves" "$F" 'six' 'SIX'
fi
if [ "$which" = all ] || [ "$which" = web ]; then
  mutate web "a web row kept" web/src/a.ts "$(printf 'x\\ty')" 'z'
  mutate web "one name in both halves" web/src/a.ts 'seven' 'SEVEN'
fi
restore
echo "discriminating: $pass"
[ "$fail" -eq 0 ]
'''

HEAD = BASE.replace(
    "'two' 'TWO'", "'two moved' 'TWO'").replace(
    '''  mutate go "its file's variable changes" "$F" 'three' 'THREE'
''', '''  G=internal/b.go
  mutate go "its file's variable changes" "$G" 'three' 'THREE'
''').replace(
    '''  mutate go "removed" "$F" 'four' 'FOUR'
''', '').replace(
    '"renamed"', '"renamed, under its new name"').replace(
    "'seven' 'SEVEN'", "'seven' 'SEVEN!'").replace(
    '''fi
if [ "$which" = all ] || [ "$which" = web ]; then''', '''  mutate go "added" "$F" 'eight' 'EIGHT'
fi
if [ "$which" = all ] || [ "$which" = web ]; then''')


class RowsChangedTests(unittest.TestCase):
    def test_added_and_changed_rows_of_each_half(self):
        root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        (root / "scripts").mkdir()
        shutil.copy(SCRIPT, root / "scripts/mutation-rows-changed.sh")
        git = lambda *a: subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", *a],
                                        cwd=root, check=True, capture_output=True, text=True).stdout.strip()
        git("init", "-q")
        (root / "scripts/mutation-check.sh").write_text(BASE)
        git("add", "-A")
        git("commit", "-qm", "base")
        base = git("rev-parse", "HEAD")
        (root / "scripts/mutation-check.sh").write_text(HEAD)
        git("commit", "-qam", "head")
        result = subprocess.run(["bash", "scripts/mutation-rows-changed.sh", base, str(root / "out")],
                                cwd=root, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("4 Go, 1 web", result.stdout)
        self.assertEqual((root / "out/go").read_text().splitlines(),
                         ["added", "its file's variable changes", "its needle moves", "renamed, under its new name"])
        self.assertEqual((root / "out/web").read_text().splitlines(), ["one name in both halves"])

        # Against itself, nothing.
        result = subprocess.run(["bash", "scripts/mutation-rows-changed.sh", "HEAD", str(root / "same")],
                                cwd=root, capture_output=True, text=True)
        self.assertIn("0 Go, 0 web", result.stdout)
        self.assertEqual((root / "same/go").read_text(), "")


if __name__ == "__main__":
    unittest.main()
