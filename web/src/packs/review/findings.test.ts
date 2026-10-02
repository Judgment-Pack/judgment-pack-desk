import { describe, expect, it } from 'vitest'
import { findingWords, packFindings } from './findings'
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
    const side = { state: 'absent' as const }
    const review: Review = { status: 'invalid', locked: true, diagnostics: [], set: null, findings: [
      { name: 'config-drift', path: 'jpack.json', earlier: side, now: side },
      { name: 'document-drift', kind: 'pack', id: 'alpha', path: 'packs/a.json', earlier: side, now: side },
      { name: 'path-mismatch', kind: 'pack', id: 'alpha', path: 'packs/a.json', earlier: side, now: side },
      { name: 'document-drift', kind: 'graph', id: 'alpha', path: 'flow.json', earlier: side, now: side }
    ] }
    const found = packFindings(review)
    expect([...found.keys()]).toEqual(['alpha'])
    expect(found.get('alpha')!.map(item => item.name)).toEqual(['document-drift', 'path-mismatch'])
  })
})
