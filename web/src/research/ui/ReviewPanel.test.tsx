import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { isValidElement, type ReactElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ChatAttachment } from '../../chat/store'
import { SourceReader } from '../../documents/SourceReader'
import { INITIAL_STATE, readinessKey, type Citation, type RunState } from '../run'
import { DraftTabs, ReviewPanel } from './DraftPanels'

afterEach(cleanup)

const digest = 'fixture-digest'
/** The pin and documents the fixtures' citations were traced on, and stand on. */
const BASIS = 'basis-1'
const FILE: ChatAttachment = { id: '12345678-1234-1234-1234-000000000001', name: 'Eligibility', text: '',
  document: { id: '12345678-1234-1234-1234-000000000001', digest: `sha256:${'1'.repeat(64)}`, pages: [1, 2], allowPartial: false }, link: { url: 'https://example.org/eligibility' } }
const PAGE = `attachment:${FILE.document!.id}/${FILE.document!.digest}/page/2`
const traced: Citation = { sourceId: 'eligibility', location: PAGE, excerptId: null, url: 'https://example.org/eligibility', traced: true, reason: '', quote: 'At least 1,560 hours.' }
const untraced: Citation = { sourceId: 'rumour', location: 'https://example.org/forum', excerptId: null, url: 'https://example.org/forum', traced: false, reason: 'citation.location is not a page of a document read in this chat' }
const SAID = 'People with 1,560 hours qualify; fewer do not.'
const CASE = { id: 'at-the-threshold', facts: { work: { hours: '1560' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }, expectationSource: 'you-1', rationale: 'You said 1,560 hours qualify.' }
/** A conversation draft settled at review over the given citations, with one agreeing case grounded in the person's first message. */
function settledDraft(citations: Citation[], patch: Partial<RunState> = {}): RunState {
  const state: RunState = { ...INITIAL_STATE, phase: 'review', status: 'ready', citations, tracedBasis: BASIS, cases: [CASE],
    turns: [{ id: 't1', role: 'user', kind: 'brief', text: SAID, at: '2026-09-30T00:00:00Z' }, { id: 't2', role: 'assistant', kind: 'message', text: 'Drafted.', at: '2026-09-30T00:00:01Z' }],
    candidates: [{ revision: 1, producedBy: 'conversation', document: {}, text: '{}', digest, check: { documentDigest: digest, valid: true, diagnostics: [], cases: [{ id: CASE.id, passed: true, expected: {}, actual: {} }] } }], ...patch }
  // A settle records readiness only where it reached ready; a patch saying otherwise stands.
  return { ...state, readiness: patch.readiness ?? readinessKey(state) }
}
const citationsLine = () => screen.getByText('Citations').nextElementSibling!.textContent

describe('the review panel on a conversation draft', () => {
  it('says in words that a draft pack citing nothing rests on what the person said, never "0 of 0"', () => {
    render(<ReviewPanel basis={BASIS} mode="draft" state={settledDraft([])} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe('This pack cites no source. It rests on what you told the assistant.')
    expect(screen.queryByText(/0 of 0/)).toBeNull()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(false)
  })

  it.each(['draft', 'web-research'] as const)('never says a %s draft that declares sources cites nothing, whatever its trace left', mode => {
    const declares = settledDraft([], { readiness: '', status: 'stopped' })
    declares.candidates = [{ ...declares.candidates[0]!, document: { sources: [{ id: 'eligibility' }] } }]
    render(<ReviewPanel basis={BASIS} mode={mode} state={declares} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(screen.queryByText(/cites no source/)).toBeNull()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('offers nothing once the pin or the documents stand other than the citations were traced on', () => {
    render(<ReviewPanel basis="basis-2" mode="draft" state={settledDraft([])} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('says the citations are being checked while they are traced, and offers nothing', () => {
    render(<ReviewPanel basis={BASIS} mode="draft" state={settledDraft([], { tracing: true })} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe("Checking the draft's citations…")
    expect(screen.queryByText(/cites no source/)).toBeNull()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('names each untraced citation and says what to do beside Create', () => {
    render(<ReviewPanel basis={BASIS} mode="web-research" state={settledDraft([traced, untraced], { readiness: '', status: 'needs-input' })} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe('1 of 2 traced to a page read in this chat')
    expect(screen.getByRole('heading', { name: 'Citations that could not be traced' })).toBeTruthy()
    expect(screen.getByText('rumour')).toBeTruthy()
    expect(screen.getByText(/Ask the assistant to read the page with read_link, or attach the document/)).toBeTruthy()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('does not offer a web research draft that cites nothing', () => {
    render(<ReviewPanel basis={BASIS} mode="web-research" state={settledDraft([])} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe('This draft cites no source.')
    expect(screen.queryByText(/0 of 0/)).toBeNull()
    expect(screen.getByText(/A web research draft rests on pages read in this chat/)).toBeTruthy()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('opens a traced page citation in the source reader, at its quote', () => {
    const read = vi.fn()
    const select = vi.fn()
    render(<DraftTabs basis={BASIS} mode="draft" state={settledDraft([traced])} sources={[]} selection={null} onSelect={select} onCreate={vi.fn()} documents={[FILE]} onRead={read} />)
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Review' }), { button: 0 })
    fireEvent.click(screen.getByRole('button', { name: /^eligibility.*Page 2/ }))
    expect(select).not.toHaveBeenCalled()
    expect(read).toHaveBeenCalledOnce()
    const opened = read.mock.calls[0]![0] as ReactElement<{ reference: unknown; citation: unknown; name: string }>
    expect(isValidElement(opened) && opened.type).toBe(SourceReader)
    expect(opened.props).toMatchObject({ name: 'Eligibility', reference: FILE.document, citation: { page: 2, quote: 'At least 1,560 hours.' } })
  })

  it('offers a saved draft without cases the step that completes it, beside the reason', () => {
    const establish = vi.fn()
    const caseless = settledDraft([], { cases: [], status: 'needs-input', readiness: '' })
    caseless.candidates = [{ ...caseless.candidates[0]!, check: { ...caseless.candidates[0]!.check!, cases: [] } }]
    const { unmount } = render(<ReviewPanel basis={BASIS} mode="draft" state={caseless} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} onEstablishCases={establish} />)
    expect(screen.getByText(/has no test cases written without its rules yet/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Write test cases without the rules' }))
    expect(establish).toHaveBeenCalledOnce()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
    unmount()
    // Not before the saved draft is rechecked, not once it has cases, and never in research.
    for (const [mode, state] of [['draft', { ...caseless, restored: true }], ['draft', settledDraft([])], ['research', caseless]] as const) {
      const { unmount: done } = render(<ReviewPanel basis={BASIS} mode={mode} state={state} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} onEstablishCases={establish} />)
      expect(screen.queryByRole('button', { name: 'Write test cases without the rules' })).toBeNull()
      done()
    }
  })

  it('shows a statement-grounded case in the person\'s words, beside readiness, where Tests is the pack workspace', () => {
    const issue = { id: 'missing-hours', original: { ...CASE, id: 'missing-hours', facts: {}, expectationSource: 'you-1' }, message: 'reasons is empty' }
    const select = vi.fn()
    render(<DraftTabs basis={BASIS} mode="draft" testsPanel={<p>Pack tests</p>} state={settledDraft([], { expectationIssues: [issue] })} sources={[]} selection={null} onSelect={select} onCreate={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Review expectations' }))
    expect(screen.getByRole('tab', { name: 'Review', selected: true })).toBeTruthy()
    expect(screen.getAllByText('Your message 1').length).toBe(2)
    expect(screen.getAllByText(SAID).length).toBe(2)
    expect(screen.getByRole('heading', { name: 'Blocked expectations' })).toBeTruthy()
    // Nothing to open for a message: no link into the Inspector.
    expect(screen.queryByRole('button', { name: 'you-1' })).toBeNull()
    expect(screen.queryByRole('button', { name: /View source you-1/ })).toBeNull()
    expect(select).not.toHaveBeenCalled()
  })

  it('opens a page-grounded case\'s page in the source reader', () => {
    const read = vi.fn()
    const pageCase = { ...CASE, id: 'quoted', expectationSource: PAGE }
    const state = settledDraft([traced], { cases: [pageCase] })
    state.candidates = [{ ...state.candidates[0]!, check: { ...state.candidates[0]!.check!, cases: [{ id: 'quoted', passed: true, expected: {}, actual: {} }] } }]
    render(<DraftTabs basis={BASIS} mode="web-research" testsPanel={<p>Pack tests</p>} state={state} sources={[]} selection={null} onSelect={vi.fn()} onCreate={vi.fn()} documents={[FILE]} onRead={read} />)
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Review' }), { button: 0 })
    fireEvent.click(screen.getByRole('button', { name: 'Page 2' }))
    const opened = read.mock.calls[0]![0] as ReactElement<{ citation: unknown }>
    expect(opened.type).toBe(SourceReader)
    expect(opened.props.citation).toEqual({ page: 2, quote: 'At least 1,560 hours.' })
  })
})
