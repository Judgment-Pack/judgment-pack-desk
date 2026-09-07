/**
 * `builtin` — the keyless fallback engine ADR-0001 ships first.
 *
 * One object implementing one contract. Everything it can reach passes through
 * a seam the desk owns: `callTool` is bound through the ToolGate, and its model
 * traffic goes to the chassis relay with no credential of its own.
 */
import { runBuiltin } from './loop'
import type { AssistantEvent, AssistantSession, Engine } from '../../engine'

export const builtin: Engine = {
  id: 'builtin',
  start(session: AssistantSession): AsyncIterable<AssistantEvent> {
    // The id travels with the session: what a loop removes from a schema is a
    // property of the engine, and the loop must not decide which engine it is.
    return runBuiltin(session, builtin.id)
  }
}

export default builtin
