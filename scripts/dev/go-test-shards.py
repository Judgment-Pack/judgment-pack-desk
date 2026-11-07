#!/usr/bin/env python3
"""The shards of internal/desk's Go tests, and the check that together they ran every test.

The package's suite is too long for one CI job, so scripts/dev/go-test.sh runs it in shards.
The package's top-level tests, sorted by name (byte order), are dealt out in turn: the i-th
(from 0) belongs to shard 1 + i mod N. Every machine computes the same split from the same
`go test -list` output, the shards differ by at most one test, and the tests of one feature,
whose names share a prefix and whose cost is alike, are spread over all the shards rather than
piled into one. No durations are recorded: the split needs none.

  go-test-shards.py select K/N LIST          the anchored -run pattern of shard K (stdout)
  go-test-shards.py summary K/N LIST LOG     a Markdown block of what shard K's log says
  go-test-shards.py complete N DIR...        every listed test reported in exactly one shard

LIST is the output of `go test -list '.*'` for the package. `complete` reads, in each DIR, the
files go-test.sh leaves there for one shard (go-test-shard.json, go-test-desk.list and
go-test-desk.log) and fails when a shard is missing or duplicated, when the shards listed
different tests, or when a listed test has no PASS, FAIL or SKIP line in any shard's log, or in
more than one. A test that never ran is never read as a pass. Standard library only.
"""
import json
import os
import re
import sys

NAME = re.compile(r'^(Test|Example|Fuzz)[A-Za-z0-9_]*$')
RESULT = re.compile(r'^--- (PASS|FAIL|SKIP): ([^\s/]+) \(([0-9.]+)s\)')
NESTED_FAIL = re.compile(r'^\s*--- FAIL: (\S+) \(')
OK_LINE = re.compile(r'^(ok|PASS|FAIL)(\s|$)')
STATE = 'go-test-shard.json'
LIST = 'go-test-desk.list'
LOG = 'go-test-desk.log'


def refuse(message):
    print('go-test-shards: ' + message, file=sys.stderr)
    sys.exit(2)


def parse_shard(text):
    match = re.fullmatch(r'([1-9][0-9]*)/([1-9][0-9]*)', text)
    if not match or int(match.group(1)) > int(match.group(2)):
        refuse('a shard is K/N with 1 <= K <= N, not %r' % text)
    return int(match.group(1)), int(match.group(2))


def shard_names(names, k, shards):
    """The tests of shard k of n, in the list's own order (the order go test runs them in)."""
    mine = set(sorted(names)[k - 1::shards])
    return [name for name in names if name in mine]


def read_list(path):
    """The runnable top-level tests of a `go test -list` output, in its order.

    Benchmarks are left out (go test runs none without -bench); any other line that is not a
    test name or the package's closing line is a refusal, so a build failure is never an
    empty shard."""
    names = []
    with open(path, encoding='utf-8', errors='replace') as handle:
        for line in handle.read().splitlines():
            if NAME.match(line):
                names.append(line)
            elif line.startswith('Benchmark') or OK_LINE.match(line) or not line.strip():
                continue
            else:
                refuse('%s: not a test list line: %r' % (path, line[:200]))
    if not names:
        refuse('%s lists no tests' % path)
    if len(set(names)) != len(names):
        refuse('%s lists a test twice' % path)
    return names


def results(path):
    """Top-level test -> list of results its log reports, and every failing test, nested too."""
    found, failed, timeout, seconds = {}, [], [], 0.0
    try:
        with open(path, encoding='utf-8', errors='replace') as handle:
            lines = handle.read().splitlines()
    except OSError:
        return None, [], [], 0.0
    for index, line in enumerate(lines):
        match = RESULT.match(line)
        if match:
            found.setdefault(match.group(2), []).append(match.group(1))
            seconds += float(match.group(3))
        match = NESTED_FAIL.match(line)
        if match:
            failed.append(match.group(1))
        if line.startswith('panic: test timed out after'):
            timeout.append(line)
            for following in lines[index + 1:index + 12]:
                if following.startswith('\t\t'):
                    timeout.append('still running: ' + following.strip())
                elif following.strip() and not following.startswith('\t'):
                    break
    return found, failed, timeout, seconds


def summary_block(label, names, found, failed, timeout, seconds, artifact=''):
    """A Markdown block: the shard's verdict, its failing tests and the tests it never reported."""
    out = []
    if found is None:
        out.append('#### %s: no log' % label)
        return out
    missing = [n for n in names if n not in found]
    verdict = 'FAIL' if failed or timeout or missing else 'PASS'
    head = '#### %s: %s, %d of %d tests reported, %d failing, %d min of tests' % (
        label, verdict, len(names) - len(missing), len(names), len(failed), round(seconds / 60))
    out.append(head + (' (log: `%s`)' % artifact if artifact else ''))
    for line in timeout:
        out.append('- `%s`' % line)
    for name in failed[:200]:
        out.append('- FAIL `%s`' % name)
    if len(failed) > 200:
        out.append('- ... %d more failing tests' % (len(failed) - 200))
    if missing:
        out.append('- %d assigned tests never reported a result, first: %s' %
                   (len(missing), ', '.join('`%s`' % n for n in missing[:10])))
    return out


def select(args):
    if len(args) != 2: refuse('select K/N LIST')
    k, n = parse_shard(args[0])
    names = shard_names(read_list(args[1]), k, n)
    if not names:
        refuse('shard %d/%d has no tests; choose fewer shards' % (k, n))
    print('^(%s)$' % '|'.join(names))


def summary(args):
    if len(args) != 3: refuse('summary K/N LIST LOG')
    k, n = parse_shard(args[0])
    names = shard_names(read_list(args[1]), k, n)
    print('\n'.join(summary_block('internal/desk shard %d/%d' % (k, n), names, *results(args[2]))))


def complete(args):
    if len(args) < 2: refuse('complete N DIR...')
    shards = int(args[0]) if args[0].isdigit() and int(args[0]) > 0 else refuse('N must be a positive count')
    problems, seen, listing, reported, report = [], {}, None, {}, []
    for directory in args[1:]:
        try:
            with open(os.path.join(directory, STATE), encoding='utf-8') as handle:
                state = json.load(handle)
            k, n = parse_shard(state['shard'])
        except (OSError, ValueError, KeyError, TypeError) as error:
            problems.append('%s: no shard record (%s)' % (directory, error))
            continue
        if n != shards:
            problems.append('%s: shard %d/%d is not one of %d shards' % (directory, k, n, shards))
            continue
        if k in seen:
            problems.append('shard %d/%d appears twice: %s and %s' % (k, n, seen[k], directory))
            continue
        seen[k] = directory
        names = read_list(os.path.join(directory, LIST))
        if listing is None:
            listing = names
        elif set(names) != set(listing):
            problems.append('%s: shard %d listed other tests than the first shard read' % (directory, k))
        mine = shard_names(names, k, n)
        found, failed, timeout, seconds = results(os.path.join(directory, LOG))
        report += summary_block('internal/desk shard %d/%d' % (k, n), mine, found, failed, timeout, seconds,
                                state.get('artifact', ''))
        for name, verdicts in (found or {}).items():
            reported.setdefault(name, []).extend((k, verdict) for verdict in verdicts)
    absent = sorted(set(range(1, shards + 1)) - set(seen))
    if absent:
        problems.append('no record of shard(s) %s of %d' % (', '.join(map(str, absent)), shards))
    if listing is not None:
        never = [name for name in listing if name not in reported]
        twice = [name for name in listing if len(reported.get(name, [])) > 1]
        unknown = sorted(set(reported) - set(listing))
        if never:
            problems.append('%d listed tests ran in no shard: %s' % (len(never), ', '.join(never[:20])))
        if twice:
            problems.append('%d tests reported more than once: %s' % (len(twice), ', '.join(twice[:20])))
        if unknown:
            problems.append('%d reported tests are not in the list: %s' % (len(unknown), ', '.join(unknown[:20])))
        tally = '%d listed tests, %d reported, over %d of %d shards' % (len(listing), len(set(reported) & set(listing)),
                                                                      len(seen), shards)
    else:
        tally = 'no shard listed its tests'
    lines = ['### internal/desk Go test shards', '', tally, ''] + report
    if problems:
        lines += ['', '**Incomplete:**'] + ['- ' + p for p in problems]
    text = '\n'.join(lines) + '\n'
    print(text, end='')
    target = os.environ.get('GITHUB_STEP_SUMMARY')
    if target:
        with open(target, 'a', encoding='utf-8') as handle:
            handle.write(text)
    if problems:
        print('go-test-shards: incomplete: ' + '; '.join(problems), file=sys.stderr)
        sys.exit(1)
    print('go-test-shards: complete: ' + tally)


if __name__ == '__main__':
    commands = {'select': select, 'summary': summary, 'complete': complete}
    if len(sys.argv) < 2 or sys.argv[1] not in commands:
        refuse('usage: select K/N LIST | summary K/N LIST LOG | complete N DIR...')
    commands[sys.argv[1]](sys.argv[2:])
