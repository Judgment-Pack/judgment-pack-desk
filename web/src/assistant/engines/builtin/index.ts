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
    return runBuiltin(session)
  }
}

export default builtin
