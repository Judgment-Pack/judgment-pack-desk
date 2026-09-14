"""Exercise the actual checker/apply commands against disposable file trees."""
import pathlib
import shlex
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts/needle-check.sh"
HARNESS = ROOT / "scripts/mutation-check.sh"


class NeedleCheckTests(unittest.TestCase):
    def check(self, content, needle, *, missing=False, empty=False):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "scripts").mkdir()
            if not missing:
                (root / "subject.txt").write_text(content)
            row = " ".join(map(shlex.quote, ["web", "fixture row", "subject.txt", needle, "replacement"]))
            (root / "scripts/mutation-check.sh").write_text(
                'if [ "$which" = all ] || [ "$which" = go ]; then\n'
                + ("  :\n" if empty else f"  mutate {row}\n") + "fi\n"
            )
            result = subprocess.run(["bash", str(CHECKER), str(root)], capture_output=True, text=True)
            if not missing:
                self.assertEqual((root / "subject.txt").read_text(), content)
            return result

    def test_one_exact_match_with_literal_shell_characters_and_newlines(self):
        result = self.check("before\n' $HOME $(unused) `unused`\nafter", "' $HOME $(unused) `unused`\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("rows checked: 1   invalid: 0", result.stdout)

    def test_missing_match_names_the_row_and_fails(self):
        result = self.check("present", "absent")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixture row", result.stdout)
        self.assertIn("invalid: 1", result.stdout)

    def test_multiple_matches_are_failure_not_just_a_warning(self):
        result = self.check("same same", "same")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("AMBIGUOUS (2x)", result.stdout)
        self.assertIn("fixture row", result.stdout)
        self.assertIn("invalid: 1", result.stdout)

    def test_missing_file_fails(self):
        result = self.check("", "absent", missing=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixture row", result.stdout)

    def test_empty_needle_fails_even_in_an_empty_file(self):
        self.assertNotEqual(self.check("", "").returncode, 0)

    def test_no_rows_is_not_a_passing_check(self):
        self.assertNotEqual(self.check("", "", empty=True).returncode, 0)

    def test_mutation_application_requires_exactly_one_match_before_writing(self):
        # Execute the harness's real apply function without its suite/restore
        # driver. No copied match logic in the test and no tracked file edits.
        source = HARNESS.read_text()
        function = source[source.index("apply() {"):source.index("\nreport() {")]
        for content, needle, expected in [("x x", "x", None), ("x", "y", None), ("", "", None), ("x", "x", "changed")]:
            with self.subTest(content=content, needle=needle), tempfile.TemporaryDirectory() as directory:
                path = pathlib.Path(directory) / "subject.txt"
                path.write_text(content)
                result = subprocess.run(["bash", "-c", function + '\napply "$@"', "apply", str(path), needle, "changed"], capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, expected is not None, result.stdout + result.stderr)
                self.assertEqual(path.read_text(), content if expected is None else expected)


if __name__ == "__main__":
    unittest.main()
