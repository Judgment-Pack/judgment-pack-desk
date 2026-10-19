import type { RunIdentity } from '@judgment-pack/harness/host'

/**
 * What a run's connection names for its subordinate work, as the harness asks
 * (`modelsFor` in `@judgment-pack/harness`, v0.2.0): the model the critic runs
 * on, the model delegated children run on, and how many children run at once.
 *
 * **Nothing, until a connection says so.** Each member is absent, so the critic
 * and the children run on the run's own model and the harness's concurrency
 * applies.
 */
export function modelsFor(_run: RunIdentity): { critic?: string | null; child?: string | null; concurrentRequests?: number | null } {
  return {}
}
