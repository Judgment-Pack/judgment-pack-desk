# Saved run reports

Desk response history keeps its existing `work.report` summary and connection
`timing`. The optional `work.report.canonical` is the complete published Agent
run-report v1 or v2 presentation. Legacy summaries remain readable unchanged.
This is an additive public saved-history surface.

Both terminal producers project the original validated execution checkpoint
through the published `ProjectRunReport` / `projectRunReport` API. Only complete
or stopped checkpoints qualify. Running and paused displays retain their
existing summary without manufacturing terminal state. Source arrays and
optional members survive whole, including prompts, provenance, skills, critic,
delegation, usage, child usage, failover, completions and Plan facts. Published
v2 feedback, usage totals, route and repeat-tool descriptions remain report
facts when retained; this does not enable their execution features.

The Go saved-conversation boundary supplies original report JSON bytes directly
to `ParseRunReport`. The browser passes preserved wire numeric literals through
`readRunReport` before accepting decoded response history. Duplicate members,
unsupported fields or versions, malformed nested values, lossy numbers and
reports over the published depth-64 bound are refused. Go enforces the
4 MiB bound on original report bytes; the browser enforces it on its retained
compact wire representation, so whitespace discarded by the browser wire
parser is not included in that browser bound. Validation never
rewrites the retained history. Existing 16 MiB whole-conversation storage bounds
also apply; no report members are silently dropped to fit. Browser retained wire
format compacts whitespace while preserving numeric spelling and array order.

The UI retains useful summary and timing lines and offers complete facts in a
collapsed report disclosure. A provider-managed allowance `used` counter is not
a native invocation count. Canonical reports use explicit complete invocation
totals or reported usage-ledger invocations; absent invocation facts display as
unknown. Historical summary-only reports keep their originally reported count.
Omitted token counts stay omitted, explicit zero stays zero, and incomplete v2
totals retain their `complete` distinction in the complete report.

Reports are descriptive data. They exclude approval, handoff, operation and
other resume capabilities. Reading or displaying a report cannot approve,
resume, dispatch, or otherwise authorize an action.
