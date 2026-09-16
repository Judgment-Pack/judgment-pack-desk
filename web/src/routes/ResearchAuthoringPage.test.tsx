import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AssistantEvent } from '../assistant/engine'
import { Ledger, type SourceRecord } from '../research/ledger'
import { INITIAL_STATE, readinessKey, type RunState } from '../research/run'
import type { ResearchRunBinding } from '../research/useResearchRun'

/**
 * The page against a controllable binding: what each run state renders, what
 * Stop and Send and Create do, and what the narrow layout shows. The run
 * itself is `research/run.test.ts`'s; the binding to the assistant slot is
 * `useResearchRun`, mocked here so the page can be driven without a socket.
 */
const binding: ResearchRunBinding & { listeners: Set<() => void>; set(patch: Partial<RunState>): void } = {
  run: null,
  state: INITIAL_STATE,
  ledger: null,
  sources: [],
  blocked: '',
  model: 'a-model',
  researchConfigured: true,
  listeners: new Set(),
  set(patch) {
    binding.state = { ...binding.state, ...patch }
    rerender()
  }
}
let rerender = () => {}
const calls: string[] = []

vi.mock('../research/useResearchRun', () => ({
  MAX_REVISIONS: 4,
  useResearchRun: () => {
    const [, tick] = (globalThis as { __react?: { useState: typeof import('react').useState } }).__react?.useState(0) ?? [0, () => {}]
    rerender = () => tick((n: number) => n + 1)
    return binding
  }
}))

import * as React from 'react'
;(globalThis as { __react?: unknown }).__react = React

import { ResearchAuthoringPage } from './ResearchAuthoringPage'

function fakeRun() {
  return {
    subscribe: () => () => {},
    getSnapshot: () => binding.state,
    running: false,
    start: (brief: string, urls: string[]) => calls.push(`start:${brief}:${urls.join(',')}`),
    send: (text: string) => calls.push(`send:${text}`),
    stop: () => calls.push('stop')
  } as unknown as NonNullable<ResearchRunBinding['run']>
}

function mount(path = '/create-pack/research') {
  const router = createMemoryRouter(
    [
      { path: '/create-pack/research', element: <ResearchAuthoringPage /> },
      { path: '/create-pack', element: <p>Create page received {JSON.stringify(Boolean((globalThis as { __state?: unknown }).__state))}</p> }
    ],
    { initialEntries: [path] }
  )
  render(<RouterProvider router={router} />)
  return router
}

afterEach(() => {
  cleanup()
  calls.length = 0
  binding.state = INITIAL_STATE
  binding.run = null
  binding.ledger = null
  binding.sources = []
  binding.blocked = ''
})

describe('Research and draft', () => {
  it('starts a run from the brief and the URLs, and says why it cannot', () => {
    binding.run = fakeRun()
    binding.blocked = 'No research gateway is configured. Add a research section to the desk-level desk.json.'
    mount()
    expect(screen.getByRole('status').textContent).toContain('No research gateway is configured')
    expect(screen.getByRole('button', { name: 'Start research' }).hasAttribute('disabled')).toBe(true)
    binding.blocked = ''
    act(() => binding.set({}))
    fireEvent.change(screen.getByLabelText(/The decision/), { target: { value: 'Screen FSWP minimum requirements.' } })
    fireEvent.change(screen.getByLabelText(/Read these first/), { target: { value: 'https://www.canada.ca/a\nnot a url\nhttps://laws-lois.justice.gc.ca/b' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start research' }))
    expect(calls).toEqual(['start:Screen FSWP minimum requirements.:https://www.canada.ca/a,https://laws-lois.justice.gc.ca/b'])
  })

  it('shows the conversation, the draft tabs, real activity, and stops a running run', () => {
    binding.run = fakeRun()
    const ledger = new Ledger('s1')
    binding.ledger = ledger
    const event: AssistantEvent = { type: 'tool_call', name: 'read_source', args: { url: 'https://x' } }
    binding.state = {
      ...INITIAL_STATE,
      phase: 'research',
      status: 'running',
      detail: 'Researching sources and drafting.',
      brief: 'b',
      turns: [{ role: 'user', kind: 'brief', text: 'Screen applicants.', at: 't' }],
      events: [event]
    }
    mount()
    expect(screen.getAllByRole('status').map((node) => node.textContent).join(' ')).toContain('Researching sources and drafting.')
    expect(screen.getByText('Screen applicants.')).not.toBeNull()
    expect(screen.getByRole('tab', { name: 'Draft' })).not.toBeNull()
    expect(screen.getByRole('tab', { name: 'Sources' })).not.toBeNull()
    expect(screen.getByRole('tab', { name: 'Tests' })).not.toBeNull()
    expect(screen.getByRole('tab', { name: 'Review' })).not.toBeNull()
    expect(screen.getByLabelText('Message the assistant').hasAttribute('disabled')).toBe(true)
    fireEvent.click(screen.getAllByRole('button', { name: 'Stop' })[0]!)
    expect(calls).toEqual(['stop'])
  })

  it('at review, lists sources with their receipt state, tests with their expectation source, and hands a passing draft to Create', () => {
    binding.run = fakeRun()
    const ledger = new Ledger('s1')
    binding.ledger = ledger
    ledger.open('page', { source: 'read', dialect: 'jina-reader', url: 'https://www.canada.ca/very/long/url/that/keeps/going/and/going/federal-skilled-workers.html' })
    ledger.settle('src-1', {
      response: { text: '{}', result: { kind: 'null' }, receipt: { kind: 'null' }, salts: {} },
      document: { url: 'https://www.canada.ca/very/long/url/that/keeps/going/and/going/federal-skilled-workers.html', title: 'FSWP', text: 'You need 1,560 hours of work.', pageDates: { issued: '2024-12-13' } }
    })
    ledger.cite('src-1', '1,560 hours')
    ledger.verified('src-1', { state: 'verified', at: 't', keyId: 'k' })
    ledger.open('search', { source: 'search', dialect: 'tavily-search', query: 'q' })
    ledger.settle('src-2', { failure: 'the gateway refused: no search source' })
    binding.sources = ledger.sources as SourceRecord[]
    const document = { title: 'FSWP screening', decision: { question: 'Does the applicant meet?' }, outcomes: [{ id: 'meets', label: 'Meets' }], rules: [{ id: 'hours', description: 'Hours', when: { op: 'fact', path: '/work/hours', operator: 'greater-than-or-equal', value: '1560' }, outcome: 'meets', onUnknown: 'escalate', sourceRefs: ['ircc'] }], sources: [{ id: 'ircc', locator: { kind: 'uri', value: 'https://www.canada.ca/very/long/url/that/keeps/going/and/going/federal-skilled-workers.html' }, citation: { location: 'src-1#e1', excerpt: '1,560 hours' } }] }
    binding.state = {
      ...INITIAL_STATE,
      phase: 'review',
      status: 'ready',
      detail: 'Every established case agrees.',
      turns: [{ role: 'user', kind: 'brief', text: 'b', at: 't' }, { role: 'assistant', kind: 'message', text: 'I read the IRCC page.', at: 't' }],
      candidates: [{ revision: 1, document, text: JSON.stringify(document), digest: 'abcdef0123456789', producedBy: 'research', check: { documentDigest: 'abcdef0123456789', valid: true, diagnostics: [], cases: [{ id: 'meets-hours', passed: true, expected: {}, actual: {} }] } }],
      cases: [{ id: 'meets-hours', facts: { work: { hours: '1560' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'meets' }, expectationSource: 'src-1#e1', rationale: 'at the threshold' }],
      unknowns: ['Student work experience is not settled.'],
      citations: [{ sourceId: 'ircc', location: 'src-1#e1', excerptId: 'src-1#e1', url: 'https://www.canada.ca/very/long/url/that/keeps/going/and/going/federal-skilled-workers.html', traced: true, reason: '' }],
      sessions: ['s1']
    }
    // Create reads the readiness the run recorded at `ready`, not the status.
    binding.state = { ...binding.state, readiness: readinessKey(binding.state) }
    const router = mount()
    expect(screen.getByText('I read the IRCC page.')).not.toBeNull()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Sources (2)' }), { button: 0 })
    expect(screen.getByText('receipt verified')).not.toBeNull()
    expect(screen.getByText('retrieval failed')).not.toBeNull()
    expect(screen.getByText('1 excerpt')).not.toBeNull()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Tests (1)' }), { button: 0 })
    expect(screen.getByText('agrees')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'src-1#e1' })).not.toBeNull()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Review' }), { button: 0 })
    expect(screen.getByText('Student work experience is not settled.')).not.toBeNull()
    expect(screen.getByText(/1 of 1 traced/)).not.toBeNull()
    // Sending a message reaches the run; Create hands the draft over with its companions.
    fireEvent.change(screen.getByLabelText('Message the assistant'), { target: { value: 'Why 1,560?' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(calls).toEqual(['send:Why 1,560?'])
    fireEvent.click(screen.getByRole('button', { name: 'Create pack from this draft' }))
    const handed = (router.state.location.state as { research?: { name: string; matrix: { cases: unknown[] }; research: { sources: unknown[] } } } | null)?.research
    expect(router.state.location.pathname).toBe('/create-pack')
    expect(handed?.name).toBe('FSWP screening')
    expect(handed?.matrix.cases).toHaveLength(1)
    expect(handed?.research.sources).toHaveLength(2)
  })

  it('withholds Create while cases disagree, and offers the narrow switch', () => {
    binding.run = fakeRun()
    binding.ledger = new Ledger('s1')
    const matchMedia = window.matchMedia
    window.matchMedia = ((query: string) => ({ matches: true, media: query, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, onchange: null, dispatchEvent: () => false })) as typeof window.matchMedia
    const document = { title: 'T', decision: { question: 'Q' }, outcomes: [], rules: [] }
    binding.state = {
      ...INITIAL_STATE,
      phase: 'review',
      status: 'budget',
      detail: 'The revision budget of 4 is spent with disagreements remaining.',
      candidates: [{ revision: 1, document, text: '{}', digest: 'd', producedBy: 'research', check: { documentDigest: 'd', valid: true, diagnostics: [], cases: [{ id: 'c', passed: false, expected: {}, actual: {} }] } }],
      cases: [{ id: 'c', facts: {}, expectedDisposition: { kind: 'outcome', outcomeId: 'x' }, expectationSource: 'src-1#e1', rationale: '' }]
    }
    try {
      mount()
      expect(screen.getByRole('button', { name: 'Create pack' }).hasAttribute('disabled')).toBe(true)
      expect(screen.getByRole('radiogroup', { name: 'Workspace view' })).not.toBeNull()
      expect(screen.getByLabelText('Message the assistant')).not.toBeNull()
      fireEvent.click(screen.getByRole('radio', { name: 'Draft' }))
      expect(screen.getByRole('tab', { name: 'Review' })).not.toBeNull()
    } finally {
      window.matchMedia = matchMedia
    }
  })
})
