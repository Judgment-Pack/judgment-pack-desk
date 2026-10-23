#!/usr/bin/env python3
"""Run semantic Go twins with overlays; never mutate a shared source file."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
rows = [
    ("retained answer digest", "assistant_run_continuation.go",
     "a.QuestionDigest != assistantRunQuestionDigest(q.Question)", "false",
     "answer", "answer digest must match retained question"),
    ("canonical grant receipt", "assistant_run_protocol.go",
     "if !found {", "if false {", "budget",
     "grant receipt must match canonical checkpoint"),
]
with tempfile.TemporaryDirectory(prefix="continuation-mutants-") as scratch:
    for label, filename, needle, replacement, case, marker in rows:
        source = root / "internal" / "desk" / filename
        text = source.read_text()
        if text.count(needle) != 1:
            raise SystemExit(f"mutation needle is not unique: {label}")
        mutant = Path(scratch) / filename
        mutant.write_text(text.replace(needle, replacement))
        overlay = Path(scratch) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}))
        command = ["go", "test", "-overlay", str(overlay), "./internal/desk", "-run",
                   "^TestAssistantRunContinuationSemanticReceiptGuards/" + case + "$", "-count=1", "-v"]
        if os.environ.get("JPDEV_WRAP"):
            command = [os.environ["JPDEV_WRAP"], "go", "--"] + command
        result = subprocess.run(command, cwd=root, capture_output=True, text=True, check=False)
        output = result.stdout + result.stderr
        if result.returncode != 1 or marker not in output or "--- FAIL:" not in output:
            raise SystemExit(f"non-semantic or surviving mutant: {label}\n{output}")
        print(f"Run continuation Go mutation: {label} caught", flush=True)
