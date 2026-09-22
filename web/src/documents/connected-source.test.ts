import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, it } from 'vitest'
import { readDocumentRecord } from './record'
const fixture = (kind: string) => JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', `${kind}-snapshot.json`), 'utf8'))
it.each(['notion', 'obsidian'])('reads the gateway-produced %s snapshot', kind => {
 const record = readDocumentRecord(fixture(kind))
 expect(record.provenance.source.provider).toBe(kind)
 expect(record.document.version).toBe(record.document.id)
})
it.each([
 ['notion', 'url', 'https://evil.invalid/11111111222233334444555555555555'],
 ['notion', 'resourceId', '99999999-9999-9999-9999-999999999999'],
 ['obsidian', 'url', 'obsidian://new?vault=Fixture&file=Policy'],
 ['obsidian', 'resourceId', '../private.md'],
 ['obsidian', 'resourceId', '.obsidian/private.md'],
 ['obsidian', 'url', 'obsidian://open?vault=Fixture&file=notes/Policy&file=other'],
 ['notion', 'version', 'sha256:' + '0'.repeat(64)],
 ['obsidian', 'provider', 'unreviewed-provider'],
])('refuses a substituted %s %s', (kind, field, value) => {
 const record = fixture(kind); record.provenance.source[field] = value
 expect(() => readDocumentRecord(record)).toThrow()
})
it.each(['notion','obsidian'])('requires %s snapshot version to identify the retained document',kind=>{
 const record=fixture(kind)
 record.document.version=record.provenance.source.version='sha256:'+'0'.repeat(64)
 expect(()=>readDocumentRecord(record)).toThrow()
})
