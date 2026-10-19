import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { validDelegatedTask, type DelegatedTask } from '@judgment-pack/harness/delegationContract'
import { DelegatedTaskDetails, TaskReportView, taskMarks } from './DelegatedTaskDetails'
afterEach(cleanup)

const id = () => crypto.randomUUID()
const task = (more: Partial<DelegatedTask> = {}): DelegatedTask => ({
  id: id(), parentRunId: id(), title: 'Review the pack', objective: 'Review the expense pack for gaps.', profile: 'review', state: 'complete',
  createdAt: new Date().toISOString(), modelRequests: 3, toolCalls: 2, attempts: 1, steps: [{ name: 'get_schema', state: 'complete' }], ...more
})
const REPORT = {
  summary: 'Two gaps in the receipt rule.',
  findings: [
    { text: 'A claim of exactly 50 USD with no receipt is not covered.', severity: 'gap' as const, citations: ['entry-1'] },
    { text: 'The finance handoff has no target.', severity: 'inconsistency' as const }
  ],
  citations: [{ id: 'entry-1', quote: 'above 50 USD is approved only when a receipt is attached' }],
  openQuestions: [{ kind: 'question' as const, text: 'Should a claim of exactly 50 USD need a receipt?' }, { kind: 'proposal' as const, text: 'A corrected rule was drafted.', ref: 'proposal-1' }],
  confidence: 'medium' as const,
  notDone: ['Did not run the saved tests.']
}

describe('what a delegated task ran under', () => {
  it('shows the profile\'s budget, the model, the references, the task it continues and what it cost', () => {
    const detailed = task({
      model: 'a-cheaper-model', budget: { modelRequests: 6, toolCalls: 6, milliseconds: 120_000 }, references: ['entry-1', 'document-2'],
      resumes: id(), tokens: { inputTokens: 1200, outputTokens: 300 }, firstByteMs: 840, durationMs: 9100
    })
    expect(validDelegatedTask(detailed)).toBe(true)
    render(<DelegatedTaskDetails task={detailed} />)
    const facts = within(screen.getByText('Model').closest('dl')!)
    expect(facts.getByText('a-cheaper-model')).toBeTruthy()
    expect(facts.getByText('Requests 6 · tool calls 6 · 120 s')).toBeTruthy()
    expect(facts.getByText('Requests 3 · tool calls 2')).toBeTruthy()
    expect(facts.getByText('in 1200, out 300')).toBeTruthy()
    expect(facts.getByText('0.8 s')).toBeTruthy()
    expect(facts.getByText('9.1 s')).toBeTruthy()
    expect(facts.getByText('entry-1')).toBeTruthy()
    expect(facts.getByText('document-2')).toBeTruthy()
    expect(facts.getByText(detailed.resumes!)).toBeTruthy()
  })

  it('shows where the whole findings are kept when the result is their head, or a summary of them', () => {
    const more = { readWith: 'read_window' as const, id: 'result-research-abc', cursor: 'c12', totalCharacters: 48_000 }
    const head = task({ result: 'The start of the findings.', more })
    expect(validDelegatedTask(head)).toBe(true)
    render(<DelegatedTaskDetails task={head} />)
    expect(screen.getByText(/This is the start of 48000 characters of findings, kept in this chat's context as result-research-abc; read the rest from c12\./)).toBeTruthy()
    cleanup()
    render(<DelegatedTaskDetails task={{ ...head, summarized: true }} />)
    expect(screen.getByText(/This is a summary of 48000 characters of findings/)).toBeTruthy()
  })

  it('names no budget, model or reference the task record does not carry', () => {
    render(<DelegatedTaskDetails task={task({ modelRequests: 0, toolCalls: 0, state: 'queued' })} />)
    for (const name of ['Model', 'Budget', 'References', 'Continues task', 'Used', 'Tokens']) expect(screen.queryByText(name)).toBeNull()
  })
})

describe('the marks a task carries', () => {
  it('says a task is partial, reused or retried, and how long the retry waited', () => {
    const failure = { version: 1 as const, code: 'provider-unavailable', source: 'model' as const, outcome: 'not-started' as const }
    const marked = task({ partial: true, reused: id(), retried: { failure: failure as never, waitMs: 1500 } })
    expect(taskMarks(marked)).toEqual(['Partial: stopped at its time limit', 'Reused: the result of an identical earlier task', 'Retried once after 1.5 s'])
    expect(taskMarks(task())).toEqual([])
    render(<DelegatedTaskDetails task={marked} />)
    expect(screen.getByText('Partial: stopped at its time limit')).toBeTruthy()
    expect(screen.getByText(marked.reused!)).toBeTruthy()
  })
})

describe('a child\'s report in its profile\'s shape', () => {
  it('shows a review\'s findings with their severity and citations, the sources, open questions as data, confidence and what was not done', () => {
    render(<DelegatedTaskDetails task={task({ report: REPORT })} />)
    expect(screen.getByText('Two gaps in the receipt rule.')).toBeTruthy()
    expect(screen.getByText('Gap')).toBeTruthy()
    expect(screen.getByText('Inconsistency')).toBeTruthy()
    expect(screen.getByText('Cites: [1]')).toBeTruthy()
    expect(screen.getByText('above 50 USD is approved only when a receipt is attached')).toBeTruthy()
    // A question for the person is shown as the data it is; the task is not a failure.
    expect(screen.getByText('Should a claim of exactly 50 USD need a receipt?')).toBeTruthy()
    expect(screen.getByText('Question')).toBeTruthy()
    expect(screen.getByText('Kept at proposal-1')).toBeTruthy()
    expect(screen.getByText('Confidence: Medium')).toBeTruthy()
    expect(screen.getByText('Did not run the saved tests.')).toBeTruthy()
    expect(screen.queryByText('Failed')).toBeNull()
  })

  it('shows an author-test\'s check, status, case and smallest fix', () => {
    render(<TaskReportView report={{ summary: 'One case fails.', findings: [{ text: 'The declined case is approved.', check: 'experimental_evaluate', status: 'failed', case: 'c-2', fix: 'Raise the receipt threshold check to 50.' }], citations: [], openQuestions: [], confidence: 'unstated', notDone: [] }} />)
    expect(screen.getByText('experimental_evaluate')).toBeTruthy()
    expect(screen.getByText('Failed')).toBeTruthy()
    expect(screen.getByText('Case c-2')).toBeTruthy()
    expect(screen.getByText('Smallest fix: Raise the receipt threshold check to 50.')).toBeTruthy()
    expect(screen.getByText('Confidence: Not stated')).toBeTruthy()
  })

  it('shows what a report says as text and never as markup', () => {
    const { container } = render(<TaskReportView report={{ ...REPORT, summary: '<img src=x onerror=alert(1)> **bold**' }} />)
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('<img src=x onerror=alert(1)> **bold**')).toBeTruthy()
  })
})

describe('a running task', () => {
  it('shows the prose it has written so far, under its task', () => {
    render(<DelegatedTaskDetails task={task({ state: 'running', modelRequests: 1 })} progress="I am checking the receipt rule against the saved cases" />)
    expect(within(screen.getByRole('region', { name: 'Written so far' })).getByText(/I am checking the receipt rule/)).toBeTruthy()
  })
})
