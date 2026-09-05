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
import { answer, chassisUrl } from '../files/client'

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
  return answer<AssistantKeyState>(await fetch(chassisUrl('/api/assistant/key'), { signal }))
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
    await fetch(chassisUrl('/api/assistant/key'), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key })
    })
  )
}

export async function removeAssistantKey(): Promise<AssistantKeyState> {
  return answer<AssistantKeyState>(
    await fetch(chassisUrl('/api/assistant/key'), { method: 'DELETE' })
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
    await fetch(chassisUrl('/api/assistant/probe'), { method: 'POST', signal })
  )
}
