import { AIFailureError, failure } from '@judgment-pack/harness/failures'
import { parseJsonText, type JsonNode } from '../../research/verify/canon'

// Matches the existing private conversation/source-ledger response bound.
export const MAX_SOURCE_CATALOG_BYTES = 16 * 1024 * 1024
const adapters = {
  'desk-search': ['search_sources', 'discover', 'read-external', 'read', 'research', 'none'],
  'desk-link': ['read_link', 'read', 'read-external', 'read', 'read', 'none'],
  'desk-website': ['explore_website', 'discover', 'read-external', 'read', 'research', 'none'],
  'desk-research-workflow': ['request_dialect_research', 'act', 'act', 'act', '', 'required'],
} as const
export type SourceAdapterID = keyof typeof adapters
export type SourceReviewStatus = 'setup-required' | 'review-required' | 'approved' | 'changed' | 'revoked'
const configurationKeys = ['target', 'targetDigest', 'authorityDigest', 'publicKeyDigest', 'deskDigest', 'preferenceDigest', 'connection', 'revision', 'provider', 'bundleDigest', 'verifierDigest', 'gatewayBuildDigest', 'ownerDigest', 'catalogDigest', 'catalogOwnerDigest'] as const
export type SourceConfiguration = Readonly<Record<typeof configurationKeys[number], string>>
export interface SourceDeclaration {
  readonly name: string
  readonly description: string
  /** Keep number literals intact for the complete declaration review. */
  readonly inputSchema: JsonNode & {kind: 'object'}
  readonly taxonomy: {readonly operation: string; readonly effect: string}
  readonly effectClass: string
  readonly delegation: string
  readonly approval: string
}
export interface SourceSnapshot {
  readonly adapterId: SourceAdapterID
  readonly adapterVersion: '1'
  readonly kind: 'custom'
  readonly trust: 'first-party'
  readonly configurationDigest: string
  readonly toolListDigest: string
  readonly configuration: SourceConfiguration
  readonly completeDeclarations: readonly SourceDeclaration[]
  readonly status: 'setup-required' | 'review-required'
  readonly identity?: {readonly kind: 'connection'; readonly label: string}
  readonly identityDigest?: string
}
export interface SourceApprovalReference {
  readonly id: string
  readonly revision: 1 | 2
  readonly approvedAt: string
  readonly revokedAt?: string
}
export interface SourceReview {
  readonly snapshot: SourceSnapshot
  readonly status: SourceReviewStatus
  readonly reviewId?: string
  readonly reviewRevision?: 1
  readonly approval?: SourceApprovalReference
}
export interface SourceCatalog {readonly version: 1; readonly sources: readonly SourceReview[]}
export interface SourceRevocation {readonly version: 1; readonly status: 'revoked'; readonly approval: SourceApprovalReference}

const invalid = (): never => { throw new AIFailureError(failure('invalid-response', 'desk', 'unknown')) }
function members(node: JsonNode | undefined, required: readonly string[], optional: readonly string[] = []): Record<string, JsonNode> {
  if (node?.kind !== 'object' || node.members.some(m => !required.includes(m.name) && !optional.includes(m.name))) return invalid()
  const fields = Object.fromEntries(node.members.map(m => [m.name, m.value]))
  if (required.some(key => !Object.hasOwn(fields, key))) return invalid()
  return fields
}
function string(node: JsonNode | undefined, empty = false): string {
  if (node?.kind !== 'string' || !empty && !node.value || /[\r\n\0]/.test(node.value)) return invalid()
  return node.value
}
function literal(node: JsonNode | undefined, expected: string): string {
  const value = string(node, expected === '')
  if (value !== expected) return invalid()
  return value
}
function integer(node: JsonNode | undefined): 1 | 2 {
  if (node?.kind !== 'number' || node.literal !== '1' && node.literal !== '2') return invalid()
  return node.literal === '1' ? 1 : 2
}
function digest(node: JsonNode | undefined): string {
  const value = string(node)
  if (!/^[a-f0-9]{64}$/.test(value)) return invalid()
  return value
}
function id(node: JsonNode | undefined): string {
  const value = string(node)
  if (!/^[a-f0-9]{32}$/.test(value)) return invalid()
  return value
}
function timestamp(node: JsonNode | undefined): string {
  const value = string(node)
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value) || !Number.isFinite(Date.parse(value))) return invalid()
  return value
}
function approval(node: JsonNode): SourceApprovalReference {
  const a = members(node, ['id', 'revision', 'approvedAt'], ['revokedAt'])
  const revision = integer(a.revision), approvedAt = timestamp(a.approvedAt)
  const revokedAt = a.revokedAt === undefined ? undefined : timestamp(a.revokedAt)
  if ((revision === 2) !== (revokedAt !== undefined) || revokedAt !== undefined && Date.parse(revokedAt) < Date.parse(approvedAt)) return invalid()
  return {id: id(a.id), revision, approvedAt, ...(revokedAt === undefined ? {} : {revokedAt})}
}
function snapshot(node: JsonNode): SourceSnapshot {
  const s = members(node, ['adapterId', 'adapterVersion', 'kind', 'trust', 'configurationDigest', 'toolListDigest', 'completeDeclarations', 'configuration', 'status'], ['identity', 'identityDigest'])
  const adapter = string(s.adapterId)
  if (!Object.hasOwn(adapters, adapter)) return invalid()
  const adapterId = adapter as SourceAdapterID, expected = adapters[adapterId]
  literal(s.adapterVersion, '1'); literal(s.kind, 'custom'); literal(s.trust, 'first-party')
  if (s.completeDeclarations.kind !== 'array' || s.completeDeclarations.items.length !== 1) return invalid()
  const d = members(s.completeDeclarations.items[0], ['name', 'description', 'inputSchema', 'taxonomy', 'effectClass', 'delegation', 'approval'])
  const taxonomy = members(d.taxonomy, ['operation', 'effect'])
  if (d.inputSchema.kind !== 'object') return invalid()
  const declaration: SourceDeclaration = {name: literal(d.name, expected[0]), description: string(d.description), inputSchema: d.inputSchema,
    taxonomy: {operation: literal(taxonomy.operation, expected[1]), effect: literal(taxonomy.effect, expected[2])},
    effectClass: literal(d.effectClass, expected[3]), delegation: literal(d.delegation, expected[4]), approval: literal(d.approval, expected[5])}
  const fields = members(s.configuration, configurationKeys)
  const configuration = Object.fromEntries(configurationKeys.map(key => [key, string(fields[key], true)])) as SourceConfiguration
  for (const key of configurationKeys) if (key.endsWith('Digest') && configuration[key] !== '' && !(key === 'preferenceDigest' && configuration[key] === 'absent') && !/^[a-f0-9]{64}$/.test(configuration[key])) return invalid()
  if (!configuration.deskDigest || !configuration.ownerDigest || new TextEncoder().encode(configuration.target).length > 4096) return invalid()
  const status = string(s.status)
  if (status !== 'setup-required' && status !== 'review-required') return invalid()
  let identity: SourceSnapshot['identity'], identityDigest: string | undefined
  if (status === 'setup-required') {
    if (s.identity !== undefined || s.identityDigest !== undefined) return invalid()
  } else {
    const i = members(s.identity, ['kind', 'label'])
    literal(i.kind, 'connection')
    const label = string(i.label), prefix = adapterId === 'desk-research-workflow' ? 'Desk research workflow ' : 'Desk research HTTP '
    if (label !== prefix + digest(s.configurationDigest)) return invalid()
    identity = {kind: 'connection', label}; identityDigest = digest(s.identityDigest)
  }
  return {adapterId, adapterVersion: '1', kind: 'custom', trust: 'first-party', configurationDigest: digest(s.configurationDigest), toolListDigest: digest(s.toolListDigest), configuration,
    completeDeclarations: [declaration], status, ...(identity === undefined ? {} : {identity, identityDigest})}
}
function review(node: JsonNode): SourceReview {
  const r = members(node, ['snapshot', 'status'], ['reviewId', 'reviewRevision', 'approval'])
  const held = snapshot(r.snapshot), status = string(r.status) as SourceReviewStatus
  if (!['setup-required', 'review-required', 'approved', 'changed', 'revoked'].includes(status) || (status === 'setup-required') !== (held.status === 'setup-required')) return invalid()
  const ref = r.approval === undefined ? undefined : approval(r.approval)
  if (status === 'approved' || status === 'changed') { if (ref?.revision !== 1) return invalid() }
  if (status === 'revoked' && ref?.revision !== 2 || status === 'review-required' && ref !== undefined) return invalid()
  const hasReview = r.reviewId !== undefined
  if (hasReview !== (r.reviewRevision !== undefined) || hasReview && (held.status !== 'review-required' || status === 'approved' || integer(r.reviewRevision) !== 1)) return invalid()
  return {snapshot: held, status, ...(ref === undefined ? {} : {approval: ref}), ...(hasReview ? {reviewId: id(r.reviewId), reviewRevision: 1 as const} : {})}
}
function parse(raw: string): JsonNode {
  if (new TextEncoder().encode(raw).length > MAX_SOURCE_CATALOG_BYTES) return invalid()
  try { return parseJsonText(raw) } catch { return invalid() }
}
/** Validates display/reference data only; the server rechecks all authority. */
export function readSourceCatalog(raw: string): SourceCatalog {
  const c = members(parse(raw), ['version', 'sources'])
  if (integer(c.version) !== 1 || c.sources.kind !== 'array' || c.sources.items.length !== 4) return invalid()
  const sources = c.sources.items.map(review)
  if (new Set(sources.map(row => row.snapshot.adapterId)).size !== 4) return invalid()
  const reviews = sources.flatMap(row => row.reviewId === undefined ? [] : [row.reviewId])
  const approvals = sources.flatMap(row => row.approval === undefined ? [] : [row.approval.id])
  if (new Set(reviews).size !== reviews.length || new Set(approvals).size !== approvals.length) return invalid()
  return {version: 1, sources}
}
export function readSourceRevocation(raw: string): SourceRevocation {
  const r = members(parse(raw), ['version', 'status', 'approval'])
  if (integer(r.version) !== 1) return invalid()
  literal(r.status, 'revoked')
  const ref = approval(r.approval)
  if (ref.revision !== 2) return invalid()
  return {version: 1, status: 'revoked', approval: ref}
}
