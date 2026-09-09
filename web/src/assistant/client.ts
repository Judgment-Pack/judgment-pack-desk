/**
 * The assistant slot's four chassis calls.
 *
 * **Nothing here ever holds a key that came back from the desk, because
 * nothing ever comes back.** The store sends one and is answered with a
 * fingerprint; the read is answered with a fingerprint; the probe is answered
 * with a status and a sentence. There is no call that returns the value, and
 * that is the whole custody argument in one paragraph: a page that could read
 * the key is a page that could be made to send it somewhere.
 *
 * These are kept apart from `files/client.ts` for the same reason the file
 * queries are kept apart from the MCP ones — the chassis' file API moves the
 * project's bytes, and this moves nothing of the project's at all — while
 * sharing that module's URL builder and refusal envelope, because the token
 * and the `{error, code}` shape are the chassis' and not any one endpoint's.
 */
import { answer, chassisUrl, deskFetch } from '../files/client'
import type { AssistantConfig } from '../config/deskConfig'

/**
 * What the desk will say about the key, and the whole of it.
 *
 * `fingerprint` is four characters from each end, and empty in two different
 * cases: no key at all, and a key too short to fingerprint without disclosing
 * it. `present` tells those apart, which is why it is a separate member rather
 * than something a caller infers from an empty string.
 */
export interface AssistantKeyState {
  present: boolean
  fingerprint: string
  /**
   * The destination this key was entered for: the scheme and host of the
   * endpoint configured when it was stored, and that endpoint's wire protocol.
   * Empty where there is no key.
   *
   * **The key travels only there.** A configuration write can move the
   * endpoint — that is what it is for — and the credential does not follow:
   * the probe and the relay refuse with `assistant-key-unbound` rather than
   * presenting it somewhere new, and the repair is a person entering it again,
   * which page code cannot do because it has never held it. These two members
   * are what lets a form say "key stored for gw.example" instead of leaving a
   * reader to discover the binding by meeting a refusal. Neither is a secret:
   * both are in the file this page already reads.
   */
  origin: string
  kind: string
  /**
   * The origin of the endpoint this desk is configured for **now**, and this
   * desk's own verdict about whether the stored key would be presented to it.
   *
   * **Computed by the chassis, never by this page**, and the reason is a
   * measured disagreement rather than a preference: the browser's `URL` drops
   * an explicit `:443` where Go's `url.Parse` keeps it, so a key stored for a
   * host and a configuration naming the same host with its default port
   * written out read as bound here while the relay answered
   * `assistant-key-unbound` and sent nothing. Two implementations of one rule
   * is one too many, and the one that decides has to be the one that presents
   * the credential. `configuredOrigin` is empty where no endpoint is
   * configured or the configured URL has no origin to take.
   */
  configuredOrigin: string
  /** That endpoint's wire protocol, empty alongside an empty origin. */
  configuredKind: string
  bound: boolean
}

/**
 * The words a probe may answer with, and the whole of them.
 *
 * **Nothing the endpoint wrote is repeated to anybody.** The probe used to
 * quote the endpoint's own error sentence with the key substituted out of it,
 * which is a categorical promise ("the key is never sent back to the browser")
 * held by one `replaceAll`: a body under the endpoint's control can carry a
 * *derived* representation of the credential — base64, percent-encoded,
 * JSON-escaped, hex, or half of it — that no substitution reliably finds. So
 * the body is discarded at the desk and one of these travels instead.
 *
 * The cost is real and accepted: a reader debugging a misconfigured gateway no
 * longer sees its sentence and must look at the endpoint's own logs.
 *
 * Held identical to `AssistantDiagnostics` in `internal/desk/assistant.go` by
 * a test that reads that declaration.
 */
export const PROBE_DIAGNOSTICS = [
  'unauthorized',
  'forbidden',
  'not-found',
  'timeout',
  'tls',
  'refused',
  'dns',
  'unexpected-status'
] as const
export type ProbeDiagnostic = (typeof PROBE_DIAGNOSTICS)[number]

/**
 * What each of those words means, in plain English.
 *
 * A lookup rather than the word itself, because `unexpected-status` is not a
 * sentence and `tls` is not English. It sits beside the vocabulary rather than
 * in the section that first rendered it, because the model listing reports a
 * refusal in the same words: two tables of one vocabulary drift, and the
 * page's copy is what turns a word into a sentence a reader sees. A word
 * outside the list has no entry, and a caller renders the word it was given
 * rather than a blank — the desk does not invent a meaning for something it
 * did not define.
 */
export const DIAGNOSTIC_SAYS: Record<string, string> = {
  unauthorized: 'the endpoint did not accept the key',
  forbidden: 'the endpoint refused this request',
  'not-found': 'nothing is at that address',
  timeout: 'no answer within ten seconds',
  tls: 'the secure connection could not be established',
  refused: 'nothing is listening there',
  dns: 'that host name did not resolve',
  'unexpected-status': 'the endpoint answered something unexpected'
}

/** What one reachability check established. */
export interface ProbeResult {
  /**
   * The endpoint answered this request successfully — not merely that a socket
   * opened. A 401 is a host that is there and a credential it will not take.
   */
  reachable: boolean
  /** The HTTP status, or 0 where no response arrived at all. */
  status: number
  latencyMs: number
  /**
   * One word from `PROBE_DIAGNOSTICS`, or empty on a success. Never text the
   * endpoint wrote.
   */
  diagnostic: string
}

export async function readAssistantKey(signal?: AbortSignal): Promise<AssistantKeyState> {
  return answer<AssistantKeyState>(await deskFetch(chassisUrl('/api/assistant/key'), { signal }))
}

/**
 * Store one key on this machine.
 *
 * The only write Admin makes, and the reason it exists is that the alternative
 * is worse: a key has to live somewhere, and the somewhere it must not live is
 * a file in a shared checkout. So it does not go through the file API — which
 * writes only inside the project — and gets this instead.
 */
export async function storeAssistantKey(key: string): Promise<AssistantKeyState> {
  return answer<AssistantKeyState>(
    await deskFetch(chassisUrl('/api/assistant/key'), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key })
    })
  )
}

export async function removeAssistantKey(): Promise<AssistantKeyState> {
  return answer<AssistantKeyState>(
    await deskFetch(chassisUrl('/api/assistant/key'), { method: 'DELETE' })
  )
}

/**
 * Ask the chassis to reach the configured endpoint.
 *
 * **The request carries no destination**, and that omission is deliberate. If
 * this sent a URL, anything holding the session token could point the chassis
 * — and the key it holds — at a host of its choosing. The destination comes
 * from the desk-level file on that machine instead, so a request body cannot
 * move it.
 */
export async function probeAssistantEndpoint(signal?: AbortSignal): Promise<ProbeResult> {
  return answer<ProbeResult>(
    await deskFetch(chassisUrl('/api/assistant/probe'), { method: 'POST', signal })
  )
}

/**
 * What a desk-level write asks for, and what it must send to be allowed.
 *
 * **`ifMatch` is the digest of the `desk.json` bytes this page last read**,
 * bare hex, and the empty string means "I believe there is no file". The
 * chassis refuses the write where that disagrees with the disk, and there is
 * no `override`: this is the one file that names the endpoint a credential is
 * presented to, and "write anyway" is not a choice a page should be able to
 * make about it. The repair is to read it again and decide about what is
 * actually there.
 */
export interface AssistantConfigWrite {
  /** The `assistant` object exactly as the decoder accepts it. */
  assistant: unknown
  ifMatch: string
}

/** What the chassis answers a desk-level write with. */
export interface AssistantConfigWritten {
  /** Absolute, on that machine. */
  path: string
  /** The digest of the bytes that landed, for the next write's `ifMatch`. */
  sha256: string
  /** The `assistant` member of the file on disk, read back after the write. */
  assistant: AssistantConfig
  /** True exactly where the write brought the file into existence. */
  created: boolean
  /**
   * True where a key is stored on this machine and is **not** the key for the
   * endpoint this write just configured.
   *
   * The write moves the endpoint and never the credential; this is how a page
   * learns that without having to make a request that fails.
   */
  keyRebindRequired: boolean
}

/**
 * Replace the `assistant` object in the desk-level file.
 *
 * **The one configuration write this page makes, and it names no path.** The
 * chassis writes exactly one file — the one on that machine, through the same
 * pinned directory the key is written through — carries every other member of
 * it across untouched, and decodes the bytes it composed before any of them
 * reach the disk. So a page cannot store a configuration Admin would then
 * report as refused, and cannot store one anywhere else.
 *
 * A refusal arrives as a `FileRequestError` carrying the chassis' code:
 * `desk-config-refused` (422) for an object the shared decoder will not accept,
 * with the problems in the body, and `desk-config-changed` (409) — a
 * `StaleWrite`, with both digests — for a file that moved underneath this page.
 */
export async function updateAssistantConfig(
  input: AssistantConfigWrite
): Promise<AssistantConfigWritten> {
  return answer<AssistantConfigWritten>(
    await deskFetch(chassisUrl('/api/desk-config'), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ assistant: input.assistant, ifMatch: input.ifMatch })
    })
  )
}
