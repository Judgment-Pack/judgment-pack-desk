import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ProposalReview } from './ProposalReview'
import { importMatrix } from './model'
afterEach(cleanup)
const cases = importMatrix(
  {
    matrixVersion: '3',
    cases: ['Normal', 'Boundary', 'Missing evidence'].map((focus) => ({
      id: focus,
      focus,
      facts: {},
      expectedDisposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } },
    })),
  },
  'ai',
)
it('reviews and saves selected cases as a batch while excluding previously saved cases', () => {
  const save = vi.fn(),
    edit = vi.fn()
  const view = render(
    <ProposalReview
      cases={cases}
      saved={{ Normal: 'receipt' }}
      busy={false}
      stale={false}
      error=""
      onSave={save}
      onEdit={edit}
      onClose={vi.fn()}
    />,
  )
  expect((screen.getByRole('checkbox', { name: 'Select Normal' }) as HTMLInputElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select Missing evidence' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save selected cases (1)' }))
  expect(save.mock.calls[0]?.[0].map((c: { id: string }) => c.id)).toEqual(['Boundary'])
  fireEvent.click(screen.getByRole('button', { name: 'Boundary' }))
  expect(edit).toHaveBeenCalledWith(cases[1])
  view.rerender(
    <ProposalReview
      cases={cases}
      saved={{ Normal: 'receipt', Boundary: 'receipt' }}
      busy={false}
      stale={true}
      error=""
      onSave={save}
      onEdit={edit}
      onClose={vi.fn()}
    />,
  )
  expect((screen.getByRole('button', { name: /Save selected cases/ }) as HTMLButtonElement).disabled).toBe(
    true,
  )
  expect(screen.getByRole('status').textContent).toContain('pack changed')
})
