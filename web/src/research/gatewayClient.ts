/**
 * The page's calls to the configured judgment-pack gateway, through the
 * chassis' research relay (`internal/desk/researchrelay.go`): an acquire per
 * source call, a seal when the run's session closes, and the registry a
 * consumer verifies against.
 *
 * **The answer is read as bytes and decoded strictly**, then parsed with the
 * verifier's own parser rather than `JSON.parse`: a receipt is verified over
 * its canonical bytes, and a parser that folds `1.0` into `1` or keeps the
 * last of two members named alike would verify a text the gateway never
 * signed. What comes back is the response text and the parsed nodes.
 */
import { chassisUrl, deskFetch } from '../files/client'
import { memberOf, parseJsonText, type JsonNode } from './verify/canon'

const RELAY = '/api/research/gateway'
type GatewayConstraint = 'local-documents'
const constraintHeaders = (constraint?: GatewayConstraint): Record<string, string> => constraint ? { 'X-JPack-Local-Documents': '1' } : {}

/** A refusal from the relay or from the gateway, with the status and the sentence. */
export class GatewayError extends Error {
  constructor(
    readonly status: number,
    message: string,
    /** The chassis' refusal code where the chassis authored the refusal. */
    readonly code?: string
  ) {
    super(message)
    this.name = 'GatewayError'
  }
}

export interface Acquired {
  /** The response text, as received: what the ledger keeps. */
  text: string
  /** Its length in bytes on the wire, which is what a byte budget counts. */
  bytes: number
  result: JsonNode
  receipt: JsonNode
  /** The commitments' salts, `args` always and `statement` where committed. */
  salts: Record<string, string>
}

/** What a registry or a refusal may be, in bytes: neither is a page. */
export const MAX_REGISTRY_BYTES = 4 << 20
export const MAX_REFUSAL_BYTES = 64 << 10

/** An answer past the bytes the caller may still take, cut at the wire. */
export class OverBudget extends Error {
  constructor(readonly limit: number) {
    super(`the answer is past the ${limit} bytes this run may still retrieve; it was cut off and nothing of it is kept`)
    this.name = 'OverBudget'
  }
}

/**
 * Read a body **counting bytes as they arrive and stopping at the limit**,
 * rather than taking the whole answer and measuring it afterwards: a budget
 * that is checked before a request and not during it bounds nothing about
 * what the request brings back. The stream is cancelled past the limit, so
 * the rest is never allocated.
 */
export async function readBounded(response: Response, limit: number): Promise<Uint8Array> {
  if (response.body === null) return new Uint8Array()
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      total += value.byteLength
      if (total > limit) {
        // Cancellation is started and not waited for: a source whose cancel
        // never settles would otherwise hold the refusal, and the refusal is
        // the point.
        void reader.cancel().catch(() => {})
        throw new OverBudget(limit)
      }
      chunks.push(value)
    }
  } finally {
    reader.releaseLock()
  }
  const out = new Uint8Array(total)
  let at = 0
  for (const chunk of chunks) {
    out.set(chunk, at)
    at += chunk.byteLength
  }
  return out
}

async function bodyText(response: Response, limit: number): Promise<string> {
  return new TextDecoder('utf-8', { fatal: true }).decode(await readBounded(response, limit))
}

async function refusal(response: Response): Promise<GatewayError> {
  let text = ''
  try {
    text = await bodyText(response, MAX_REFUSAL_BYTES)
  } catch {
    return new GatewayError(response.status, `the gateway answered ${response.status} with text that is not UTF-8, or past ${MAX_REFUSAL_BYTES} bytes`)
  }
  try {
    const parsed = JSON.parse(text) as { error?: unknown; code?: unknown }
    if (typeof parsed.error === 'string') {
      return new GatewayError(response.status, parsed.error, typeof parsed.code === 'string' ? parsed.code : undefined)
    }
  } catch {
    // Not an envelope: the status line is the sentence.
  }
  return new GatewayError(response.status, `the gateway answered ${response.status} ${response.statusText}`)
}

/**
 * One `/acquire`: the source named, the canonical arguments as given, and
 * the bytes the caller may still take, past which the answer is cut at the
 * wire and refused as `OverBudget`.
 */
export async function acquire(
  session: string,
  source: string,
  args: unknown,
  limit: number,
  signal?: AbortSignal,
  constraint?: GatewayConstraint
): Promise<Acquired> {
  const response = await deskFetch(chassisUrl(`${RELAY}/acquire`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json', ...constraintHeaders(constraint) },
    body: JSON.stringify({ session, source, arguments: args }),
    signal
  })
  if (!response.ok) throw await refusal(response)
  const raw = await readBounded(response, limit)
  const text = new TextDecoder('utf-8', { fatal: true }).decode(raw)
  const parsed = parseJsonText(text)
  const result = memberOf(parsed, 'result')
  const receipt = memberOf(parsed, 'receipt')
  const salts = memberOf(parsed, 'salts')
  if (result === undefined || receipt === undefined) {
    throw new GatewayError(response.status, 'the gateway answered without a result and a receipt')
  }
  const saltMap: Record<string, string> = {}
  if (salts?.kind === 'object') {
    for (const member of salts.members) {
      if (member.value.kind === 'string') saltMap[member.name] = member.value.value
    }
  }
  return { text, bytes: raw.byteLength, result, receipt, salts: saltMap }
}

/** One `/seal`: the session's final count, sealed under the gateway's key. */
export async function seal(session: string, signal?: AbortSignal, constraint?: GatewayConstraint): Promise<JsonNode> {
  const response = await deskFetch(chassisUrl(`${RELAY}/seal`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json', ...constraintHeaders(constraint) },
    body: JSON.stringify({ session }),
    signal
  })
  if (!response.ok) throw await refusal(response)
  return parseJsonText(await bodyText(response, MAX_REFUSAL_BYTES))
}

/** The registry, one seal per line, fetched from the key holder. */
export async function registry(signal?: AbortSignal, constraint?: GatewayConstraint): Promise<string> {
  const response = await deskFetch(chassisUrl(`${RELAY}/registry`), { method: 'GET', headers: constraintHeaders(constraint), signal })
  if (!response.ok) throw await refusal(response)
  return bodyText(response, MAX_REGISTRY_BYTES)
}

/** A flat session token (gateway SPEC.md §3a) for one authoring run. */
export function newResearchSession(now = new Date()): string {
  const random = new Uint8Array(6)
  crypto.getRandomValues(random)
  const hex = Array.from(random, (byte) => byte.toString(16).padStart(2, '0')).join('')
  const stamp = now.toISOString().replace(/[-:.]/g, '').slice(0, 15)
  return `desk-${stamp}-${hex}`
}
