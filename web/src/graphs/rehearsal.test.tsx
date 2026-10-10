import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { GraphRehearsal, rehearseGraph } from './GraphRehearsal'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const raw = '{"id":"onboarding","answer":{"command":"experimental graph evaluate","status":"evaluated","kind":"non-normative-runtime-convention","experimental":true,"rehearsal":true,"label":"composite <runtime>  label","conformanceClaimReference":"CONFORMANCE.md","disposition":{"number":1.0,"text":"\\u0026"},"nodes":[{"disposition":{"kind":"outcome"},"factFeeds":[],"evidenceFeeds":[],"trace":{"steps":[]}}],"handoffs":[]}}'

it('asks only when chosen, shows labels verbatim, and preserves all runtime bytes', async () => {
  vi.mocked(deskFetch).mockResolvedValue(new Response(raw))
  const { unmount } = render(<GraphRehearsal graphId="onboarding" />)
  expect(deskFetch).not.toHaveBeenCalled()
  expect(screen.getByText(/Try supplied facts/).textContent).toContain('creates no decision audit record')
  expect(screen.getByText(/Inputs and results on this page/).textContent).toContain('retained with the conversation')
  const inputs = '{"node":{"facts":{"number":1.0,"text":"$(touch nope)"}}}'
  fireEvent.change(screen.getByLabelText('Inputs by node id'), { target: { value: inputs } })
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  await screen.findByText('evaluated')
  expect(screen.getByText(/Results are shown separately from the current graph/)).toBeTruthy()
  expect(deskFetch).toHaveBeenCalledWith('/api/graphs/evaluate?id=onboarding', expect.objectContaining({ method: 'POST', body: inputs, signal: expect.any(AbortSignal) }))
  expect(screen.getByText('composite <runtime>  label', { exact: true, collapseWhitespace: false }).getAttribute('lang')).toBe('en')
  expect(screen.getByText('non-normative-runtime-convention').getAttribute('lang')).toBe('en')
  expect(screen.getByText('CONFORMANCE.md').getAttribute('lang')).toBe('en')
  expect(document.querySelector('.graph-labels')?.textContent).toContain('rehearsal true')
  expect(document.querySelector('.graph-labels')?.textContent).toContain('experimental true')
  const pre = document.querySelector('pre')!
  expect(pre.textContent).toBe(raw.slice(raw.indexOf('"answer":') + 9, -1))
  expect(pre.getAttribute('lang')).toBe('en')
  const signal = vi.mocked(deskFetch).mock.calls[0][1]!.signal!
  unmount()
  expect(signal.aborted).toBe(true)
  render(<GraphRehearsal graphId="onboarding" />)
  expect((screen.getByLabelText('Inputs by node id') as HTMLTextAreaElement).value).toBe('{}')
  expect(document.querySelector('pre')).toBeNull()
  expect(deskFetch).toHaveBeenCalledTimes(1)
})

it('refuses absent or false rehearsal and shows runtime refusals as errors', async () => {
  for (const replacement of ['', '"rehearsal":false,']) {
    vi.mocked(deskFetch).mockResolvedValueOnce(new Response(raw.replace('"rehearsal":true,', replacement)))
    await expect(rehearseGraph('g', '{}')).rejects.toThrow('rehearsal: true')
  }
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response(JSON.stringify({ error: 'screening: unknown flag: --rehearsal' }), { status: 502 }))
  render(<GraphRehearsal graphId="g" />)
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  expect((await screen.findByRole('alert')).textContent).toContain('screening: unknown flag: --rehearsal')
  expect(screen.queryByText('evaluated')).toBeNull()
})

it('bounds UTF-8 inputs and refuses non-objects before asking', async () => {
  for (const inputs of ['null', '[]', '1', '"facts"', '{}{}', '{', '{"x":"' + 'é'.repeat(2 * 1024 * 1024) + '"}']) {
    await expect(rehearseGraph('g', inputs)).rejects.toThrow()
  }
  expect(deskFetch).not.toHaveBeenCalled()
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response(raw))
  await rehearseGraph('id / &', '{}')
  expect(deskFetch).toHaveBeenCalledWith('/api/graphs/evaluate?id=id%20%2F%20%26', expect.anything())
})

it('aborts on leaving and clears a result when inputs change', async () => {
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response(raw))
  const { unmount } = render(<GraphRehearsal graphId="g" />)
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  await screen.findByText('evaluated')
  fireEvent.change(screen.getByLabelText('Inputs by node id'), { target: { value: '{"other":{}}' } })
  expect(document.querySelector('pre')).toBeNull()
  vi.mocked(deskFetch).mockImplementationOnce(() => new Promise(() => {}))
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  const signal = vi.mocked(deskFetch).mock.calls[1][1]!.signal!
  unmount()
  expect(signal.aborted).toBe(true)
})

it('refuses an answer from another command', async () => {
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response(raw.replace('experimental graph evaluate', 'experimental graph explain')))
  render(<GraphRehearsal graphId="g" />)
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  expect((await screen.findByRole('alert')).textContent).toBe('The runtime did not return rehearsal: true.')
  expect(screen.queryByText('evaluated')).toBeNull()
})

it('lists runtime members verbatim under their own names', async () => {
  const answer = '{"command":"experimental graph evaluate","status":"evaluated","rehearsal":true,"disposition":{ "number":1.0,"text":"\\u0026" },"nodes":[{"node":"screening","disposition":{"outcomeId":"clear"},"factFeeds":[ { "from":"screening","pointer":"/screening/status","injected":true,"value":"clear" } ],"evidenceFeeds":[{"from":"screening","requirement":"screening-outcome","state":"present"}],"trace":{"steps":[{"n":1e2}]}},{"node":"onboarding","disposition":{"outcomeId":"approve"}}],"handoffs":[{"target":"owner","n":9007199254740993}]}'
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response('{"answer":' + answer + '}'))
  render(<GraphRehearsal graphId="g" />)
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  await screen.findByText('evaluated')
  const items = Array.from(document.querySelectorAll('li')).filter(li => li.querySelector(':scope > code'))
  expect(items.map(li => li.textContent)).toEqual([
    'disposition: { "number":1.0,"text":"\\u0026" }',
    'node: "screening"',
    'disposition: {"outcomeId":"clear"}',
    'factFeeds: [ { "from":"screening","pointer":"/screening/status","injected":true,"value":"clear" } ]',
    'evidenceFeeds: [{"from":"screening","requirement":"screening-outcome","state":"present"}]',
    'trace: {"steps":[{"n":1e2}]}',
    'node: "onboarding"',
    'disposition: {"outcomeId":"approve"}',
    'handoffs: [{"target":"owner","n":9007199254740993}]',
  ])
  for (const item of items) expect(item.querySelector('code')?.getAttribute('lang')).toBe('en')
  expect(document.querySelector('pre')?.textContent).toBe(answer)
})

it('does not invent absent runtime members', async () => {
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response('{"answer":{"command":"experimental graph evaluate","status":"evaluated","rehearsal":true}}'))
  render(<GraphRehearsal graphId="g" />)
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  await screen.findByText('evaluated')
  expect(document.querySelectorAll('li')).toHaveLength(0)
  for (const name of ['disposition', 'nodes', 'node', 'factFeeds', 'evidenceFeeds', 'trace', 'handoffs']) {
    expect(screen.queryByText(name, { exact: true })).toBeNull()
  }
})

it('does not mark Desk refusals as English', async () => {
  const { container } = render(<div lang="fr"><GraphRehearsal graphId="g" /></div>)
  fireEvent.change(screen.getByLabelText('Inputs by node id'), { target: { value: '[]' } })
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  const alert = await screen.findByRole('alert')
  expect(alert.closest('[lang]')).toBe(container.firstChild)
  expect(alert.getAttribute('lang')).toBeNull()
  expect(deskFetch).not.toHaveBeenCalled()
})
