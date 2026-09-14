import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { blockedExpectation, proposedExpectation } from '../__fixtures__/expectationReview'
import { DraftTabs, ReviewPanel, TestsPanel } from './DraftPanels'

afterEach(cleanup)

describe('expectation review UI', () => {
  it('counts the blocked case and opens its review without hiding it in a smaller passed suite', () => {
    render(<DraftTabs state={blockedExpectation} sources={[]} selection={null} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(screen.getByRole('tab', { name: 'Tests (2)' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Review expectations' }))
    expect(screen.getByRole('heading', { name: 'Invalid expectations' })).toBeTruthy()
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
})
