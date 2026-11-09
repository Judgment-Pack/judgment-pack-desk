#!/usr/bin/env python3
"""Install the attested official companion archives the component lock names.

scripts/dev/official-companions.py BUNDLE DOWNLOADS

BUNDLE receives the programs and component-artifacts.json; DOWNLOADS receives the archives.
Both are created by this script and must not exist yet. The release lookups read GH_TOKEN.
"""
import importlib.util
import platform
import sys
from pathlib import Path


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main(argv):
    if len(argv) != 3:
        sys.exit(__doc__)
    bundle, downloads = Path(argv[1]), Path(argv[2])
    scripts = Path(__file__).resolve().parent.parent
    components = load('components', scripts / 'component-releases.py')
    published = load('published', scripts / 'published-components.py')
    system = {'Linux': 'linux', 'Darwin': 'darwin'}[platform.system()]
    machine = {'x86_64': 'amd64', 'AMD64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
    plan = components.read_plan()
    components.verify_plan(plan)
    bundle.mkdir()
    published.install(plan, system + '/' + machine, bundle, downloads)
    print((bundle / 'component-artifacts.json').read_text(), end='')


main(sys.argv)
