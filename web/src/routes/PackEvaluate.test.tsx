import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient, type ToolHandler } from '../testing/harness'
import { consoleSnapshot, forgetConsole } from '../shell/consoleLog'
import { PackEvaluate } from './PackEvaluate'
import type { PackRunSnapshot } from '../packs/runContext'

const payload = {
  status: 'ok', experimental: true, rehearsal: true, packId: 'https://example.test/pack',
  packVersion: '1', disposition: { kind: 'outcome', outcomeId: 'deny', reasons: [] }, trace: []
}
const answer = { text: JSON.stringify(payload) }
afterEach(() => { cleanup(); forgetConsole() })

function draw(evaluate: ToolHandler, packBytes?: string) {
  const stub = stubClient({
    list_packs: () => ({ text: JSON.stringify({ packs: [{ id: 'sample', matrix: true }] }) }),
    experimental_evaluate: evaluate,
    ...(packBytes === undefined ? {} : { get_pack: () => ({ text: packBytes }) })
  })
  const router = createMemoryRouter([{ path: '/packs/:packId/evaluate', element: <PackEvaluate /> }, { path: '/packs/:packId', element: <p>Explanation destination</p> }], {
    initialEntries: ['/packs/sample/evaluate']
  })
  const queryClient = testQueryClient()
  render(<QueryClientProvider client={queryClient}><McpContext.Provider value={connected({ client: stub.client, rehearsalSupported: true })}>
    <RouterProvider router={router} />
  </McpContext.Provider></QueryClientProvider>)
  return { router, calls: stub.calls, queryClient }
}

describe('the pack testing workspace', () => {
  it('binds Explain on map to the exact submitted pack bytes and retains that run on return', async () => {
    const packBytes = '{"title":"Snapshot pack","version":"1","decision":{"question":"Approve?"}}\n'
    const { calls, router, queryClient } = draw(() => answer, packBytes)
    await screen.findByRole('heading', { name: 'Snapshot pack' })
    fireEvent.change(screen.getByLabelText('Facts'), { target: { value: '{"amount":"20"}' } })
    fireEvent.click(screen.getByRole('button', { name: 'Run evaluation' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Explain on map' }))
    await screen.findByText('Explanation destination')
    expect(calls.find(c => c.name === 'experimental_evaluate')!.args).toEqual({ pack: packBytes, facts: '{"amount":"20"}', rehearsal: true })
    const saved = queryClient.getQueryData<PackRunSnapshot>(['pack-explanation', 'sample'])!
    expect(saved.packBytes).toBe(packBytes)
    expect(new URLSearchParams(router.state.location.search).get('run')).toBe(saved.id)
    await act(async () => { await router.navigate('/packs/sample/evaluate') })
    await screen.findByRole('tab', { name: 'Outcome & trace' })
    expect((screen.getByLabelText('Facts') as HTMLTextAreaElement).value).toBe('{"amount":"20"}')
  })
  it('preserves edits made during a run, labels the old result, and sends only submitted inputs', async () => {
    let finish!: (value: typeof answer) => void
    const { calls } = draw(() => new Promise((resolve) => { finish = resolve }))
    const facts = screen.getByLabelText('Facts') as HTMLTextAreaElement
    fireEvent.change(facts, { target: { value: '{"amount":10}' } })
    fireEvent.click(screen.getByRole('button', { name: 'Run evaluation' }))
    await waitFor(() => expect(finish).toBeDefined())
    fireEvent.change(facts, { target: { value: '{"amount":20}' } })
    await act(async () => finish(answer))
    await screen.findByText(/Inputs changed since this result/)
    expect(facts.value).toBe('{"amount":20}')
    const call = calls.find((entry) => entry.name === 'experimental_evaluate')!
    expect(call.args).toEqual({ pack_id: 'sample', facts: '{"amount":10}', rehearsal: true })
    expect(consoleSnapshot().filter((entry) => entry.channel === 'calls').map((entry) => entry.text)).toEqual([
      'Running pack evaluation…', 'Pack evaluation completed. Results are available in Test.'
    ])
    fireEvent.click(screen.getByRole('button', { name: 'Restore last run inputs' }))
    expect(facts.value).toBe('{"amount":10}')
    expect(screen.queryByText(/Inputs changed since this result/)).toBeNull()
  })

  it('keeps invalid input local and offers saved cases separately from exploratory runs', async () => {
    const { calls } = draw(() => answer)
    fireEvent.change(screen.getByLabelText('Facts'), { target: { value: '{' } })
    expect((screen.getByRole('button', { name: 'Run evaluation' }) as HTMLButtonElement).disabled).toBe(true)
    expect(calls.some((call) => call.name === 'experimental_evaluate')).toBe(false)
    await screen.findByRole('link', { name: 'Saved cases' })
  })

  it('does not attach a late result to a different pack', async () => {
    let finish!: (value: typeof answer) => void
    const { router } = draw(() => new Promise((resolve) => { finish = resolve }))
    fireEvent.click(screen.getByRole('button', { name: 'Run evaluation' }))
    await waitFor(() => expect(finish).toBeDefined())
    await act(async () => { await router.navigate('/packs/another/evaluate') })
    await act(async () => finish(answer))
    expect(screen.queryByRole('tab', { name: 'Outcome & trace' })).toBeNull()
    expect((screen.getByLabelText('Facts') as HTMLTextAreaElement).value).toBe('{}')
  })
})
