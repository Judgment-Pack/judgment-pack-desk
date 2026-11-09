import { expect, it } from 'vitest'
import { AIFailureError } from '@judgment-pack/harness/failures'
import { wireJSON } from '../../chat/wire'
import { MAX_SOURCE_CATALOG_BYTES, readSourceCatalog, readSourceRevocation } from './catalog'

type ObjectValue = Record<string, unknown>
interface WireRow { snapshot: ObjectValue; status: string; reviewId?: string; reviewRevision?: number; approval?: ObjectValue }
interface WireCatalog { version: number; sources: WireRow[] }
const hash = 'a'.repeat(64), recordId = '0123456789abcdef0123456789abcdef'
const at = '2026-01-02T03:04:05.123456789Z'
// Handwritten producer-shape examples; no response recorded from a run.
function catalog(): WireCatalog {
  return {version: 1, sources: [
    ['desk-search', 'search_sources', 'discover', 'read-external', 'read', 'research', 'none'],
    ['desk-link', 'read_link', 'read', 'read-external', 'read', 'read', 'none'],
    ['desk-website', 'explore_website', 'discover', 'read-external', 'read', 'research', 'none'],
    ['desk-research-workflow', 'request_dialect_research', 'act', 'act', 'act', '', 'required'],
  ].map(([adapterId, name, operation, effect, effectClass, delegation, approval]) => ({status: 'setup-required', snapshot: {
    adapterId, adapterVersion: '1', kind: 'custom', trust: 'first-party', status: 'setup-required',
    configurationDigest: hash, toolListDigest: hash,
    completeDeclarations: [{name, description: 'Complete test declaration.', inputSchema: {type: 'object', properties: {}, additionalProperties: false}, taxonomy: {operation, effect}, effectClass, delegation, approval}],
    configuration: {target: '', targetDigest: '', authorityDigest: '', publicKeyDigest: '', deskDigest: hash, preferenceDigest: '', connection: '', revision: '', provider: '', bundleDigest: '', verifierDigest: '', gatewayBuildDigest: '', ownerDigest: hash, catalogDigest: '', catalogOwnerDigest: ''},
  }}))}
}
function ready(row: WireRow) {
  row.status = 'review-required'
  Object.assign(row.snapshot, {status: 'review-required', identity: {kind: 'connection', label: `Desk research ${row.snapshot.adapterId === 'desk-research-workflow' ? 'workflow' : 'HTTP'} ${hash}`}, identityDigest: hash})
}
function reference(revoked = false): ObjectValue {
  return {id: recordId, revision: revoked ? 2 : 1, approvedAt: at, ...(revoked ? {revokedAt: '2026-01-02T03:04:06Z'} : {})}
}
function declaration(row: WireRow): ObjectValue { return (row.snapshot.completeDeclarations as ObjectValue[])[0] }

it('requires the complete four-adapter catalog and retains every declaration member', () => {
  const input = catalog(), result = readSourceCatalog(JSON.stringify(input))
  expect(result.sources.map(row => row.snapshot.adapterId)).toEqual(['desk-search', 'desk-link', 'desk-website', 'desk-research-workflow'])
  expect(result.sources[3].snapshot.completeDeclarations[0]).toMatchObject({taxonomy: {operation: 'act', effect: 'act'}, approval: 'required', delegation: ''})
  expect(wireJSON(result.sources[0].snapshot.completeDeclarations[0].inputSchema)).toBe('{"additionalProperties":false,"properties":{},"type":"object"}')
  expect(result.sources.every(row => row.reviewId === undefined && row.approval === undefined)).toBe(true)
})
it.each(['review-required', 'approved', 'changed', 'revoked', 'setup-required'])('reads the exact %s reference state without inferring a grant', status => {
  const input = catalog(), row = input.sources[0]
  if (status !== 'setup-required') ready(row)
  row.status = status
  if (status !== 'review-required') row.approval = reference(status === 'revoked')
  if (['review-required', 'changed', 'revoked'].includes(status)) { row.reviewId = recordId; row.reviewRevision = 1 }
  const parsed = readSourceCatalog(JSON.stringify(input)).sources[0]
  expect(parsed.status).toBe(status)
  expect(parsed.reviewId).toBe(row.reviewId)
  expect(parsed.approval).toEqual(row.approval)
})
it('accepts an approve response without fresh review IDs and an unavailable source with a revoked old record', () => {
  const input = catalog(); ready(input.sources[0]); input.sources[1].approval = reference(true)
  const parsed = readSourceCatalog(JSON.stringify(input))
  expect(parsed.sources[0].reviewId).toBeUndefined()
  expect(parsed.sources[1].status).toBe('setup-required')
  expect(parsed.sources[1].approval?.revision).toBe(2)
})
it('keeps every schema number literal instead of rounding a reviewed declaration', () => {
  const input = catalog(); declaration(input.sources[0]).inputSchema = {type: 'object', minimum: 'NUMBER_SENTINEL'}
  const raw = JSON.stringify(input).replace('"NUMBER_SENTINEL"', '9007199254740993')
  expect(wireJSON(readSourceCatalog(raw).sources[0].snapshot.completeDeclarations[0].inputSchema)).toContain('9007199254740993')
})

const refusals: [string, (c: WireCatalog, row: WireRow) => void][] = [
  ['missing adapter', c => { c.sources.pop() }],
  ['duplicate adapter', c => { c.sources[1] = c.sources[0] }],
  ['unknown adapter', (_, r) => { r.snapshot.adapterId = 'remote-mcp' }],
  ['unexpected catalog authority', c => { Object.assign(c, {approved: true}) }],
  ['unknown row field', (_, r) => { Object.assign(r, {scope: {}}) }],
  ['unknown snapshot field', (_, r) => { r.snapshot.token = 'not-a-token' }],
  ['missing configuration member', (_, r) => { delete (r.snapshot.configuration as ObjectValue).provider }],
  ['unknown configuration member', (_, r) => { (r.snapshot.configuration as ObjectValue).secret = 'redacted' }],
  ['invalid owner digest', (_, r) => { (r.snapshot.configuration as ObjectValue).ownerDigest = '' }],
  ['truncated declarations', (_, r) => { r.snapshot.completeDeclarations = [] }],
  ['extra declaration', (_, r) => { (r.snapshot.completeDeclarations as ObjectValue[]).push(declaration(r)) }],
  ['wrong tool mapping', (_, r) => { declaration(r).name = 'read_link' }],
  ['extra declaration authority', (_, r) => { declaration(r).approved = true }],
  ['wrong taxonomy', (_, r) => { declaration(r).taxonomy = {operation: 'act', effect: 'act'} }],
  ['schema is not an object', (_, r) => { declaration(r).inputSchema = null }],
  ['wrong delegation', (_, r) => { declaration(r).delegation = '' }],
  ['wrong action approval', (_, r) => { declaration(r).approval = 'required' }],
  ['partial review reference', (_, r) => { ready(r); r.reviewId = recordId }],
  ['stale review revision', (_, r) => { ready(r); r.reviewId = recordId; r.reviewRevision = 2 }],
  ['setup masquerading as approved', (_, r) => { r.status = 'approved'; r.approval = reference() }],
  ['identity without readiness', (_, r) => { r.snapshot.identity = {kind: 'connection', label: 'unobserved'} }],
  ['missing observed identity', (_, r) => { ready(r); delete r.snapshot.identity }],
  ['wrong identity revision', (_, r) => { ready(r); (r.snapshot.identity as ObjectValue).label = 'Desk research HTTP changed' }],
  ['approved without receipt', (_, r) => { ready(r); r.status = 'approved' }],
  ['approved with pending review', (_, r) => { ready(r); r.status = 'approved'; r.approval = reference(); r.reviewId = recordId; r.reviewRevision = 1 }],
  ['changed with revoked receipt', (_, r) => { ready(r); r.status = 'changed'; r.approval = reference(true) }],
  ['revoked with active receipt', (_, r) => { ready(r); r.status = 'revoked'; r.approval = reference() }],
  ['revoked missing timestamp', (_, r) => { ready(r); r.status = 'revoked'; r.approval = reference(true); delete r.approval.revokedAt }],
  ['invalid approval timestamp', (_, r) => { r.approval = {...reference(), approvedAt: 'not-a-date'} }],
  ['reversed approval timestamps', (_, r) => { r.approval = {...reference(true), revokedAt: '2026-01-01T00:00:00Z'} }],
  ['duplicate review selectors', c => { for (const r of c.sources.slice(0, 2)) { ready(r); r.reviewId = recordId; r.reviewRevision = 1 } }],
  ['duplicate approval selectors', c => { for (const r of c.sources.slice(0, 2)) { ready(r); r.status = 'approved'; r.approval = reference() } }],
]
it.each(refusals)('refuses %s before exposing review controls', (_, change) => {
  const input = catalog(); change(input, input.sources[0])
  expect(() => readSourceCatalog(JSON.stringify(input))).toThrow(AIFailureError)
})
it.each([
  ['duplicate top-level member', (raw: string) => raw.replace('"version":1', '"version":1,"version":1')],
  ['duplicate nested schema member', (raw: string) => raw.replace('"type":"object"', '"type":"object","type":"object"')],
  ['wrong integer wire spelling', (raw: string) => raw.replace('"version":1', '"version":1.0')],
  ['trailing document', (raw: string) => raw + '{}'],
  ['invalid Unicode', (raw: string) => raw.replace('Complete test declaration.', '\\ud800')],
])('refuses %s in the raw response', (_, change) => {
  expect(() => readSourceCatalog(change(JSON.stringify(catalog())))).toThrow(AIFailureError)
})
it('accepts the existing byte bound and refuses one byte over', () => {
  const raw = JSON.stringify(catalog()), bounded = raw + ' '.repeat(MAX_SOURCE_CATALOG_BYTES - new TextEncoder().encode(raw).length)
  expect(readSourceCatalog(bounded).sources).toHaveLength(4)
  expect(() => readSourceCatalog(bounded + ' ')).toThrow(AIFailureError)
})
it('reads only the exact persisted revocation receipt', () => {
  const revoked = {version: 1, status: 'revoked', approval: reference(true)}
  expect(readSourceRevocation(JSON.stringify(revoked)).approval.revision).toBe(2)
  expect(() => readSourceRevocation(JSON.stringify({...revoked, approval: reference()}))).toThrow(AIFailureError)
  expect(() => readSourceRevocation(JSON.stringify({...revoked, sources: []}))).toThrow(AIFailureError)
  expect(() => readSourceRevocation(JSON.stringify({...revoked, status: 'approved'}))).toThrow(AIFailureError)
})
