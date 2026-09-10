/**
 * What the Create dialog says when a write is refused.
 *
 * **Nothing here is a translation of a message.** The chassis sends a stable
 * `code` beside every refusal precisely so a caller can decide without reading
 * English, and this is the caller doing that. The chassis' own sentence is
 * written for whoever is editing a file — "the directory packs does not exist
 * in the project; create it first", "this editor writes at most N bytes" — and
 * putting it in front of somebody who typed a pack's name and pressed Create is
 * putting a different tool's diagnostic in front of them.
 *
 * So the code chooses a sentence about *creating a pack*, and the chassis'
 * words go to the details line underneath, where a diagnostic belongs. A code
 * this file does not know produces no lead of its own: the caller keeps its own
 * general sentence rather than this file inventing a specific one for a
 * refusal it does not understand.
 */
import { FileRequestError, StaleWrite } from '../files/client'

/**
 * Every code the chassis declares, mirrored so the two can be checked against
 * each other.
 *
 * `internal/desk/files.go` is the authority; this is a copy, and a copy that
 * silently drifts is worse than none. The test walks this list against the Go
 * constants in the source and fails on either side gaining a member the other
 * does not have — which is how a code the dialog has never heard of gets
 * noticed at the moment it is added rather than the first time a user meets it.
 */
export const CHASSIS_CODES = [
  'outside-root',
  'symlink',
  'not-found',
  'directory-missing',
  'parent-is-a-file',
  'too-deep',
  'exists',
  'stale',
  'too-large',
  'not-utf8',
  'not-a-file',
  'unauthorized',
  'no-handoff',
  'handoff-spent',
  'handoff-expired',
  'sessions-full',
  'forbidden',
  'bad-request',
  'staging-file',
  'excluded-directory',
  'assistant-unconfigured',
  'assistant-no-key',
  'assistant-unusable-store',
  'assistant-relay-path',
  'assistant-relay-busy',
  'assistant-relay-upstream',
  'assistant-listing-refused',
  'assistant-key-unbound',
  'desk-config-changed',
  'desk-config-refused',
  'internal'
] as const

export type ChassisCode = (typeof CHASSIS_CODES)[number]

/**
 * The codes this dialog deliberately handles as **control flow** rather than
 * as a refusal to report.
 *
 * `not-found` is the whole of it: the create sequence *asks* whether a file is
 * there and reads a 404 as "no", so it never reaches a sentence. Naming it here
 * is what keeps that from looking like an omission — the exhaustiveness test
 * below requires every other code to have a sentence, and requires these to
 * have none.
 */
export const CONTROL_FLOW_CODES = ['not-found'] as const

/**
 * The codes no create can ever meet, because they belong to another endpoint.
 *
 * **This is a third category, and it is here rather than absent because the
 * alternative was worse.** The code set is the *chassis'*, not the file API's,
 * and it grew members that only the assistant's own routes answer with — the
 * probe's three, the model relay's three, the key binding's one, and the
 * desk-level write's two.
 * Giving them a sentence about creating a pack would put text on the page
 * asserting something that cannot happen; leaving them out would let the
 * exhaustiveness test below be satisfied by an omission. So they are listed,
 * with the reason, and every code still has to be in exactly one of the three
 * lists — "nobody thought about it" is still not a category.
 */
export const OTHER_ENDPOINT_CODES = [
  'assistant-unconfigured',
  'assistant-no-key',
  'assistant-unusable-store',
  // The model relay's three. A create writes a file through the file API and
  // never carries model traffic, so none of them can reach this dialog either.
  'assistant-relay-path',
  'assistant-relay-busy',
  'assistant-relay-upstream',
  // …and the one the relay answers about a listing whose body carried the
  // configured key. A create carries no model traffic and asks for no listing.
  'assistant-listing-refused',
  // The key binding's one: a credential entered for one endpoint and a desk
  // now configured for another. A create carries no model traffic.
  'assistant-key-unbound',
  // The desk-level write's two. A create writes a project file through the
  // file API; these belong to the one route that rewrites the `assistant`
  // object of the file in `~/.config`, which no create touches.
  'desk-config-changed',
  'desk-config-refused',
  // The exchange's four. `POST /api/session` is what the page calls once at
  // load — nothing this desk recognises was presented, a handoff it had
  // finished with was, one it had let lapse was, or it holds as many sessions
  // as it will — and no create ever calls that route.
  'no-handoff',
  'handoff-spent',
  'handoff-expired',
  'sessions-full'
] as const

/** The chassis codes this dialog has a sentence for. */
export const CREATE_REFUSALS: Readonly<Record<string, string>> = {
  // The parent the configuration names is not there and could not be made.
  'directory-missing':
    'The folder configured for packs is not there, and it could not be created. Nothing was created.',
  // Something at the parent's path is a regular file. A rename fixes it, and
  // this is emphatically not a containment failure — the chassis used to
  // report it as one, which sent people hunting a security problem.
  'parent-is-a-file':
    'The folder configured for packs is a file, so nothing can be written inside it. Nothing was created.',
  'too-deep':
    'The folder configured for packs is nested too deeply for this desk to write into. Nothing was created.',
  symlink:
    'The folder configured for packs is reached through a shortcut this desk does not write through. Nothing was created.',
  'outside-root': 'That location is outside this project. Nothing was created.',
  exists: 'Something is already there under that name — try another.',
  stale: 'Something else changed that file while this was open. Nothing was created.',
  'too-large': 'The pack this template would create is too large to write. Nothing was created.',
  'not-utf8': 'That file is not text this desk can read. Nothing was created.',
  'not-a-file': 'Something that is not a file is in the way. Nothing was created.',
  unauthorized:
    'This desk’s session is no longer accepted — reload the page. Nothing was created.',
  forbidden: 'This desk was not allowed to write there. Nothing was created.',
  'bad-request': 'This desk sent a request the chassis could not read. Nothing was created.',
  'staging-file':
    'That name is one this desk reserves for its own temporary files — try another. Nothing was created.',
  'excluded-directory':
    'The folder configured for packs is one this desk never reads or writes. Nothing was created.',
  internal: 'The chassis could not complete the write. Nothing was created.'
}

/**
 * The code a failure carries, whatever kind of failure it is.
 *
 * `StaleWrite` is its own type because a 409 is not an error in the ordinary
 * sense, and it carries the same code member; a `FileRequestError` carries one
 * where the chassis sent one. Anything else — a transport failure, a bug — has
 * none, and saying so is better than guessing at one.
 */
export function codeOf(cause: unknown): string | undefined {
  if (cause instanceof StaleWrite) return cause.code
  if (cause instanceof FileRequestError) return cause.code
  return undefined
}

/**
 * The Create-specific sentence for a refusal, or undefined where there is none.
 *
 * Undefined is a real answer and the caller must keep its own lead for it: a
 * code this desk has never seen is a refusal it cannot describe, and inventing
 * a description would be worse than the general sentence the caller already
 * has.
 */
export function refusalLead(cause: unknown): string | undefined {
  const code = codeOf(cause)
  return code === undefined ? undefined : CREATE_REFUSALS[code]
}

/**
 * The technical detail, for the line under the sentence.
 *
 * The chassis' own words, verbatim, and never promoted into the main text: a
 * diagnostic is worth keeping and worth keeping *somewhere else*.
 */
export function refusalDetail(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}
