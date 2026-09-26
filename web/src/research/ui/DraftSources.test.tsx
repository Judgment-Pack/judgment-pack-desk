import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { DraftSources } from './DraftSources'
import { TestsPanel } from './DraftPanels'
import { INITIAL_STATE } from '../run'

it('lists declared references and retained inputs without claiming receipt verification', () => {
  const onRead = vi.fn()
  render(<DraftSources document={{ sources: [{ id: 'policy', title: 'Policy guide', locator: { value: 'https://example.org/policy' } }] }} files={[{ name: 'notes.txt', text: 'Notes' }]} onRead={onRead} />)
  expect(screen.getByText('Policy guide')).toBeTruthy()
  expect(screen.getByText('notes.txt')).toBeTruthy()
  expect(screen.queryByText('receipt verified')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /Policy guide/ }))
  expect(onRead).toHaveBeenCalledOnce()
})
it('shows a saved structure check without inventing behavioral cases', () => {
  const digest = 'a'.repeat(64)
  render(<TestsPanel mode="draft" state={{ ...INITIAL_STATE, restored: true, candidates: [{ revision: 1, text: '{}', document: {}, digest, producedBy: 'conversation', previousCheck: { documentDigest: digest, valid: true, diagnostics: [], cases: [] } }] }} onSelect={vi.fn()} />)
  expect(screen.getByRole('heading', { name: 'Structure check' })).toBeTruthy()
  expect(screen.getByText('Valid')).toBeTruthy()
  expect(screen.getByText('Saved result. Recheck the draft before creating it.')).toBeTruthy()
  expect(screen.queryByRole('table')).toBeNull()
})
