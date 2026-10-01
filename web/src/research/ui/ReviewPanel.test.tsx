import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { isValidElement, type ReactElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ChatAttachment } from '../../chat/store'
import { SourceReader } from '../../documents/SourceReader'
import { INITIAL_STATE, readinessKey, type Citation, type RunState } from '../run'
import { DraftTabs, ReviewPanel } from './DraftPanels'

afterEach(cleanup)

const digest = 'fixture-digest'
const FILE: ChatAttachment = { id: '12345678-1234-1234-1234-000000000001', name: 'Eligibility', text: '',
  document: { id: '12345678-1234-1234-1234-000000000001', digest: `sha256:${'1'.repeat(64)}`, pages: [1, 2], allowPartial: false }, link: { url: 'https://example.org/eligibility' } }
const PAGE = `attachment:${FILE.document!.id}/${FILE.document!.digest}/page/2`
const traced: Citation = { sourceId: 'eligibility', location: PAGE, excerptId: null, url: 'https://example.org/eligibility', traced: true, reason: '', quote: 'At least 1,560 hours.' }
const untraced: Citation = { sourceId: 'rumour', location: 'https://example.org/forum', excerptId: null, url: 'https://example.org/forum', traced: false, reason: 'citation.location is not a page of a document read in this chat' }
/** A conversation draft settled at review over the given citations, as the run records one. */
function settledDraft(citations: Citation[], patch: Partial<RunState> = {}): RunState {
  const state: RunState = { ...INITIAL_STATE, phase: 'review', status: 'ready', citations,
    candidates: [{ revision: 1, producedBy: 'conversation', document: {}, text: '{}', digest, check: { documentDigest: digest, valid: true, diagnostics: [], cases: [] } }], ...patch }
  // A settle records readiness only where it reached ready; a patch saying otherwise stands.
  return { ...state, readiness: patch.readiness ?? readinessKey(state) }
}
const citationsLine = () => screen.getByText('Citations').nextElementSibling!.textContent

describe('the review panel on a conversation draft', () => {
  it('says in words that a draft pack citing nothing rests on what the person said, never "0 of 0"', () => {
    render(<ReviewPanel mode="draft" state={settledDraft([])} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe('This pack cites no source. It rests on what you told the assistant.')
    expect(screen.queryByText(/0 of 0/)).toBeNull()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('never says a draft that declares sources cites nothing, whatever its trace left', () => {
    const declares = settledDraft([], { readiness: '', status: 'stopped' })
    declares.candidates = [{ ...declares.candidates[0]!, document: { sources: [{ id: 'eligibility' }] } }]
    render(<ReviewPanel mode="draft" state={declares} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(screen.queryByText(/cites no source/)).toBeNull()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('says the citations are being checked while they are traced, and offers nothing', () => {
    render(<ReviewPanel mode="draft" state={settledDraft([], { tracing: true })} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe("Checking the draft's citations…")
    expect(screen.queryByText(/cites no source/)).toBeNull()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('names each untraced citation and says what to do beside Create', () => {
    render(<ReviewPanel mode="web-research" state={settledDraft([traced, untraced], { readiness: '', status: 'needs-input' })} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe('1 of 2 traced to a page read in this chat')
    expect(screen.getByRole('heading', { name: 'Citations that could not be traced' })).toBeTruthy()
    expect(screen.getByText('rumour')).toBeTruthy()
    expect(screen.getByText(/Ask the assistant to read the page with read_link, or attach the document/)).toBeTruthy()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('does not offer a web research draft that cites nothing', () => {
    render(<ReviewPanel mode="web-research" state={settledDraft([])} sources={[]} onSelect={vi.fn()} onCreate={vi.fn()} />)
    expect(citationsLine()).toBe('This draft cites no source.')
    expect(screen.queryByText(/0 of 0/)).toBeNull()
    expect(screen.getByText(/A web research draft rests on pages read in this chat/)).toBeTruthy()
    expect((screen.getByRole('button', { name: /Review and create/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('opens a traced page citation in the source reader, at its quote', () => {
    const read = vi.fn()
    const select = vi.fn()
    render(<DraftTabs mode="draft" state={settledDraft([traced])} sources={[]} selection={null} onSelect={select} onCreate={vi.fn()} documents={[FILE]} onRead={read} />)
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Review' }), { button: 0 })
    fireEvent.click(screen.getByRole('button', { name: /^eligibility.*Page 2/ }))
    expect(select).not.toHaveBeenCalled()
    expect(read).toHaveBeenCalledOnce()
    const opened = read.mock.calls[0]![0] as ReactElement<{ reference: unknown; citation: unknown; name: string }>
    expect(isValidElement(opened) && opened.type).toBe(SourceReader)
    expect(opened.props).toMatchObject({ name: 'Eligibility', reference: FILE.document, citation: { page: 2, quote: 'At least 1,560 hours.' } })
  })
})
