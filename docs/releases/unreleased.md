# Unreleased

The v0.3.0 claim that both chat modes ask for test cases without sight of draft rules did not hold when a pack chat or “Ask Assistant about this” appended rule context. Those routes now keep Desk context separate from the person’s message and supplied files; the case writer receives only the latter. The main assistant still receives the full context. Older augmented turns are not used for case grounding because their authored material cannot reliably be separated from draft context; send the requirements again to ground new cases. Plain saved messages remain usable.
