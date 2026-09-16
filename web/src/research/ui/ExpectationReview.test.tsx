import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { blockedExpectation, disagreeingCase, proposedExpectation, withheldButPassing } from '../__fixtures__/expectationReview'
import { DraftTabs, ReviewPanel, TestsPanel } from './DraftPanels'

afterEach(cleanup)

describe('expectation review UI', () => {
  it('counts the blocked case and opens its review without hiding it in a smaller passed suite', () => {
    render(<DraftTabs state={blockedExpectation} sources={[]} selection={null} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(screen.getByRole('tab', { name: 'Tests (2)' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Review expectations' }))
    expect(screen.getByRole('heading', { name: 'Blocked expectations' })).toBeTruthy()
    expect(screen.getByLabelText('Original expectation').textContent).toContain('"reasons": []')
    expect(screen.queryByRole('button', { name: 'Approve correction and retest' })).toBeNull()
  })

  it('presents both exact values and sends only the displayed approval token', () => {
    const approve = vi.fn()
    const select = vi.fn()
    render(<TestsPanel state={proposedExpectation} onSelect={select} onApproveCorrection={approve} onProposeCorrection={vi.fn()} />)
    expect(approve).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Original expectation').textContent).toContain('"reasons": []')
    expect(screen.getByLabelText('Proposed expectation').textContent).toContain('"unknown"')
    fireEvent.click(screen.getByRole('button', { name: 'View source src-1#e1' }))
    expect(select).toHaveBeenCalledWith({ kind: 'excerpt', id: 'src-1#e1' })
    fireEvent.click(screen.getByRole('button', { name: 'Approve correction and retest' }))
    expect(approve).toHaveBeenCalledWith('missing-required-fact-unresolved', 'visible-proposal-token')
  })

  it('disables correction actions during a run and Create even if a stale state says ready', () => {
    const approve = vi.fn()
    const { unmount } = render(<TestsPanel state={{ ...proposedExpectation, status: 'running' }} onSelect={vi.fn()} onApproveCorrection={approve} onProposeCorrection={vi.fn()} />)
    expect((screen.getByRole('button', { name: 'Approve correction and retest' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: 'Request another correction' }) as HTMLButtonElement).disabled).toBe(true)
    unmount()
    const create = vi.fn()
    render(<ReviewPanel state={{ ...proposedExpectation, status: 'ready' }} sources={[]} onSelect={vi.fn()} onCreate={create} />)
    const button = screen.getByRole('button', { name: /Create pack/ }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    fireEvent.click(button)
    expect(create).not.toHaveBeenCalled()
    expect(screen.getByText('Blocked by invalid expectations')).toBeTruthy()
  })

  it('withholds Create for a withheld run whose current check passes', () => {
    // The gate is not "the latest check passed": a run withheld for an untraced
    // citation or a failed receipt has a complete, current, passing check and
    // must still not hand the draft to Create.
    const create = vi.fn()
    render(<ReviewPanel state={withheldButPassing} sources={[]} onSelect={vi.fn()} onCreate={create} />)
    const button = screen.getByRole('button', { name: /Create pack/ }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    fireEvent.click(button)
    expect(create).not.toHaveBeenCalled()
  })

  it('shows the handoff target a case asserts beside the disposition', () => {
    // The exact expectation is the pair, and approval is bound to what is shown.
    const target = { kind: 'human-role', name: 'Screening officer' }
    const state = {
      ...proposedExpectation,
      expectationIssues: [{
        ...proposedExpectation.expectationIssues[0]!,
        original: { ...proposedExpectation.expectationIssues[0]!.original, expectedHandoffTarget: target }
      }]
    }
    render(<TestsPanel state={state} onSelect={vi.fn()} onApproveCorrection={vi.fn()} onProposeCorrection={vi.fn()} />)
    // Scoped to the review panel: a disagreeing row's disclosure prints the
    // same words about the pair the check compared, and an unscoped count here
    // would read one surface's line as the other's.
    const review = within(screen.getByLabelText('Expectation review'))
    expect(review.getAllByText(/Expected handoff target/)).toHaveLength(2)
    expect(review.getAllByText(new RegExp(JSON.stringify(target).slice(1, 20)))).not.toHaveLength(0)
  })

  it('discloses both halves of a disagreement, and discloses nothing where the case agrees', () => {
    // The table draws outcomeId ?? kind, which is "unresolved" on both sides
    // here: reasons, handoff and the target carry the whole difference the
    // person is being asked to judge.
    render(<TestsPanel state={disagreeingCase} onSelect={vi.fn()} onApproveCorrection={vi.fn()} onProposeCorrection={vi.fn()} />)
    expect(screen.getByText('What blocked-unresolved disagrees about')).toBeTruthy()
    expect(screen.queryByText('What agreeing-case disagrees about')).toBeNull()
    const disclosure = within(screen.getByText('What blocked-unresolved disagrees about').closest('details')!)
    expect(disclosure.getByLabelText('Expected disposition').textContent).toContain('"unknown"')
    expect(disclosure.getByLabelText('Runtime disposition').textContent).toContain('"no-match"')
    expect(disclosure.getByLabelText('Runtime disposition').textContent).toContain('"requested"')
    expect(disclosure.getByText(/Expected handoff target/).textContent).toContain('Screening officer')
    expect(disclosure.getByText(/Runtime handoff target/).textContent).toContain('null')
  })

  it('holds a refused case to its named refusal, and discloses no pair it never had', () => {
    // `checkCandidate` marks a refusal `passed: false`, so the row badges
    // `disagrees` — but its `actual` is not a pair: this fixture carries the
    // bare disposition of a rehearsal that never completed. A disclosure over
    // it would read `Runtime disposition: null` beside a real expectation, an
    // answer the runtime never gave. The named refusal is what the row shows.
    render(<TestsPanel state={disagreeingCase} onSelect={vi.fn()} onApproveCorrection={vi.fn()} onProposeCorrection={vi.fn()} />)
    expect(screen.queryByText('What refused-case disagrees about')).toBeNull()
    expect(screen.getByText('refused: no completed rehearsal: status refused')).toBeTruthy()
  })
})
