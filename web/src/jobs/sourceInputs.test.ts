import { expect, it } from 'vitest'
import { base64 } from '../documents/client'
import type { PackDocument } from '../mcp/types'
import { initialMapping, sourcePaths, sourceText, verifySource } from './sourceInputs'
import { signedInput } from './__fixtures__/signedInput'
const snapshot = (text: string) => ({ version: 1 as const, original: { name: 'input.json', mediaType: 'application/json', bytes: base64(new TextEncoder().encode(text)), sha256: 'sha256:test' } })
it('lists exact escaped paths without reserializing large numbers or rounding decimals', () => {
 const text = '{"n":9007199254740993,"decimal":1.234567890123456789,"a/b":[null,false,0]}'
 const original = snapshot(text)
 expect(sourcePaths(original)).toEqual(['', '/n', '/decimal', '/a~1b', '/a~1b/0', '/a~1b/1', '/a~1b/2'])
 expect(sourceText(original)).toBe(text)
})
it.each(['{"a":1,"a":2}', '[]', '{"s":"\\ud800"}', '{} {}', '{"a":'])('rejects ambiguous or malformed input %s', text => expect(() => sourcePaths(snapshot(text))).toThrow())
it('suggests only source paths that actually exist, preserving absent evidence', () => {
 const doc = { rules: [{ condition: { op: 'fact', path: '/request/type', value: 'clean' } }, { condition: { op: 'fact', path: '/request/absent', value: true } }], evidenceRequirements: [{ id: 'receipt' }, { id: 'absent' }] } as unknown as PackDocument
 expect(initialMapping(doc, 'local-file', sourcePaths(snapshot('{"facts":{"request":{"type":"clean"}},"evidence":{"receipt":"unknown"}}')))).toEqual({ version: 1, provider: 'local-file', facts: [{ target: '/request/type', source: '/facts/request/type' }], evidence: [{ requirement: 'receipt', source: '/evidence/receipt' }] })
})
it('verifies an actual signed Drive record and refuses a changed pin, selection, source or original', async () => {
 const { object, pin } = await signedInput()
 const source = { mapping: { version: 1 as const, provider: 'google-drive' as const, facts: [], evidence: [] }, snapshot: object }
 await verifySource(source, pin)
 await expect(verifySource(source, null)).rejects.toThrow()
 await expect(verifySource(source, { ...pin, signer: { ...pin.signer, public: '11'.repeat(32) } })).rejects.toThrow()
 for (const kind of ['selection', 'source', 'original']) {
  const altered = structuredClone(source)
  if (kind === 'selection') altered.snapshot.proof!.drive!.fileId = 'other'
  if (kind === 'source') altered.snapshot.proof!.source = 'documents'
  if (kind === 'original') altered.snapshot.original.bytes = base64(new TextEncoder().encode('{}'))
  await expect(verifySource(altered, pin)).rejects.toThrow()
 }
})
