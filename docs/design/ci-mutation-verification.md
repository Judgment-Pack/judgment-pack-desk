# Go mutation packages and split-job diagnostics

A Go mutation row may supply a sixth argument naming one test package:

```sh
mutate go 'bridge guard' internal/codexbridge/run.go \
  'original guard' 'removed guard' ./internal/codexbridge
```

Existing five-argument rows keep `./internal/desk`. Selectors admit only a single
existing `./internal/<package>` path with Go sources. Patterns, remote packages,
options, empty selectors and traversal are refused. Package selection is explicit;
the runner does not infer it from the mutated source file or a recorded test name.
The two bridge fence rows keep their original recorded
`TestRunHostFencesAtPhysicalAdmissionAndGeneration` test.

Each selected package gets one clean binary and full unmutated baseline before
any mutation. Failed builds/baselines are not mutation verdicts. Selection caches
include both package and exact declared test names. The clean binary lists every
declared test before a selection is admitted; missing or empty declared selections
are inconclusive. A row without a recorded selection retains whole-package testing.
Known tests that need whole-package setup retain the existing setup fallback.
Mutated selected tests and whole-package fallback use the same selected package.
Build errors, no tests, panics and timeouts remain inconclusive, never evidence
that a safeguard was caught or survived. Changed-row fingerprints include the
package argument so moving a row to another package schedules it again.

The required Go and Web jobs keep their concise Devkit summaries. Their
`JPDEV_LOGDIR` directories are uploaded by `always()` steps, including failure or
cancellation when log files exist. Artifact names are
`go-check-logs-<run id>-<attempt>` and `web-check-logs-<run id>-<attempt>`, retained
for seven days. Only per-step command output is included; these steps do not
upload the workspace, environment, credentials or caches. Missing logs are ignored.
A hand-authored long failing check verifies retention of its original late
assertion and exit status without replay or an automatic environment dump.
