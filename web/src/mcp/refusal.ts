/**
 * The one error type that means "the runtime answered, and its answer was no".
 *
 * It lives in its own module, importing nothing but types, so that every module
 * which must tell a refusal apart from a dropped socket — the query layer that
 * throws it, the views that read its envelope, and the walk helpers that word a
 * fallback — can import it without importing each other.
 */
import type { RefusalEnvelope } from './types'

/**
 * A refusal the runtime reported in band: `isError`, with the message as text
 * and, on the evaluation surface, the JPS §8.4 envelope as structuredContent.
 * Both are kept, so a view can show the machine-readable class and phase
 * instead of parsing them back out of the prose.
 *
 * Nothing else is a refusal. A dropped socket, an abort, a payload that was not
 * the JSON its tool promises — none of those is the runtime saying no, and a
 * view that called them one would attribute to the runtime a position it never
 * took.
 */
export class ToolRefusal extends Error {
  readonly envelope: RefusalEnvelope | undefined

  constructor(message: string, envelope: RefusalEnvelope | undefined) {
    super(message)
    this.name = 'ToolRefusal'
    this.envelope = envelope
  }
}
