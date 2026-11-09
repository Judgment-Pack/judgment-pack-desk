import { AIFailureError, failure } from '@judgment-pack/harness/failures'
import { parseJsonText } from '../../research/verify/canon'

export type SourceDecisionKind = 'approve' | 'revoke'
export interface SourceApproveRequest { readonly reviewId: string; readonly reviewRevision: 1 }
export interface SourceRevokeRequest { readonly approvalId: string; readonly approvalRevision: 1 }
export type SourceDecisionRequest = SourceApproveRequest | SourceRevokeRequest
const invalid = (): never => { throw new AIFailureError(failure('invalid-request', 'desk', 'not-started')) }

/** Exact selectors only. Neither a declaration nor scope can be client authority. */
export function readSourceDecisionRequest(kind: SourceDecisionKind, raw: string): SourceDecisionRequest {
  if (kind !== 'approve' && kind !== 'revoke' || new TextEncoder().encode(raw).length > 4096) return invalid()
  let node
  try { node = parseJsonText(raw) } catch { return invalid() }
  const idKey = kind === 'approve' ? 'reviewId' : 'approvalId'
  const revisionKey = kind === 'approve' ? 'reviewRevision' : 'approvalRevision'
  if (node.kind !== 'object' || node.members.length !== 2) return invalid()
  const id = node.members.find(member => member.name === idKey)?.value
  const revision = node.members.find(member => member.name === revisionKey)?.value
  // Match the Go uint64 reader: 1.0 and 1e0 are not integer wire literals.
  if (id?.kind !== 'string' || !/^[a-f0-9]{32}$/.test(id.value) || revision?.kind !== 'number' || revision.literal !== '1') return invalid()
  return kind === 'approve' ? {reviewId: id.value, reviewRevision: 1} : {approvalId: id.value, approvalRevision: 1}
}

/** Used before sending an explicit decision; validation cannot trigger a request. */
export function encodeSourceDecisionRequest(kind: SourceDecisionKind, value: unknown): string {
  let raw
  try { raw = JSON.stringify(value) } catch { return invalid() }
  if (raw === undefined) return invalid()
  return JSON.stringify(readSourceDecisionRequest(kind, raw))
}
