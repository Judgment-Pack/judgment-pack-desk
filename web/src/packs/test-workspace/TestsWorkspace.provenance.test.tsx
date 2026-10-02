import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { TestsContent } from './TestsWorkspace'
import { emptySuite, importMatrix, type TestSuite } from './model'
import type { PackDocument } from '../../mcp/types'

const fixture = vi.hoisted(() => ({ call: vi.fn(), tools: vi.fn(), suite: undefined as unknown as TestSuite }))
vi.mock('../../mcp/McpProvider', () => ({ useMcp: () => ({status: 'ready', client}) }))
const client = { callTool: fixture.call, listTools: fixture.tools }
vi.mock('../../research/checkCandidate', async original => ({...await original<typeof import('../../research/checkCandidate')>(), digestOf: async () => 'digest'}))
vi.mock('../../files/queries', () => ({ useFileListing: () => ({data: {root: 'project', files: []}}) }))
vi.mock('./store', () => ({ useTestStorage: () => ({suite: fixture.suite, query: {isSuccess: true}, update: async (change: (suite: TestSuite) => TestSuite) => { fixture.suite = await change(fixture.suite); return fixture.suite }}), updateTests: vi.fn() }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

it('sends the Desk AI origin to the runtime when running a saved proposal suite', async () => {
 const expected = {kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: {state: 'none'}}
 fixture.suite = {...emptySuite(), cases: importMatrix({cases: [{id: 'proposed', origin: 'manual', facts: {}, expectedDisposition: expected}]}, 'ai')}
 fixture.tools.mockResolvedValue({tools: [{name: 'experimental_test_cases'}]})
 fixture.call.mockResolvedValue({structuredContent: {status: 'passed', summary: {total: 1, passed: 1}, packs: []}})
 render(<QueryClientProvider client={new QueryClient()}><Tooltip.Provider><MemoryRouter><TestsContent owner="pack" document={{version: '1', outcomes: []} as unknown as PackDocument} text="{}" title="Example" /></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 const button = screen.getByRole('button', {name: 'Run tests'}) as HTMLButtonElement
 await waitFor(() => expect(button.disabled).toBe(false))
 fireEvent.click(button)
 await waitFor(() => expect(fixture.call).toHaveBeenCalled())
 const call = fixture.call.mock.calls.find(([args]) => args.name === 'experimental_test_cases')![0]
 expect(JSON.parse(call.arguments.matrix).cases[0].origin).toBe('ai')
 await waitFor(() => expect(fixture.suite.runs).toHaveLength(1))
})
