import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { PackOverview } from '../packs/PackWorkspace'
import { GraphOverview } from '../graphs/GraphOverview'
import type { GraphDocument, PackDocument } from '../mcp/types'
import { DraftPanel } from '../research/ui/DraftPanels'
import { INITIAL_STATE } from '../research/run'

afterEach(cleanup)
const pack = {
  id: 'example', title: 'Example', version: '1.0.0',
  decision: { question: 'Can this launch?', intent: 'Can this launch?' }, description: 'Checks launch readiness.',
  outcomes: Array.from({ length: 6 }, (_, i) => ({ id: `o${i}`, label: `Outcome ${i}` })),
  fallbackOutcome: 'o5', applicability: { all: [] }, evidenceRequirements: [],
  escalation: { target: { kind: 'human-role', name: 'Operations lead' } },
  rules: [{ id: 'rule-one', sourceRefs: ['s1'] }], sources: [{ id: 's1', title: 'Policy' }]
} as unknown as PackDocument

it('keeps purpose and results visible, marks fallback once, and collapses secondary facts', () => {
  const logic = vi.fn()
  render(<PackOverview document={pack} onViewLogic={logic}/>)
  const overview = screen.getByRole('region', { name: 'Pack overview' })
  expect(within(overview).getAllByText('Can this launch?')).toHaveLength(1)
  expect(within(overview).getAllByRole('listitem')).toHaveLength(4)
  expect(within(overview).getAllByText('Fallback')).toHaveLength(1)
  expect(within(overview).getByText('Outcome 5')).toBeTruthy()
  expect(within(overview).getByText('2 more outcomes in Logic.')).toBeTruthy()
  expect(within(overview).queryByText('At a glance')).toBeNull()
  const details = overview.querySelector('details')!
  expect(details.open).toBe(false)
  fireEvent.click(within(overview).getByText('Details'))
  expect(details.open).toBe(true)
  expect(within(details).getByText('Operations lead')).toBeTruthy()
  expect(within(details).getByText('No separate evidence requirements are declared.')).toBeTruthy()
  fireEvent.click(within(overview).getByRole('button', { name: 'View logic' }))
  expect(logic).toHaveBeenCalledTimes(1)
})

it('uses the same overview for a draft without appending rule/source cards or JSON', () => {
  const state = { ...INITIAL_STATE, candidates: [{ revision: 1, text: JSON.stringify(pack), document: pack, digest: 'digest', producedBy: 'conversation' }] } as typeof INITIAL_STATE
  render(<DraftPanel state={state}/>)
  expect(screen.getByRole('region', { name: 'Pack overview' })).toBeTruthy()
  expect(screen.queryByText('Rules and their sources')).toBeNull()
  expect(screen.queryByText('Full document (JSON)')).toBeNull()
  expect(screen.queryByText('rule-one')).toBeNull()
})

it('counts reused packs once, names the result step and keeps composition details collapsed', () => {
  const logic = vi.fn()
  const document: GraphDocument = { description: 'Combines readiness checks.', nodes: { first: { pack: 'check' }, retry: { pack: 'check' }, final: { pack: 'decision' } }, edges: [], result: 'final', version: '9.0.0' }
  render(<GraphOverview document={document} description={document.description} onViewLogic={logic}/>)
  const overview = screen.getByRole('region', { name: 'Graph overview' })
  expect(within(overview).getAllByText(document.description!)).toHaveLength(1)
  expect(within(overview).getByText('3 steps · 2 distinct packs')).toBeTruthy()
  expect(within(overview).getByText('final')).toBeTruthy()
  expect(within(overview).getByText('Provided by decision.')).toBeTruthy()
  expect(within(overview).queryByText('9.0.0')).toBeNull()
  expect(within(overview).queryByRole('list')).toBeNull()
  expect(overview.querySelector('details')!.open).toBe(false)
  fireEvent.click(within(overview).getByRole('button', { name: 'View logic' }))
  expect(logic).toHaveBeenCalledTimes(1)
})

it('does not turn missing or malformed graph members into an empty composition or resolved result', () => {
  const { rerender } = render(<GraphOverview loading/>)
  expect(screen.getByText('Loading overview…')).toBeTruthy()
  expect(screen.queryByText('No readable graph document was returned.')).toBeNull()
  rerender(<GraphOverview document={{ result: 'missing', nodes: { bad: {} } } as unknown as GraphDocument}/>)
  expect(screen.getByText('The result step could not be found.')).toBeTruthy()
  expect(screen.getByText('Composition details could not be read.')).toBeTruthy()
  expect(screen.queryByText('0 steps · 0 distinct packs')).toBeNull()
  rerender(<GraphOverview document={{ nodes: {}, edges: [] }}/>)
  expect(screen.getByText('0 steps · 0 distinct packs')).toBeTruthy()
  expect(screen.getByText('No result step has been selected.')).toBeTruthy()
})
