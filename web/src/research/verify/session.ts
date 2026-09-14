/**
 * The consumer's verification of one session (gateway SPEC.md §1.4, §4 and
 * §5a), run by the page over what it holds: the receipts it was handed on
 * each `/acquire`, the results beside them, and the registry fetched from the
 * gateway, under the public key the desk-level file pinned.
 *
 * **Session-scoped, deliberately, and said so.** §5a.1 makes a store-wide
 * verdict the default and a session-scoped one "a choice with a name, made
 * deliberately in the consumer's code". This desk is a live consumer that
 * holds exactly the session its own run produced — it has no store snapshot
 * of anybody else's sessions to grade — so the verdict is about that session:
 * every receipt in it passes the §1.4 ladder, its sequence and chain hold, the
 * registry the key holder served carries a seal for it under the pinned key,
 * and the seal's count is the count held. Everything §4 says about other
 * sessions, sealed sessions absent from the store, decision records and
 * action citations is outside what this verifier can see, and an action
 * receipt in the held session is withheld rather than half-checked.
 *
 * **What a passing verdict means, and does not.** The bytes the page holds
 * are the bytes the gateway signed, in the order it signed them, sealed at
 * that count by the key holder: integrity and lineage within the gateway's
 * own bounds (§5). It says nothing about whether a page's content is true,
 * current, legally authoritative, or came from the site its URL names — the
 * adapter's `peerIdentity` names the provider the gateway spoke to, not the
 * page's origin.
 */
import {
  CanonError,
  canonicalize,
  memberOf,
  parseJsonText,
  stringMember,
  type JsonNode
} from './canon'
import { keyIdOf, receiptSigningInput, sealSigningInput, sha256Hex, verifyEd25519 } from './receipt'

export interface HeldReceipt {
  /** The receipt as the acquire response carried it. */
  receipt: JsonNode
  /** The result beside it, or null where the page holds none. */
  result: JsonNode | null
}

export interface Finding {
  sessionId: string
  /** The receipt's index, or null for a session-level finding. */
  callIndex: number | null
  status: string
  /** For `malformed`: the position the receipt was held at, as the file it would be. */
  file?: string
}

export interface SessionVerdict {
  ok: boolean
  findings: Finding[]
  /** Whether the registry carried a loadable seal for the session. */
  sealed: boolean
  /** The pinned key's id, which every receipt and seal must name. */
  keyId: string
}

export interface SessionInput {
  sessionId: string
  authority: string
  /** The pinned Ed25519 public key, 64 lowercase hex characters. */
  publicKeyHex: string
  /** The receipts in the order they were held; position is the expected callIndex. */
  receipts: HeldReceipt[]
  /** The registry as the gateway served it: one seal per line. */
  registryText: string
}

const DIGEST = /^sha256:[0-9a-f]{64}$/
const SIGNATURE_3 = /^[0-9a-f]{128}$/
const HEX = /^[0-9a-fA-F]+$/
const HMAC_DIGEST = /^hmac-sha256:[0-9a-fA-F]+$/
const SESSION_TOKEN = /^[A-Za-z0-9._-]{1,128}$/
const SHAPES = new Set(['airbyte', 'mcp', 'http', 'command'])

type Members = Map<string, JsonNode>

function membersOf(node: JsonNode): Members | undefined {
  if (node.kind !== 'object') return undefined
  return new Map(node.members.map((member) => [member.name, member.value]))
}

function isString(node: JsonNode | undefined): node is JsonNode & { kind: 'string' } {
  return node?.kind === 'string'
}

function isNullOrString(node: JsonNode | undefined): boolean {
  return node !== undefined && (node.kind === 'null' || node.kind === 'string')
}

function isDigest(node: JsonNode | undefined): boolean {
  return isString(node) && DIGEST.test(node.value)
}

function isNullOrDigest(node: JsonNode | undefined): boolean {
  return node !== undefined && (node.kind === 'null' || isDigest(node))
}

function isInteger(node: JsonNode | undefined): node is JsonNode & { kind: 'number' } {
  return node?.kind === 'number' && /^-?(0|[1-9][0-9]*)$/.test(node.literal)
}

/** The structural conditions of §1.2a's `acquisition` object. */
function acquisitionMalformed(node: JsonNode | undefined): boolean {
  const a = node === undefined ? undefined : membersOf(node)
  if (!a) return true
  const adapter = a.get('adapter') === undefined ? undefined : membersOf(a.get('adapter')!)
  if (!adapter || !isString(adapter.get('name')) || !isString(adapter.get('version')) || !isDigest(adapter.get('digest'))) {
    return true
  }
  const shape = a.get('shape')
  if (!isString(shape) || !SHAPES.has(shape.value)) return true
  if (!isNullOrString(a.get('endpoint')) || !isNullOrDigest(a.get('statement')) || !isNullOrString(a.get('snapshot'))) {
    return true
  }
  if (!isNullOrString(a.get('peerIdentity')) || !isNullOrDigest(a.get('schema')) || !isNullOrString(a.get('upstreamToken'))) {
    return true
  }
  if (!isString(a.get('observedAt'))) return true
  const pageItems = a.get('pageItems')
  if (pageItems !== undefined && (pageItems.kind !== 'array' || !pageItems.items.every(isDigest))) return true
  return false
}

/** The structural conditions of §1.2a's `action` object. */
function actionMalformed(node: JsonNode | undefined): boolean {
  const a = node === undefined ? undefined : membersOf(node)
  if (!a) return true
  const requester = a.get('requester') === undefined ? undefined : membersOf(a.get('requester')!)
  if (!requester || !isString(requester.get('issuer')) || !isString(requester.get('subject')) || !isDigest(requester.get('tokenDigest'))) {
    return true
  }
  const decision = a.get('decision') === undefined ? undefined : membersOf(a.get('decision')!)
  if (!decision || !isDigest(decision.get('recordDigest')) || !isDigest(decision.get('packDigest'))) return true
  const cites = a.get('cites')
  if (cites?.kind !== 'array') return true
  for (const cite of cites.items) {
    const c = membersOf(cite)
    const sessionId = c?.get('sessionId')
    const signature = c?.get('signature')
    if (!c || !isString(sessionId) || !SESSION_TOKEN.test(sessionId.value) || sessionId.value === '.' || sessionId.value === '..') return true
    if (!isInteger(c.get('callIndex')) || c.get('callIndex')!.kind !== 'number' || (c.get('callIndex') as { literal: string }).literal.startsWith('-')) return true
    if (!isString(signature) || !SIGNATURE_3.test(signature.value)) return true
  }
  const tool = a.get('tool') === undefined ? undefined : membersOf(a.get('tool')!)
  if (!tool || !isString(tool.get('shape')) || !isNullOrString(tool.get('endpoint')) || !isString(tool.get('name'))) return true
  if (!isDigest(a.get('request'))) return true
  const adapter = a.get('adapter') === undefined ? undefined : membersOf(a.get('adapter')!)
  if (!adapter || !isString(adapter.get('name')) || !isString(adapter.get('version')) || !isDigest(adapter.get('digest'))) {
    return true
  }
  return !isString(a.get('observedAt'))
}

/**
 * Order 1 of the ladder: the structural conditions of §1.2 or §1.2a for the
 * receipt's version, and the generic ones for any version. Returns the version
 * where the receipt is well-formed, or `'malformed'` / `'unsupported-version'`.
 */
function shapeOf(receipt: JsonNode): '2' | '3' | 'malformed' | 'unsupported-version' {
  const r = membersOf(receipt)
  if (!r) return 'malformed'
  const signature = r.get('signature')
  if (!isString(signature) || !HEX.test(signature.value) || signature.value.length === 0) return 'malformed'
  const callIndex = r.get('callIndex')
  if (!isInteger(callIndex) || callIndex.literal.startsWith('-')) return 'malformed'
  if (!isDigest(r.get('resultDigest'))) return 'malformed'
  const version = r.get('receiptVersion')
  if (!isString(version)) return 'malformed'
  const common = ['sessionId', 'source', 'servedAt', 'authority', 'keyId']
  if (common.some((name) => !isString(r.get(name)))) return 'malformed'
  const sessionId = (r.get('sessionId') as { value: string }).value
  if (!SESSION_TOKEN.test(sessionId) || sessionId === '.' || sessionId === '..') return 'malformed'
  if (!isNullOrString(r.get('prevSignature'))) return 'malformed'
  if (version.value === '2') {
    const digest = r.get('argumentsDigest')
    if (!isString(digest) || !HMAC_DIGEST.test(digest.value)) return 'malformed'
    return '2'
  }
  if (version.value !== '3') return 'unsupported-version'
  if (!SIGNATURE_3.test(signature.value)) return 'malformed'
  if (r.has('argumentsDigest')) return 'malformed'
  if (!isDigest(r.get('argumentsCommitment'))) return 'malformed'
  const caller = r.get('caller')
  if (caller === undefined) return 'malformed'
  if (caller.kind !== 'null') {
    const c = membersOf(caller)
    if (!c || !isString(c.get('issuer')) || !isString(c.get('subject')) || !isDigest(c.get('tokenDigest'))) return 'malformed'
  }
  const kind = r.get('kind')
  if (!isString(kind)) return 'malformed'
  if (kind.value === 'acquisition') {
    if (r.has('action') || acquisitionMalformed(r.get('acquisition'))) return 'malformed'
  } else if (kind.value === 'action') {
    if (r.has('acquisition') || actionMalformed(r.get('action'))) return 'malformed'
  } else {
    return 'malformed'
  }
  return '3'
}

interface Passed {
  index: number
  callIndex: number
  version: '2' | '3'
  signature: string
  prevSignature: string | null
}

/** Verify the held session. Never throws for a defect in what is held. */
export async function verifySession(input: SessionInput): Promise<SessionVerdict> {
  const findings: Finding[] = []
  const keyId = await keyIdOf(input.publicKeyHex)
  const passed: Passed[] = []
  const held = input.receipts.length
  const say = (callIndex: number | null, status: string, file?: string) => {
    findings.push(
      file === undefined
        ? { sessionId: input.sessionId, callIndex, status }
        : { sessionId: input.sessionId, callIndex, status, file }
    )
  }
  for (let index = 0; index < held; index += 1) {
    const { receipt, result } = input.receipts[index]!
    const shape = shapeOf(receipt)
    if (shape === 'malformed') {
      say(null, 'malformed', `${index}.json`)
      continue
    }
    if (shape === 'unsupported-version') {
      say(index, 'unsupported-version')
      continue
    }
    const r = membersOf(receipt)!
    const callIndex = Number((r.get('callIndex') as { literal: string }).literal)
    if ((r.get('keyId') as { value: string }).value !== keyId) {
      say(callIndex, 'key-mismatch')
      continue
    }
    const signature = (r.get('signature') as { value: string }).value
    let input3: Uint8Array
    try {
      input3 = receiptSigningInput(receipt, shape)
    } catch {
      say(null, 'malformed', `${index}.json`)
      continue
    }
    if (!(await verifyEd25519(input.publicKeyHex, input3, signature.toLowerCase()))) {
      say(callIndex, 'signature-mismatch')
      continue
    }
    if (callIndex !== index || (r.get('sessionId') as { value: string }).value !== input.sessionId) {
      say(callIndex, 'misfiled')
      continue
    }
    if ((r.get('authority') as { value: string }).value !== input.authority) {
      say(callIndex, 'authority-mismatch')
      continue
    }
    const kind = stringMember(receipt, 'kind')
    if (kind === 'action') {
      // §4 steps 5 and 6 resolve an action's citations and decision record
      // against a store and a record directory this consumer does not hold,
      // and its artifact is the target's raw answer rather than a canonical
      // value. Withheld rather than half-checked, under a status of this
      // consumer's own.
      say(callIndex, 'action-unchecked')
      continue
    }
    if (result === null) {
      say(callIndex, 'artifact-missing')
      continue
    }
    let digest: string
    try {
      digest = 'sha256:' + (await sha256Hex(canonicalize(result)))
    } catch (cause) {
      if (!(cause instanceof CanonError)) throw cause
      say(callIndex, 'artifact-mismatch')
      continue
    }
    if (digest !== (r.get('resultDigest') as { value: string }).value) {
      say(callIndex, 'artifact-mismatch')
      continue
    }
    say(callIndex, 'ok')
    const prev = memberOf(receipt, 'prevSignature')
    passed.push({
      index,
      callIndex,
      version: shape,
      signature,
      prevSignature: prev?.kind === 'string' ? prev.value : null
    })
  }
  // Per session, over the receipts that passed: the sequence, then the chain.
  const indices = passed.map((p) => p.callIndex).sort((a, b) => a - b)
  const contiguous = indices.every((value, at) => value === at)
  if (!contiguous) {
    say(null, 'sequence-broken')
  } else if (passed.length > 0) {
    const head = passed[0]!
    for (let at = 0; at < passed.length; at += 1) {
      const current = passed[at]!
      const expectedPrev = at === 0 ? null : passed[at - 1]!.signature
      if (current.prevSignature !== expectedPrev || current.version !== head.version) {
        say(null, 'chain-broken')
        break
      }
    }
  }
  // The registry, from the key holder: a seal counts only under the pinned
  // key and its own signature; the first loadable seal for the session wins.
  let sealed = false
  let finalCount: number | null = null
  for (const line of input.registryText.split('\n')) {
    if (line.trim() === '') continue
    let seal: JsonNode
    try {
      seal = parseJsonText(line)
    } catch {
      continue
    }
    if (stringMember(seal, 'sessionId') !== input.sessionId) continue
    if (stringMember(seal, 'keyId') !== keyId) continue
    const count = memberOf(seal, 'finalCount')
    const signature = stringMember(seal, 'signature')
    if (!isInteger(count) || signature === undefined) continue
    let sealInput: Uint8Array
    try {
      sealInput = sealSigningInput(seal)
    } catch {
      continue
    }
    if (!(await verifyEd25519(input.publicKeyHex, sealInput, signature.toLowerCase()))) continue
    sealed = true
    finalCount = Number(count.literal)
    break
  }
  if (!sealed) {
    say(null, 'unregistered-session')
  } else if (held < finalCount!) {
    say(null, 'tail-rollback')
  } else if (held > finalCount!) {
    say(null, 'count-exceeds-seal')
  }
  const ok = held > 0 && findings.every((finding) => finding.status === 'ok')
  return { ok, findings, sealed, keyId }
}

/** The state a source's receipt is in, as the ledger records it. */
export type VerificationState =
  | { state: 'unchecked' }
  | { state: 'verified'; at: string; keyId: string }
  | { state: 'failed'; at: string; findings: Finding[] }
