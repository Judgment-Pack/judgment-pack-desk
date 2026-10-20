import type { AITarget } from './aiConnections'
import type { AssistantConfig } from '../config/deskConfig'

/**
 * What the Desk knows of each AI connection a run can be bound to, by the
 * connection's id: its settings as this desk reads them (the machine's
 * connection with the desk's own choice of models applied), kept as each
 * assistant slot resolves them.
 *
 * The harness asks the host about a run's connection **after** the run has
 * started and with only the connection's name (`modelsFor`, asked with
 * `context.connection`, `@judgment-pack/harness` v0.2.0), so what the answer
 * is made of has to be reachable by that name. A slot writes its connection
 * here as it resolves it; a run reads it back by the same id.
 *
 * A record is read only for the revision it was written for: a connection edited
 * since the run's slot resolved it is a different connection, and the run is
 * answered as one the Desk knows nothing about rather than from settings it was
 * not started on.
 */
interface Held { revision?: string; assistant: AssistantConfig }
const held = new Map<string, Held>()

/** A slot resolved this connection with these settings. */
export function rememberConnection(target: AITarget, assistant: AssistantConfig): void {
  if (target.connectionId === undefined) return
  held.set(target.connectionId, { ...(target.connectionRevision === undefined ? {} : { revision: target.connectionRevision }), assistant })
}

/** The settings of the connection a run is bound to, or nothing where this desk has none for it. */
export function connectionConfig(target: AITarget | undefined): AssistantConfig | undefined {
  if (target?.connectionId === undefined) return undefined
  const record = held.get(target.connectionId)
  if (record === undefined) return undefined
  if (record.revision !== undefined && target.connectionRevision !== undefined && record.revision !== target.connectionRevision) return undefined
  return record.assistant
}

/** Forget every connection (a test's reset). */
export function forgetConnections(): void {
  held.clear()
}

/**
 * The settings of every remembered connection that serves one model on one
 * wire: those whose endpoint speaks that protocol and enables that model.
 */
export function connectionsServing(family: string, model: string): AssistantConfig[] {
  return [...held.values()]
    .map((record) => record.assistant)
    .filter((config) => config.endpoint !== null && config.endpoint.kind === family && config.endpoint.models.includes(model))
}
