import type { RunIdentity } from '@judgment-pack/harness/host'
import { allowedAgentModels } from '../config/deskConfig'
import { connectionConfig } from './connectionRecords'

/**
 * What a run's connection names for its subordinate work, as the harness asks
 * (`modelsFor` in `@judgment-pack/harness`, v0.2.0): the model the critic runs
 * on, the model delegated children run on, and how many children run at once.
 *
 * **Answered per connection, from the connection's own settings, and never from
 * the model name.** Two connections may serve one model with different
 * settings, and the harness asks once per run with the connection the run is
 * bound to. A model is named only where the connection's settings name one and
 * the desk's choice of models still holds it (the relay admits nothing else, so
 * a name the desk filtered out would be a refused request); otherwise the
 * critic and the children run on the model of the run. **Nothing for a run
 * whose connection this desk has no record of**, a connection edited since the
 * run's slot resolved it, or one that names nothing.
 */
export function modelsFor(run: RunIdentity): { critic?: string | null; child?: string | null; concurrentRequests?: number | null } {
  const config = connectionConfig(run.connection)
  if (config === undefined) return {}
  const settings = run.engine === 'codex' ? config.agent : config.endpoint
  if (settings === undefined || settings === null) return {}
  const allowed: readonly string[] = run.engine === 'codex' ? allowedAgentModels(config.agent) : (config.endpoint?.models ?? [])
  const held = (id: string | undefined) => (id !== undefined && allowed.includes(id) ? id : undefined)
  const critic = held(settings.criticModel)
  const child = held(settings.childModel)
  // How many children run at once is the connection's own statement; a subscription runs one at a time whatever is said.
  const capacity = run.engine === 'codex' ? undefined : config.endpoint?.capacity
  return { ...(critic === undefined ? {} : { critic }), ...(child === undefined ? {} : { child }), ...(capacity === undefined ? {} : { concurrentRequests: capacity }) }
}
