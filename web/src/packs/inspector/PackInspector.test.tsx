/**
 * The three panels, driven from the pointer in the route.
 *
 * What each may say is the substance here: References reports a document fact
 * and never a verdict, Checks prints the runtime's own words and never dresses
 * an empty set as a clean bill.
 */
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PackDocument, ValidationReport } from '../../mcp/types'
import { anchor, truncationNote } from '../checks'
import { PackInspector } from './PackInspector'

afterEach(cleanup)

const full = JSON.parse(
  readFileSync(join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'), 'utf8')
) as PackDocument

const META = {
  path: 'packs/vendor-onboarding.pack.json',
  bytes: 4182,
  sha256: 'a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2'
}

const REPORT: ValidationReport = {
  status: 'invalid',
  layers: [
    { name: 'carrier', status: 'passed' },
    { name: 'structural', status: 'failed' }
  ],
  diagnostics: [
    {
      code: 'JPS-STRUCTURE-DECIMAL-OPERAND',
      codeStability: 'provisional',
      layer: 'structural',
      severity: 'error',
      instancePath: '/rules/1/when/conditions/0/value',
      message: 'Ordered comparison operand must be a decimal string, for example "5000".'
    }
  ],
  diagnosticsTruncated: false
}

const RENDERED = new Set([
  '',
  '/rules',
  '/rules/1',
  '/rules/1/when/conditions/0/value',
  '/outcomes',
  '/outcomes/0'
])

function draw(
  at: string | null,
  overrides: Partial<Parameters<typeof PackInspector>[0]> = {}
) {
  const props = {
    packId: 'vendor-onboarding',
    document: full,
    at,
    meta: META,
    fileSha256: META.sha256,
    fileBytes: META.bytes,
    anchored: anchor(REPORT, RENDERED),
    truncation: truncationNote(REPORT),
    stale: false,
    checkedWhat: 'checked against the bytes of packs/vendor-onboarding.pack.json',
    tab: 'member' as string | null,
    onTabChange: vi.fn(),
    ...overrides
  }
  return { props, ...render(<MemoryRouter><PackInspector {...props} /></MemoryRouter>) }
}

describe('nothing selected', () => {
  it('renders the empty state', () => {
    draw(null)
    expect(screen.getByText(/Select a member of the document/)).toBeTruthy()
    expect(screen.queryByRole('tab')).toBeNull()
  })
})

describe('the Member panel', () => {
  it('prints the member’s own subtree and the provenance beside it', () => {
    draw('/rules/1')
    const json = screen.getByText(/"approve-when-clear"/)
    expect(json.textContent).toContain('"greater-than"')
    expect(screen.getByText(META.path)).toBeTruthy()
    expect(screen.getByText('4,182')).toBeTruthy()
    expect(screen.getByText(META.sha256)).toBeTruthy()
  })

  it('says the bytes match only when the two digests are equal', () => {
    draw('/rules/1')
    expect(screen.getByText('matches the file the editor holds')).toBeTruthy()
    cleanup()
    // Two answers about one file. Where they disagree the sentence is absent,
    // because the desk never observed the binding it would be asserting.
    draw('/rules/1', { fileSha256: 'f'.repeat(64) })
    expect(screen.queryByText('matches the file the editor holds')).toBeNull()
    cleanup()
    // And where the file did not answer at all there is nothing to bind to.
    draw('/rules/1', { fileSha256: undefined })
    expect(screen.queryByText('matches the file the editor holds')).toBeNull()
  })

  it('says so where the pointer names no member', () => {
    draw('/rules/9')
    expect(screen.getByText('The document declares no member at this pointer.')).toBeTruthy()
  })
})

describe('the References panel', () => {
  it('reports both directions', () => {
    const { props } = draw('/rules/1')
    // Radix activates a tab on pointer-down, not on click.
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'References' }))
    expect(props.onTabChange).toHaveBeenCalledWith('references')
    cleanup()
    draw('/rules/1', { tab: 'references' })
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).toContain('outcome')
    expect(panel.textContent).toContain('approve')
    expect(panel.textContent).toContain('cited by')
    expect(panel.textContent).toContain('vendor-waiver')
  })

  it('says an id resolves to nothing as a document fact and never a verdict', () => {
    const broken: PackDocument = {
      ...full,
      rules: [{ ...full.rules[0]!, outcome: 'nope' }]
    }
    draw('/rules/0', { document: broken, tab: 'references' })
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).toContain('no declared outcome carries this id')
    // JPS-SEMANTIC-UNRESOLVED-OUTCOME is the runtime's to issue.
    for (const verdict of ['invalid', 'error', 'broken']) {
      expect(panel.textContent, verdict).not.toContain(verdict)
    }
  })
})

describe('the Checks panel', () => {
  it('prints code, layer, severity, provisional and the pointer verbatim', () => {
    draw('/rules/1/when/conditions/0/value', { tab: 'checks' })
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).toContain('JPS-STRUCTURE-DECIMAL-OPERAND')
    expect(panel.textContent).toContain('structural')
    expect(panel.textContent).toContain('error')
    expect(panel.textContent).toContain('provisional')
    expect(panel.textContent).toContain('/rules/1/when/conditions/0/value')
    expect(panel.textContent).toContain(
      'Ordered comparison operand must be a decimal string'
    )
  })

  it('does not dress an empty set as a clean bill', () => {
    draw('/outcomes/0', { tab: 'checks' })
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).toContain('No other diagnostic names this member.')
    for (const verdict of ['valid', 'passed', 'no problems', 'clean']) {
      expect(panel.textContent, verdict).not.toContain(verdict)
    }
  })

  it('says the list was cut rather than that nothing else was found', () => {
    const cut: ValidationReport = { ...REPORT, diagnosticsTruncated: true }
    draw('/outcomes/0', { tab: 'checks', truncation: truncationNote(cut) })
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).not.toContain('No other diagnostic names this member.')
    expect(panel.textContent).toContain('100')
  })

  it('names which bytes were checked', () => {
    draw('/outcomes/0', { tab: 'checks' })
    expect(screen.getByRole('tabpanel').textContent).toContain(
      'checked against the bytes of packs/vendor-onboarding.pack.json'
    )
  })

  it('says a check ran over other bytes rather than re-anchoring it', () => {
    draw('/rules/1', { tab: 'checks', stale: true, anchored: [] })
    expect(screen.getByRole('tabpanel').textContent).toContain(
      'computed against other bytes'
    )
  })

  it('says why there is no check, where there is none', () => {
    draw('/rules/1', {
      tab: 'checks',
      unavailable: 'This runtime does not offer validate, so this document is unchecked.'
    })
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).toContain('does not offer validate')
    expect(panel.textContent).not.toContain('No other diagnostic')
  })
})
