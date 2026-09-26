import { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { TestAssistant } from './TestAssistant'
import { emptySuite, importMatrix, type TestSuite } from './model'
import type { TestProposal } from './proposals'
const fixture = vi.hoisted(() => ({
  prompts: [] as string[],
  replies: [] as unknown[],
  checks: 0,
  hold: false,
  calls: [] as string[],
}))
vi.mock('../../assistant/useAssistantSlot', async importOriginal => ({
  ...await importOriginal<typeof import('../../assistant/useAssistantSlot')>(),
  useAssistantSlot: () => ({
    state: 'configured', keyStatus:'success', keyPresent:true,
    endpoint: { url: 'fixture', kind: 'openai-compatible', models: ['fixture'], model: 'fixture' },
    engine: 'vercel',
    thinking: 'off',
  }),
}))
vi.mock('../../assistant/pickedModel', () => ({
  usePickedModel: () => ({ model: 'fixture', pick: vi.fn() }),
}))
vi.mock('../../files/queries', () => ({ useFileListing: () => ({ data: { root: '/fixture' } }) }))
vi.mock('../../config/DeskConfigProvider', () => ({
  useEffectiveConfig: () => ({ config: { research: {} } }),
}))
vi.mock('../../chat/MessageRenderer', () => ({
  MessageRenderer: ({ text }: { text: string }) => <div>{text}</div>,
}))
vi.mock('../../assistant/useAssistantRun', async () => {
  const React = await import('react')
  return {
    useAssistantRun: () => {
      const [state, set] = React.useState<{ status: 'idle' | 'running' | 'finished'; events: unknown[] }>({
        status: 'idle',
        events: [],
      })
      const start = React.useCallback((prompt: string) => {
        fixture.prompts.push(prompt)
        set({ status: 'running', events: [] })
        const document = fixture.replies.shift()
        queueMicrotask(() =>
          set({
            status: 'finished',
            events: [
              { type: 'message', text: 'Designed a suite.' },
              { type: 'proposal', document, unknowns: [] },
              { type: 'end' },
            ],
          }),
        )
      }, [])
      const stop = React.useCallback(() => set((s) => ({ ...s, status: 'finished' })), [])
      return { ...state, start, stop, failure: undefined }
    },
  }
})
vi.mock('../../mcp/McpProvider', () => ({
  useMcp: () => ({
    status: 'ready',
    client: {
      callTool: async ({ name, arguments: args }: { name: string; arguments: { matrix: string } }) => {
        fixture.calls.push(name)
        const envelope = { contractVersion: '1', specVersion: '0.2.0-draft' }
        if (name === 'experimental_get_test_matrix_contract')
          return {
            structuredContent: {
              ...envelope,
              contract: { matrixVersion: '3', rowMembers: ['id', 'facts', 'focus', 'expectedDisposition'] },
            },
          }
        fixture.checks++
        if (fixture.hold) return new Promise(() => {})
        const matrix = JSON.parse(args.matrix)
        const results = matrix.cases.map((c: { id: string; name?: string }, index: number) => ({
          index,
          id: c.id,
          status: c.name ? 'invalid' : 'valid',
          findings: c.name
            ? [
                {
                  code: 'MATRIX-MEMBER',
                  path: `/cases/${index}/name`,
                  message: 'Unsupported name; use focus.',
                },
              ]
            : [],
        }))
        return {
          structuredContent: {
            ...envelope,
            status: results.some((r: { status: string }) => r.status === 'invalid') ? 'invalid' : 'valid',
            findings: [],
            results,
          },
        }
      },
    },
  }),
}))
let saved: TestSuite,
  review = vi.fn()
const expected = { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } }
const good = {
  matrixVersion: '3',
  cases: Array.from({ length: 12 }, (_, i) => ({
    id: `case-${i}`,
    facts: { index: i },
    expectedDisposition: expected,
    focus: `Case ${i}`,
  })),
}
const bad = { ...good, cases: good.cases.map((c) => ({ ...c, name: c.focus })) }
function Harness({ digest = 'pack', request = { id: 1, text: 'Design twelve cases' } }: { digest?: string; request?: { id:number; text:string; additionsOnly?:boolean; coverageRunId?:string } }) {
  const [suite, set] = useState(saved)
  const checkpoint = async (record: TestProposal, messages?: NonNullable<TestSuite['messages']>) => {
    saved = {
      ...saved,
      proposals: [...(saved.proposals ?? []).filter((p) => p.id !== record.id), record],
      messages: messages ? [...(saved.messages ?? []), ...messages] : saved.messages,
    }
    set(saved)
  }
  return (
    <TestAssistant
      document="{}"
      packDigest={digest}
      suite={suite}
      sources={[]}
      onSource={vi.fn()}
      onRemoveSource={vi.fn()}
      onReview={review}
      onCheckpoint={checkpoint}
      request={request}
    />
  )
}
beforeEach(() => {
  saved = emptySuite()
  review = vi.fn()
  fixture.prompts = []
  fixture.replies = []
  fixture.checks = 0
  fixture.hold = false
  fixture.calls = []
})
afterEach(cleanup)
it('repairs a rejected 12-case suite, persists both attempts and reopens proposals after reload without saving cases', async () => {
  fixture.replies = [bad, good]
  const view = render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await waitFor(() => expect(screen.getAllByRole('button', { name: 'Review cases' })).toHaveLength(1))
  expect(fixture.prompts).toHaveLength(2)
  expect(fixture.prompts[1]).toContain('/cases/0/name')
  expect(saved.cases).toHaveLength(0)
  expect(saved.proposals?.[0]?.attempts).toHaveLength(2)
  expect(saved.messages?.at(-1)?.text).toContain('ready for review')
  view.unmount()
  render(<Harness />)
  expect(screen.getAllByRole('button', { name: 'Review cases' })).toHaveLength(1)
  fireEvent.click(screen.getAllByRole('button', { name: 'Review cases' })[0]!)
  await waitFor(() => expect(review).toHaveBeenCalledTimes(1))
  expect(review.mock.calls[0]?.[0]?.id).toBe(saved.proposals?.[0]?.id)
  expect(fixture.checks).toBe(3)
  expect(fixture.prompts).toHaveLength(2)
  expect(
    fixture.calls.every(
      (name) =>
        name === 'experimental_get_test_matrix_contract' || name === 'experimental_validate_test_matrix',
    ),
  ).toBe(true)
})
it('retains every failed attempt without displaying ready or review controls', async () => {
  fixture.replies = [bad, bad, bad]
  render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await waitFor(() => expect(saved.messages?.at(-1)?.text).toContain('could not resolve'))
  expect(saved.proposals?.[0]?.state).toBe('blocked')
  expect(saved.proposals?.[0]?.attempts).toHaveLength(3)
  expect(screen.queryByRole('button', { name: 'Review cases' })).toBeNull()
  expect(saved.cases).toHaveLength(0)
})
it('Stop cancels a waiting validation without launching a correction', async () => {
  fixture.replies = [bad]
  fixture.hold = true
  render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await waitFor(() => expect(fixture.checks).toBe(1))
  fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
  await waitFor(() => expect(saved.proposals?.[0]?.state).toBe('stopped'))
  expect(fixture.prompts).toHaveLength(1)
  expect(screen.queryByRole('button', { name: 'Review cases' })).toBeNull()
})

it('keeps each proposal reference under its response after another response and a reload', async () => {
  fixture.replies = [good]
  const view = render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Review cases' })).toBeTruthy())
  expect(saved.messages?.at(-1)?.proposalId).toBe(saved.proposals?.[0]?.id)
  saved.messages!.push(
    { role: 'user', text: 'Explain the boundaries', at: '2026-09-25T00:00:00Z' },
    { role: 'assistant', text: 'Boundary explanation', at: '2026-09-25T00:00:01Z' },
  )
  saved.proposals![0]!.savedCases = { 'case-0': 'receipt' }
  view.unmount()
  render(<Harness />)
  const reference = screen.getByRole('button', { name: 'Review cases' })
  expect(reference.closest('[class*="artifact"]')?.parentElement?.parentElement?.textContent).toContain(
    'Designed a suite.',
  )
  expect(reference.closest('[class*="artifact"]')?.parentElement?.parentElement?.textContent).not.toContain(
    'Boundary explanation',
  )
  expect(screen.getByText('12 cases · 1 saved')).toBeTruthy()
})

it('retains gap-filling constraints and presents additional cases for review without saving', async()=>{
 saved.cases=importMatrix({...good,cases:[{...good.cases[0]!,id:'existing-case'}]},'manual')
 saved.runs=[{id:'coverage',packDigest:'pack',cases:structuredClone(saved.cases),report:{packs:[{coverage:[{probe:'reason:unknown',status:'missing'}]}]}} as TestSuite['runs'][number]]
 fixture.replies=[good]
 render(<Harness request={{id:2,text:'Design additional tests for uncovered behavior.',additionsOnly:true,coverageRunId:'coverage'}}/>)
 fireEvent.click(screen.getByRole('button',{name:'Send'}))
 await screen.findByRole('button',{name:'Review cases'})
 expect(fixture.prompts[0]).toContain('reason:unknown')
 expect(fixture.prompts[0]).toContain('existing-case')
 expect(saved.cases.map(c=>c.id)).toEqual(['existing-case'])
 expect(saved.proposals?.[0]?.additionsOnly).toBe(true)
 expect(saved.proposals?.[0]?.coverageRunId).toBe('coverage')
})
it('does not send a stale coverage request to the model',async()=>{
 render(<Harness request={{id:2,text:'Fill unknown coverage',additionsOnly:true,coverageRunId:'missing-run'}}/>)
 fireEvent.click(screen.getByRole('button',{name:'Send'}))
 await screen.findByText('Coverage changed. Run the current suite and choose Design missing tests again.')
 expect(fixture.prompts).toHaveLength(0)
 expect(saved.proposals??[]).toHaveLength(0)
})
