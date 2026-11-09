import { sourceMessage } from '../i18n/source'
/** Fixed server metadata action. The server selects the saved destination and
 * first listing page, guards the current connection and scans the bounded reply.
 * This reader retains protocol parsing and never receives a model-call capability.
 */
import { modelIdProblem, type EndpointKind } from '../config/deskConfig'
import { DIAGNOSTIC_SAYS, type ProbeDiagnostic } from './client'
import { chassisUrl, deskFetch } from '../files/client'
import { aiHeaders } from './aiConnections'

/**
 * The listing path each protocol documents, **relative to the configured
 * base**, exactly as the probe and the README have it.
 *
 * The `v1` in the relay's mount point belongs to that route and not to any
 * endpoint, which is how one mount point serves three protocols: what is
 * appended here lands after the configured base, unchanged.
 */
export const LISTING_SUFFIX: Readonly<Record<EndpointKind, string>> = {
  'openai-compatible': 'models',
  anthropic: 'v1/models',
  gemini: 'v1beta/models'
}

/** One model the endpoint offers: what to save, and what to show. */
export interface ModelRow {
  /** The id exactly as the endpoint spells it. This is what is saved. */
  id: string
  /** What a person reads. Never saved, and never sent anywhere. */
  label: string
}

/**
 * The sentence a listing that answered with something else carries.
 *
 * Fixed, because the alternative is `JSON.parse`'s own message — which quotes
 * the text it failed on, and that text is a body this desk has promised not to
 * repeat.
 */
export const NOT_A_LISTING =
  sourceMessage("the endpoint answered something this desk could not read as a model listing")

/**
 * A refusal, as its status and one word from the probe's vocabulary.
 *
 * **Neither half is attributed**, and that is deliberate rather than vague.
 * This route carries the desk's own refusals — no key, a key bound elsewhere,
 * too many requests in flight — and the endpoint's answers, on the same
 * statuses; telling them apart would mean reading a body, which is the one
 * thing the probe's ruling forbids. So the line says what came back and does
 * not claim who said it, and the key row above says whether the desk had a
 * reason of its own.
 */
export function listingRefusal(status: number): string {
  const says = SAYS[status] ?? DIAGNOSTIC_SAYS[diagnosticFor(status)]
  return sourceMessage("the model listing was refused — answered {{value0}}, {{message0}}", { value0: status, message0: says })
}

/**
 * The two statuses this desk's own refusals arrive on, said without claiming
 * which of the two happened.
 *
 * A `502` on this route is either the endpoint never answering, or the desk
 * refusing to put what it answered on the page — the credential scan. The page
 * cannot tell them apart without reading a body, which is exactly what it will
 * not do, so it says what both have in common and leaves the desk's own log
 * and the key row to say more.
 */
const SAYS: Record<number, string> = {
  502:
    sourceMessage("either the endpoint never answered, or what it answered is something this desk will not put on the page"),
  503: sourceMessage("this desk is already carrying as many requests to the endpoint as it will")
}

function diagnosticFor(status: number): ProbeDiagnostic {
  if (status === 401) return 'unauthorized'
  if (status === 403) return 'forbidden'
  if (status === 404) return 'not-found'
  return 'unexpected-status'
}

/** Load only metadata from the current saved API connection. No retries. */
export async function listModels(
  kind: EndpointKind,
  connectionId?: string,
  connectionRevision?: string,
  signal?: AbortSignal
): Promise<ModelRow[]> {
  const answered = await deskFetch(chassisUrl('/api/assistant/models'), {
    method: 'POST', headers: aiHeaders(connectionId, connectionRevision), signal
  })
  if (!answered.ok) throw new Error(listingRefusal(answered.status))
  let body: unknown
  try { body = JSON.parse(await answered.text()) } catch { throw new Error(NOT_A_LISTING) }
  const items = body !== null && typeof body === 'object'
    ? (body as Record<string, unknown>)[kind === 'gemini' ? 'models' : 'data'] : undefined
  if (!Array.isArray(items)) throw new Error(NOT_A_LISTING)
  return modelRows(kind, body)
}

/**
 * One family's listing shape, read into rows.
 *
 * The three differ and are read apart rather than guessed at: Gemini answers
 * `models[]` with a `models/<id>` name, a `displayName` and the methods each
 * model supports; the other two answer `data[]` with an `id`, and Anthropic
 * adds a `display_name`.
 *
 * **An id is a row only if the configuration decoder would take it**, and the
 * rule is *imported* rather than restated: a copy is exactly how the picker
 * came to offer a whitespace-only id that produced a 422 the moment it was
 * saved — the file's reader trims and the copy did not. A row nobody can save
 * is not a row.
 *
 * **Gemini's rows are filtered to the models that can generate content.** That
 * listing carries embedding and other models an assistant cannot run on, and
 * offering one would be a picker that produces a session no request succeeds
 * in. It is the endpoint's own declaration doing the filtering, not a list of
 * names this desk keeps.
 */
export function modelRows(kind: EndpointKind, body: unknown): ModelRow[] {
  if (body === null || typeof body !== 'object') return []
  if (kind === 'gemini') {
    const models = (body as { models?: unknown }).models
    if (!Array.isArray(models)) return []
    const rows: ModelRow[] = []
    for (const entry of models) {
      if (entry === null || typeof entry !== 'object') continue
      const { name, displayName, supportedGenerationMethods } = entry as {
        name?: unknown
        displayName?: unknown
        supportedGenerationMethods?: unknown
      }
      if (typeof name !== 'string') continue
      if (
        !Array.isArray(supportedGenerationMethods) ||
        !supportedGenerationMethods.includes('generateContent')
      ) {
        continue
      }
      // **The rule decides, and the trim only normalises what it accepted.**
      // Trimming first would have made the rule unobservable on this branch —
      // `models/   ` reduces to the empty string, which any test for
      // emptiness also rejects — so the check would have been carried by the
      // trim while appearing to be carried by the shared rule. A mutation
      // check said so before a reader could.
      const raw = name.startsWith('models/') ? name.slice('models/'.length) : name
      if (modelIdProblem(raw) !== undefined) continue
      const id = raw.trim()
      rows.push({ id, label: typeof displayName === 'string' && displayName !== '' ? displayName : id })
    }
    return rows
  }
  const data = (body as { data?: unknown }).data
  if (!Array.isArray(data)) return []
  const rows: ModelRow[] = []
  for (const entry of data) {
    if (entry === null || typeof entry !== 'object') continue
    const { id: raw, display_name: shown } = entry as { id?: unknown; display_name?: unknown }
    if (modelIdProblem(raw) !== undefined) continue
    // Trimmed, and saved trimmed: what the decoder accepts is the trimmed
    // value, so an id offered untrimmed would be a picker showing one string
    // and writing another.
    const id = (raw as string).trim()
    rows.push({
      id,
      label: kind === 'anthropic' && typeof shown === 'string' && shown !== '' ? shown : id
    })
  }
  return rows
}
