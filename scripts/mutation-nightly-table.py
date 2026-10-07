"""Put the nightly mutation run's shards back into one table, and say what it found.

    python3 scripts/mutation-nightly-table.py <shards> <out> --half all|go|web --run-url URL --date YYYY-MM-DD

<shards> holds one directory a shard (its shard.txt, table.md and the record it
left). Into <out> go:

- table.md: every shard's rows in the harness's own order. The shards are
  round-robin, so taking their rows in turn gives that order back. A shard
  whose table has no closing line did not finish, and is a row of its own.
- mutation-expects.tsv: the tracked record with each shard's changes over it.
  It stays an artifact: committing it is a person's step.
- summary.md: for the run's page.
- issue-title.txt and issue.md, where a row was a survivor or INCONCLUSIVE.
- clean: "true" where the run covered every row and every row was caught,
  the only run that may close the open survivor issue.

The run's verdicts are the harness's; this only gathers them.
"""
import argparse
import pathlib
import sys

ISSUE_ROWS = 200
GAP = ("This is a gap in the test suite to fix, never a reason to hold a release: "
       "the nightly mutation run is not a required check, and no workflow waits on it.")


def record(path):
    lines = {}
    if path.exists():
        for line in path.read_text(encoding="utf-8").splitlines():
            if line:
                kind, name, tests = line.split("\t")
                lines[(kind, name)] = tests
    return lines


def rows_of(text):
    return [line for line in text.splitlines()
            if line.startswith("| ") and not line.startswith(("| mutation |", "| --- |"))]


def verdict(row):
    """The row's name and what the harness said, from `| name | what |`."""
    name, _, said = row[2:].partition(" | ")
    return name.strip(), said[:-2].strip() if said.endswith(" |") else said.strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("shards", type=pathlib.Path)
    parser.add_argument("out", type=pathlib.Path)
    parser.add_argument("--half", choices=("all", "go", "web"), default="all")
    parser.add_argument("--run-url", default="")
    parser.add_argument("--date", default="")
    parser.add_argument("--record", type=pathlib.Path, default=pathlib.Path("scripts/mutation-expects.tsv"))
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)

    base = record(args.record)
    merged, halves = dict(base), {}
    for shard in sorted(p for p in args.shards.iterdir() if (p / "shard.txt").exists()):
        half, k, n = (shard / "shard.txt").read_text().split()
        text = (shard / "table.md").read_text(encoding="utf-8") if (shard / "table.md").exists() else ""
        halves.setdefault(half, {})[int(k)] = (int(n), rows_of(text), "discriminating:" in text)
        mine = record(shard / "mutation-expects.tsv")
        for key in set(mine) | set(base):
            if mine.get(key) != base.get(key):
                if key in mine:
                    merged[key] = mine[key]
                else:
                    merged.pop(key, None)

    table, unfinished = ["| mutation | test that failed |", "| --- | --- |"], []
    for half in ("go", "web"):
        if args.half not in ("all", half):
            continue
        parts = halves.get(half, {})
        if not parts:
            unfinished.append("| %s rows | **DID NOT RUN — no shard of this half reported** |" % half)
            continue
        n = max(p[0] for p in parts.values())
        for k in range(1, n + 1):
            if k not in parts or not parts[k][2]:
                unfinished.append("| %s shard %d/%d | **DID NOT FINISH — see its progress.log** |" % (half, k, n))
        for i in range(max(len(p[1]) for p in parts.values())):
            for k in range(1, n + 1):
                if k in parts and i < len(parts[k][1]):
                    table.append(parts[k][1][i])
    table += unfinished

    survivors, inconclusive, not_run = [], [], []
    for row in table[2:]:
        name, said = verdict(row)
        if "NOT DISCRIMINATING" in said:
            survivors.append((name, said))
        elif "INCONCLUSIVE" in said:
            inconclusive.append((name, said))
        elif said.startswith("**") or said == "MUTATION DID NOT APPLY":
            not_run.append((name, said))
    caught = len(table) - 2 - len(survivors) - len(inconclusive) - len(not_run)
    changed = sum(1 for key in set(merged) | set(base) if merged.get(key) != base.get(key))

    (args.out / "table.md").write_text("\n".join(table) + "\n", encoding="utf-8")
    with open(args.out / "mutation-expects.tsv", "w", encoding="utf-8") as f:
        for key in sorted(merged):
            f.write("%s\t%s\t%s\n" % (key[0], key[1], merged[key]))

    counts = "%d caught, %d survivors, %d inconclusive, %d not run; %d lines of the record changed" % (
        caught, len(survivors), len(inconclusive), len(not_run), changed)
    summary = [counts, ""]
    listed = survivors + inconclusive + not_run
    if listed:
        summary += ["| mutation | what happened |", "| --- | --- |"]
        summary += ["| %s | %s |" % (name, said.strip("*")) for name, said in listed]
    else:
        summary.append("Every row was caught.")
    (args.out / "summary.md").write_text("\n".join(summary) + "\n", encoding="utf-8")

    clean = args.half == "all" and not listed
    (args.out / "clean").write_text("true\n" if clean else "false\n")
    for stale in ("issue-title.txt", "issue.md"):
        (args.out / stale).unlink(missing_ok=True)
    if survivors or inconclusive:
        (args.out / "issue-title.txt").write_text(
            "Nightly mutation run %s: %d survivors, %d inconclusive\n" % (args.date, len(survivors), len(inconclusive)))
        body = ["The nightly mutation run %s (%s) found rows its test suite did not catch: %s." % (
                    args.date, args.run_url or "this run", counts),
                "", GAP, "",
                "| mutation | what happened |", "| --- | --- |"]
        shown = (survivors + inconclusive)[:ISSUE_ROWS]
        body += ["| %s | %s |" % (name, said.strip("*")) for name, said in shown]
        if len(survivors) + len(inconclusive) > ISSUE_ROWS:
            body.append("")
            body.append("…and %d more, in the run's table." % (len(survivors) + len(inconclusive) - ISSUE_ROWS))
        if not_run:
            body += ["", "Not run in this run: " + "; ".join("%s (%s)" % (n, s.strip("*")) for n, s in not_run[:20])]
        body += ["", "The table, each shard's progress log and the record this run left are the run's artifacts: %s" % (
            (args.run_url + "#artifacts") if args.run_url else "see the run")]
        (args.out / "issue.md").write_text("\n".join(body) + "\n", encoding="utf-8")
    print("survivors=%d inconclusive=%d not_run=%d clean=%s" % (
        len(survivors), len(inconclusive), len(not_run), "true" if clean else "false"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
