import { describe, expect, it } from 'vitest'
import { fileFindings, findingWords, otherFindings, packFindings } from './findings'
import type { Review } from './client'

describe('the runtime’s findings in plain words', () => {
  it('names each finding packs verify reports', () => {
    expect(findingWords('config-drift')).toBe('The project file changed; every decision waits for a lock')
    expect(findingWords('document-drift')).toBe('Changed since the last lock')
    expect(findingWords('lock-entry-missing')).toBe('New, never locked')
    expect(findingWords('locked-but-undeclared')).toBe('Removed from the project')
    expect(findingWords('document-missing')).toBe('File missing')
    expect(findingWords('path-mismatch')).toBe('Locked at another path')
  })
  it('shows a finding it does not know as the runtime wrote it', () => {
    expect(findingWords('something-new')).toBe('something-new')
  })
  it('groups pack findings by decision id, and nothing else', () => {
    const review: Review = { status: 'invalid', locked: true, diagnostics: [], files: [], contents: {}, findings: [
      { name: 'config-drift', path: 'jpack.json' },
      { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/a.json' },
      { name: 'path-mismatch', kind: 'pack', id: 'alpha', path: 'packs/a.json' },
      { name: 'document-drift', kind: 'graph', id: 'alpha', path: 'flow.json' }
    ] }
    const found = packFindings(review)
    expect([...found.keys()]).toEqual(['alpha'])
    expect(found.get('alpha')!.map(item => item.name)).toEqual(['document-drift', 'path-mismatch'])
  })
  it('puts each finding beside the file it is about, and keeps the rest', () => {
    const now = { state: 'text' as const, digest: 'sha256:0' }
    const review: Review = { status: 'invalid', locked: true, diagnostics: [], contents: { 'sha256:0': '{}' }, files: [
      { kind: 'config', path: 'jpack.json', lock: 'other', now },
      { kind: 'pack', id: 'alpha', path: 'packs/a.json', lock: 'other', now },
      { kind: 'graph', id: 'alpha', path: 'flow.json', lock: 'same', now }
    ], findings: [
      { name: 'config-drift', path: 'jpack.json' },
      { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/a.json' },
      { name: 'document-missing', kind: 'pack', id: 'gone', path: 'packs/g.json' }
    ] }
    expect(fileFindings(review, review.files[0]!).map(item => item.name)).toEqual(['config-drift'])
    expect(fileFindings(review, review.files[1]!).map(item => item.name)).toEqual(['document-drift'])
    expect(fileFindings(review, review.files[2]!)).toEqual([])
    expect(otherFindings(review).map(item => item.id)).toEqual(['gone'])
  })
})
