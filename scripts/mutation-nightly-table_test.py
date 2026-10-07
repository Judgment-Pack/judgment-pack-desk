"""Run the nightly's table step on made-up shards, as the workflow's last job does."""
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent / "mutation-nightly-table.py"
BASE = "go\trow a\tTestA\ngo\trow c\tTestC\nweb\trow x\tsrc/x.test.ts\n"


class TableTests(unittest.TestCase):
    def setUp(self):
        self.dir = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(lambda: subprocess.run(["rm", "-rf", str(self.dir)]))
        (self.dir / "scripts").mkdir()
        (self.dir / "scripts/mutation-expects.tsv").write_text(BASE)
        self.shards = self.dir / "shards"
        self.n = 0

    def shard(self, half, k, n, rows, record=BASE, finished=True):
        d = self.shards / ("mutation-shard-%d" % self.n)
        self.n += 1
        d.mkdir(parents=True)
        (d / "shard.txt").write_text("%s %d %d\n" % (half, k, n))
        text = "mutation check against abc\n\n| mutation | test that failed |\n| --- | --- |\n"
        text += "".join("| %-52s | %s |\n" % row for row in rows)
        if finished:
            text += "\ndiscriminating: 1    not discriminating: 0\n"
        (d / "table.md").write_text(text)
        (d / "mutation-expects.tsv").write_text(record)

    def run_table(self, half="all"):
        out = self.dir / "out"
        result = subprocess.run(
            [sys.executable, str(SCRIPT), str(self.shards), str(out), "--half", half,
             "--run-url", "https://example.invalid/runs/1", "--date", "2026-10-07"],
            cwd=self.dir, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        return out, result.stdout

    def test_rows_come_back_in_order_and_survivors_open_an_issue(self):
        # Go rows a b c d e: shard 1/2 ran a c e, shard 2/2 ran b d.
        self.shard("go", 1, 2, [("row a", "TestA"), ("row c", "**NOT DISCRIMINATING — nothing failed**"), ("row e", "TestE")],
                   "go\trow a\tTestA\ngo\trow e\tTestE\nweb\trow x\tsrc/x.test.ts\n")
        self.shard("go", 2, 2, [("row b", "TestB"), ("row d", "**INCONCLUSIVE — suite timed out (the mutation hangs a handler)**")],
                   BASE.replace("go\trow c", "go\trow b\tTestB\ngo\trow c"))
        self.shard("web", 1, 2, [("row x", "it holds")])
        self.shard("web", 2, 2, [("row y", "it holds | and this")], finished=False)
        out, said = self.run_table()
        table = (out / "table.md").read_text().splitlines()
        self.assertEqual([line.split("|")[1].strip() for line in table[2:]],
                         ["row a", "row b", "row c", "row d", "row e", "row x", "row y", "web shard 2/2"])
        self.assertEqual((out / "mutation-expects.tsv").read_text(),
                         "go\trow a\tTestA\ngo\trow b\tTestB\ngo\trow e\tTestE\nweb\trow x\tsrc/x.test.ts\n")
        self.assertIn("survivors=1 inconclusive=1 not_run=1 clean=false", said)
        self.assertEqual((out / "issue-title.txt").read_text().strip(),
                         "Nightly mutation run 2026-10-07: 1 survivors, 1 inconclusive")
        issue = (out / "issue.md").read_text()
        self.assertIn("| row c | NOT DISCRIMINATING — nothing failed |", issue)
        self.assertIn("| row d | INCONCLUSIVE — suite timed out (the mutation hangs a handler) |", issue)
        self.assertIn("never a reason to hold a release", issue)
        self.assertIn("https://example.invalid/runs/1#artifacts", issue)
        self.assertIn("web shard 2/2 (DID NOT FINISH", issue)
        self.assertEqual((out / "clean").read_text().strip(), "false")

    def test_a_clean_run_of_every_row_may_close_the_issue(self):
        self.shard("go", 1, 1, [("row a", "TestA")])
        self.shard("web", 1, 1, [("row x", "it holds")])
        out, said = self.run_table()
        self.assertIn("clean=true", said)
        self.assertFalse((out / "issue.md").exists())
        self.assertIn("Every row was caught.", (out / "summary.md").read_text())

    def test_one_half_is_never_clean_and_a_missing_half_did_not_run(self):
        self.shard("web", 1, 1, [("row x", "it holds")])
        out, said = self.run_table(half="web")
        self.assertIn("clean=false", said)
        self.assertFalse((out / "issue.md").exists())
        out, said = self.run_table(half="all")
        self.assertIn("| go rows | **DID NOT RUN", (out / "table.md").read_text())
        self.assertIn("not_run=1 clean=false", said)

    def test_a_row_that_did_not_apply_is_not_caught(self):
        self.shard("go", 1, 1, [("row a", "MUTATION DID NOT APPLY")])
        self.shard("web", 1, 1, [("row x", "it holds")])
        out, said = self.run_table()
        self.assertIn("not_run=1 clean=false", said)
        self.assertFalse((out / "issue.md").exists(), "a needle is not a test-suite gap")


if __name__ == "__main__":
    unittest.main()
