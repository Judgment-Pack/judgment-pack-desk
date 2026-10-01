/** The evaluator version assumed for a runtime that does not report one.
 * Runtime v0.24.0 and later name it beside every example listing as
 * evaluatorSpecVersion, and that is what `useEvaluatorVersion` uses. An older
 * or external jpack names nothing and refuses spec_version on its example
 * tools, so its evaluator is taken to be the one runtime v0.23.1 shipped. */
export const FALLBACK_EVALUATOR_SPEC_VERSION = '0.2.0-draft'
