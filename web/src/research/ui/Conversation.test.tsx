import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { INITIAL_STATE, type RunState } from '../run'
import { Conversation } from './Conversation'

afterEach(cleanup)

const failedInValidation: RunState = {
  ...INITIAL_STATE,
  phase: 'cases',
  status: 'failed',
  detail: 'Expectations could not be validated: the runtime connection dropped',
  candidates: [{ revision: 1, producedBy: 'research', document: null, text: '{}', digest: 'fixture-digest' }],
  heldProposal: {
    candidateDigest: 'fixture-digest',
    admitted: [{ id: 'meets-hours', facts: {}, expectedDisposition: null, expectationSource: 'src-1#e1', rationale: 'at the threshold' }],
    dropped: [],
    unknowns: []
  }
}

describe('the research conversation', () => {
  it('offers the retry only while a proposal is held for the draft on hand', () => {
    const retry = vi.fn()
    render(<Conversation state={failedInValidation} onSend={vi.fn()} onStop={vi.fn()} onRetryValidation={retry} />)
    fireEvent.click(screen.getByRole('button', { name: 'Retry validation' }))
    expect(retry).toHaveBeenCalledTimes(1)
    cleanup()
    // Nothing held, and a hold from an earlier draft, are both a reviewer's
    // question rather than a retry, so neither offers one.
    render(<Conversation state={{ ...failedInValidation, heldProposal: null }} onSend={vi.fn()} onStop={vi.fn()} onRetryValidation={retry} />)
    expect(screen.queryByRole('button', { name: 'Retry validation' })).toBeNull()
    cleanup()
    render(<Conversation state={{ ...failedInValidation, candidates: [{ ...failedInValidation.candidates[0]!, digest: 'other' }] }} onSend={vi.fn()} onStop={vi.fn()} onRetryValidation={retry} />)
    expect(screen.queryByRole('button', { name: 'Retry validation' })).toBeNull()
    expect(retry).toHaveBeenCalledTimes(1)
  })
})
