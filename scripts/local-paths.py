#!/usr/bin/env python3
"""Reject machine-local paths in tracked text; allow only reviewed file/literal pairs.

Exclusions name exact literals, never a file-wide waiver or a glob. Remove an entry
when its literal disappears. Prefer portable placeholders in historical records.
JSON slashes in the inventory are escaped so it needs no scanner exemption.
"""
import json
from pathlib import Path
import re
import subprocess
import sys

# Match the complete portable path literal, not just its first directory component.
LOCAL_PATH = re.compile(r"""(?<![A-Za-z0-9_])/(?:home|Users|tmp)/[A-Za-z0-9_.-]+[^\s"'`<>\[\]{}\\,;:()|]*""")
EXCLUSIONS = 'scripts/local-paths-exclusions.json'


def check(root, files, exclusions):
    errors = []
    allowed = {}
    for filename, entry in exclusions.items():
        if filename not in files:
            errors.append(f'{filename}: unknown exclusion file')
        if not isinstance(entry, dict) or set(entry) != {'reason', 'paths'}:
            errors.append(f'{filename}: exclusion needs reason and paths')
            continue
        paths = entry['paths']
        if not isinstance(entry['reason'], str) or not entry['reason'].strip():
            errors.append(f'{filename}: exclusion needs a reason')
        if (not isinstance(paths, list) or not paths or
                any(not isinstance(p, str) or LOCAL_PATH.fullmatch(p) is None for p in paths)):
            errors.append(f'{filename}: exclusion needs exact local path literals')
            continue
        if len(paths) != len(set(paths)):
            errors.append(f'{filename}: duplicate exclusion literal')
        allowed[filename] = set(paths)
    for filename in sorted(files):
        data = (root / filename).read_bytes()
        if b'\0' in data:
            if filename in allowed:
                errors.append(f'{filename}: exclusion names a binary file')
            continue
        found = set()
        for number, line in enumerate(data.decode('utf-8', errors='replace').splitlines(), 1):
            for match in LOCAL_PATH.finditer(line):
                literal = match.group()
                found.add(literal)
                if literal not in allowed.get(filename, set()):
                    errors.append(f'{filename}:{number}: machine-local path {literal}')
        for literal in sorted(allowed.get(filename, set()) - found):
            errors.append(f'{filename}: stale exclusion {literal}')
    return errors


def main():
    root = Path(__file__).resolve().parent.parent
    try:
        result = subprocess.run(['git', 'ls-files', '-z'], cwd=root, check=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        files = set(result.stdout.decode('utf-8').rstrip('\0').split('\0')) - {''}
        if not files:
            raise ValueError('git returned no tracked files')
        exclusions = json.loads((root / EXCLUSIONS).read_text())
        if not isinstance(exclusions, dict):
            raise ValueError('exclusions must be a file mapping')
        errors = check(root, files, exclusions)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'local-paths: cannot complete scan: {error}', file=sys.stderr)
        return 1
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        return 1
    print('local-paths: tracked text contains only reviewed local path literals')
    return 0


if __name__ == '__main__':
    sys.exit(main())
