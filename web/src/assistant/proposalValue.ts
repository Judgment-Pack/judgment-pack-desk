import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { sourceMessage } from '../i18n/source'

export function plain(value: unknown): unknown {
  let text: string | undefined
  try {
    text = JSON.stringify(value)
  } catch {
    return undefined
  }
  if (text === undefined) return undefined
  return JSON.parse(text) as unknown
}

/**
 * Plain data, frozen all the way down.
 *
 * Canonicalizing once is not enough on its own: what comes out is an ordinary
 * object, and it travels to the pane on `AssistantRun.events` where anything
 * holding the event could reach into it between the diff and the accept. The
 * diff would then describe one document and the writer write another — which is
 * the defect ingestion exists to prevent, one layer out. Frozen, the two are
 * the same object and there is nothing to disagree about.
 *
 * The recursion terminates because this only ever runs over the output of
 * `JSON.parse`, which has no cycles.
 */
export function frozen<T>(value: T): T {
  if (typeof value !== 'object' || value === null) return value
  for (const held of Object.values(value as Record<string, unknown>)) frozen(held)
  return Object.freeze(value)
}

/**
 * One proposal event as plain, frozen JSON data — or the error that says why
 * there is none.
 *
 * The whole payload together: the document, the unknowns and the critique. So
 * nothing an engine put on the event survives as a live object — no getters, no
 * `toJSON`, no functions, no symbol keys, no prototype — and nothing downstream
 * can move what is left.
 *
 * **A document that is not a JSON object is refused rather than shown.** A pack
 * is an object; `null`, `[]`, `7` and `"a pack"` are each a value this desk
 * cannot draw a diff of member by member and cannot write into a draft, and a
 * proposal offering one is a session that produced nothing to accept.
 */
export function canonicalProposal(
  event: Extract<AssistantEvent, { type: 'proposal' }>
): AssistantEvent {
  const held = plain({
    document: event.document,
    unknowns: event.unknowns,
    critique: event.critique
  }) as { document?: unknown; unknowns?: unknown; critique?: { refuted: boolean } } | undefined
  if (held === undefined) {
    return {
      type: 'error',
      message:
        sourceMessage("the proposal could not be read as JSON data; nothing was proposed and nothing was written")
    }
  }
  const document = held.document
  if (typeof document !== 'object' || document === null || Array.isArray(document)) {
    return {
      type: 'error',
      message:
        sourceMessage("the proposal carries no document object, so there is nothing to diff and nothing to write; nothing was written")
    }
  }
  return frozen({
    type: 'proposal',
    document,
    unknowns: Array.isArray(held.unknowns) ? held.unknowns.map((entry) => String(entry)) : [],
    ...(held.critique === undefined ? {} : { critique: held.critique })
  })
}
