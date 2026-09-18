import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, it } from 'vitest'
import { readDocumentRecord, usablePages, needsPartialConsent } from './record'
const root = join(import.meta.dirname, '__fixtures__')
const fixture = (name: string) => JSON.parse(readFileSync(join(root, name + '.json'), 'utf8'))
it.each(readdirSync(root).filter(n => n.endsWith('.json')))('reads merged gateway record %s', name => {
  const record = readDocumentRecord(fixture(name.slice(0, -5)))
  expect(needsPartialConsent(record)).toBe(record.processing.status !== 'complete' || record.content.pages.some(p => p.unmapped > 0))
  expect(usablePages(record).every(p => p.status === 'ok' && p.unmapped === 0)).toBe(true)
})
it('allows new members but withholds unknown versions and states', () => {
  const record = fixture('complete-verbatim-text'); record.extra = true; record.content.extra = true; record.content.pages[0].extra = true
  expect(readDocumentRecord(record).content.pages).toHaveLength(1)
  record.attachmentVersion = '2'; expect(() => readDocumentRecord(record)).toThrow()
  record.attachmentVersion = '1'; record.content.pages[0].status = 'future'; expect(() => readDocumentRecord(record)).toThrow()
})
it.each(['chars','page-count','page-gap','not-normalized','no-text','truncated','status','retention','unmapped'] as const)('rejects inconsistent %s', kind => {
  const record = fixture('complete-verbatim-text')
  switch (kind) {
    case 'chars': record.content.pages[0].chars++; break
    case 'page-count': record.content.pageCount = 0; break
    case 'page-gap': record.content.pages[0].number = 2; break
    case 'not-normalized': record.content.pages[0].text = '\n' + record.content.pages[0].text; record.content.pages[0].chars++; record.content.chars++; break
    case 'no-text': record.content.pages[0].status = 'no-text'; break
    case 'truncated': record.content.pageCount = 2; break
    case 'status': record.processing.status = 'partial'; break
    case 'retention': record.original.bytes = 'hidden bytes'; break
    case 'unmapped': record.content.pages[0].unmapped = 1; break
  }
  expect(() => readDocumentRecord(record)).toThrow()
})
it('counts scalar values and retains a BOM that is legitimate after blank-line trimming', () => {
  const record = fixture('complete-verbatim-text'); record.content.pages[0].text = '\ufeffHello 😀'; record.content.pages[0].chars = 8; record.content.chars = 8
  expect(readDocumentRecord(record).content.chars).toBe(8)
})
