/**
 * The one reading of "is this run clean", against the shapes that made it one.
 *
 * Each case here is a sequence a real engine can emit, and the two consumers —
 * the Assistant tab and the Create dialog — get the same answer for each of
 * them by construction now. That is the point of the module: the tab used to
 * disagree with the dialog about `proposal → end → throw`.
 */
import { describe, expect, it } from 'vitest'
import { outcomeOf } from './runOutcome'
import type { AssistantEvent } from './engine'

const proposal: AssistantEvent = { type: 'proposal', document: { a: 1 }, unknowns: [] }
const end: AssistantEvent = { type: 'end' }
const failed = (message: string): AssistantEvent => ({ type: 'error', message })

describe('what a run stands behind', () => {
  it('offers the proposal of a run that ended cleanly', () => {
    const outcome = outcomeOf({ events: [proposal, end] })
    expect(outcome.proposal).toBe(proposal)
    expect(outcome.failure).toBe('')
  })

  it('offers nothing where the run proposed nothing, and calls it no failure', () => {
    // "There is nothing here" is not the same statement as "this went wrong",
    // and a caller that needs a sentence for the first supplies its own.
    const outcome = outcomeOf({ events: [end] })
    expect(outcome.proposal).toBeUndefined()
    expect(outcome.failure).toBe('')
  })

  it('withdraws a proposal an error followed', () => {
    const outcome = outcomeOf({ events: [proposal, failed('the final check did not run'), end] })
    expect(outcome.proposal).toBeUndefined()
    expect(outcome.failure).toBe('the final check did not run')
  })

  it('keeps a proposal an error preceded', () => {
    // A tool call that failed and was retried is an ordinary run, and the
    // document that came out of it is the run's answer.
    const outcome = outcomeOf({ events: [failed('validate refused once'), proposal, end] })
    expect(outcome.proposal).toBe(proposal)
    expect(outcome.failure).toBe('')
  })

  it('withdraws a proposal the run failed after its terminal event', () => {
    // The sequence the Assistant tab used to accept: the failure cannot be on
    // the stream, because one `end` is the contract.
    const outcome = outcomeOf({
      events: [proposal, end],
      failure: 'the session could not be closed'
    })
    expect(outcome.proposal).toBeUndefined()
    expect(outcome.failure).toBe('the session could not be closed')
  })

  it('reports the last error, where a run made several', () => {
    const outcome = outcomeOf({ events: [failed('one'), failed('two'), end] })
    expect(outcome.failure).toBe('two')
  })

  it('prefers the failure the run reported over anything on the stream', () => {
    // A run that put an error on the stream and *then* fell over while
    // unwinding failed twice; the later one is the account of the run.
    const outcome = outcomeOf({ events: [failed('on the stream'), end], failure: 'while unwinding' })
    expect(outcome.failure).toBe('while unwinding')
  })
})
