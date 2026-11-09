import { expect, it } from 'vitest'
import vectors from '../../../../internal/desk/testdata/assistant-source-decisions.json'
import { encodeSourceDecisionRequest, readSourceDecisionRequest, type SourceDecisionKind } from './requests'

it('loads the handwritten shared source decision corpus', () => {
  expect(vectors.version).toBe(1)
  expect(vectors.cases.length).toBeGreaterThan(0)
  expect(new Set(vectors.cases.map(row => row.id)).size).toBe(vectors.cases.length)
})
it.each(vectors.cases)('matches the Go source decision reader: $id', row => {
  expect(['approve', 'revoke']).toContain(row.kind)
  const raw = row.raw + ' '.repeat(Math.max(0, (row.padToBytes ?? 0) - new TextEncoder().encode(row.raw).length))
  const read = () => readSourceDecisionRequest(row.kind as SourceDecisionKind, raw)
  if (row.accept) {
    const value = read()
    expect(value).toEqual(JSON.parse(row.raw))
    expect(readSourceDecisionRequest(row.kind as SourceDecisionKind, encodeSourceDecisionRequest(row.kind as SourceDecisionKind, value))).toEqual(value)
  } else expect(read).toThrow()
})
it('refuses client-added authority before encoding a decision', () => {
  expect(() => encodeSourceDecisionRequest('approve', {reviewId:'0123456789abcdef0123456789abcdef', reviewRevision:1, scope:{}})).toThrow()
  expect(() => encodeSourceDecisionRequest('revoke', undefined)).toThrow()
})
