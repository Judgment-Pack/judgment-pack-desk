import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { GraphRehearsal, rehearseGraph } from './GraphRehearsal'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const raw = '{"id":"onboarding","answer":{"command":"experimental graph evaluate","status":"evaluated","kind":"non-normative-runtime-convention","experimental":true,"rehearsal":true,"label":"composite <runtime>  label","conformanceClaimReference":"CONFORMANCE.md","disposition":{"number":1.0,"text":"\\u0026"},"nodes":[{"disposition":{"kind":"outcome"},"feeds":[],"trace":{"steps":[]}}],"handoffs":[]}}'

it('asks only when chosen, shows labels verbatim, and preserves all runtime bytes', async () => {
  vi.mocked(deskFetch).mockResolvedValue(new Response(raw))
  const { unmount } = render(<GraphRehearsal graphId="onboarding" />)
  expect(deskFetch).not.toHaveBeenCalled()
  expect(screen.getByText(/A rehearsal shows/)?.textContent).toContain('It does not establish a decision, a record in the trail, a reviewed set consulted, which bytes were read, or that the facts are true.')
  expect(screen.getByText(/Neither the plan/)?.textContent).toContain('never joined to it')
  expect(screen.getByText(/Neither the plan/)?.textContent).toContain('artifact.bundleDigest is the runtime’s bundle’s digest, not the graph’s.')
  expect(screen.getByText(/The trail is silent/)?.textContent).toContain('Desk keeps neither the inputs nor the result')
  const inputs = '{"node":{"facts":{"number":1.0,"text":"$(touch nope)"}}}'
  fireEvent.change(screen.getByLabelText('Inputs by node id'), { target: { value: inputs } })
  fireEvent.click(screen.getByRole('button', { name: 'Rehearse' }))
  await screen.findByText('evaluated')
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
