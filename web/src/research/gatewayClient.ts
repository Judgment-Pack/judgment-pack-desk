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
  result: JsonNode
  receipt: JsonNode
  /** The commitments' salts, `args` always and `statement` where committed. */
  salts: Record<string, string>
}

async function bodyText(response: Response): Promise<string> {
  const bytes = new Uint8Array(await response.arrayBuffer())
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}

async function refusal(response: Response): Promise<GatewayError> {
  let text = ''
  try {
    text = await bodyText(response)
  } catch {
    return new GatewayError(response.status, `the gateway answered ${response.status} with text that is not UTF-8`)
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

/** One `/acquire`: the source named, the canonical arguments as given. */
export async function acquire(
  session: string,
  source: string,
  args: unknown,
  signal?: AbortSignal
): Promise<Acquired> {
  const response = await deskFetch(chassisUrl(`${RELAY}/acquire`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ session, source, arguments: args }),
    signal
  })
  if (!response.ok) throw await refusal(response)
  const text = await bodyText(response)
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
  return { text, result, receipt, salts: saltMap }
}

/** One `/seal`: the session's final count, sealed under the gateway's key. */
export async function seal(session: string, signal?: AbortSignal): Promise<JsonNode> {
  const response = await deskFetch(chassisUrl(`${RELAY}/seal`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ session }),
    signal
  })
  if (!response.ok) throw await refusal(response)
  return parseJsonText(await bodyText(response))
}

/** The registry, one seal per line, fetched from the key holder. */
export async function registry(signal?: AbortSignal): Promise<string> {
  const response = await deskFetch(chassisUrl(`${RELAY}/registry`), { method: 'GET', signal })
  if (!response.ok) throw await refusal(response)
  return bodyText(response)
}

/** A flat session token (gateway SPEC.md §3a) for one authoring run. */
export function newResearchSession(now = new Date()): string {
  const random = new Uint8Array(6)
  crypto.getRandomValues(random)
  const hex = Array.from(random, (byte) => byte.toString(16).padStart(2, '0')).join('')
  const stamp = now.toISOString().replace(/[-:.]/g, '').slice(0, 15)
  return `desk-${stamp}-${hex}`
}
