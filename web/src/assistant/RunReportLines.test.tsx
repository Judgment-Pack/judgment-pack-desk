import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { RunReportLines } from './RunReportLines'
import type { TokenFigures } from '@judgment-pack/harness/execution/state'
import { DELEGATION } from './reportFixtures'
afterEach(cleanup)

const rowsOf = () => [...document.querySelectorAll('tbody tr')].map((row) => [...row.querySelectorAll('th, td')].map((cell) => cell.textContent))

it('shows each child\'s model, requests, tool calls, tokens, time to first byte and duration, with the batch total beside the parent\'s own', () => {
  const [a, b] = DELEGATION.batches[0]!.children.map((child) => child.task)
  render(<RunReportLines report={{ requests: 11, unit: 'model-requests', delegation: DELEGATION }} titles={{ [a!]: 'Review the pack', [b!]: 'Research the threshold' }} />)
  expect(rowsOf()).toEqual([
    ['Assistant (its own)', 'a-model', '3', '—', 'in 5000, out 400', '—', '—'],
    ['Review the pack', 'a-cheaper-model', '3', '2', 'in 900, out 120', '0.7', '8.2'],
    ['Research the threshold (partial)', 'a-cheaper-model', '5', '4', '—', '0.9', '11.0'],
    ['Batch 1 total', '—', '8', '6', 'in 900, out 120', '—', '11.5']
  ])
  expect(screen.getByText('Model requests: 11')).toBeTruthy()
})

it('shows a figure the provider did not return as a dash, and names a child by its profile where no title is known', () => {
  render(<RunReportLines report={{ requests: 4, unit: 'model-requests', delegation: DELEGATION }} />)
  expect(rowsOf()[2]![0]).toBe('Research (partial)')
  expect(rowsOf()[2]![4]).toBe('—')
})

it('says how many requests the response made, how many were retried and why, and what the provider reported', () => {
  render(<RunReportLines report={{ requests: 7, unit: 'model-requests', retries: { count: 2, reasons: ['provider-unavailable', 'rate-limited'] }, tokens: { inputTokens: 40000, outputTokens: 2500 } }} />)
  const report = screen.getByRole('region', { name: 'Run report' })
  expect(within(report).getByText('Model requests: 7 · retried: 2 (provider-unavailable, rate-limited) · tokens reported by the provider: in 40000, out 2500')).toBeTruthy()
  expect(report.querySelector('table')).toBeNull()
})

it('counts a subscription\'s runs as agent runs, which report no tokens', () => {
  render(<RunReportLines report={{ requests: 3, unit: 'provider-managed' }} />)
  expect(screen.getByText('Agent runs: 3')).toBeTruthy()
})

it('says how long the requests and the tools took and which requests did not complete, and lists each step', () => {
  render(<RunReportLines report={{ requests: 3, unit: 'model-requests', timing: { omitted: 2, steps: [
    { model: 'a-model', requestBytes: 4096, firstByteMs: 500, durationMs: 3000, status: 200, outcome: 'complete', tools: [{ name: 'validate', ms: 700 }, { name: 'get_schema', ms: 300, failed: true }] },
    { model: 'a-model', requestBytes: 8192, durationMs: 100, status: 503, outcome: 'failed' },
    { model: 'a-model', requestBytes: 512, firstByteMs: 300, durationMs: 900, outcome: 'aborted' }
  ] } }} />)
  expect(screen.getByText('Requests took 4.0 s in all · first byte 0.4 s on average · tool calls: 2 (1.0 s) · requests that did not complete: 2')).toBeTruthy()
  fireEvent.click(screen.getByText('Steps'))
  expect(rowsOf()).toEqual([
    ['3', 'a-model', '4.0', '0.5', '3.0', 'Done · 200', 'validate 0.7 s, get_schema 0.3 s ✗'],
    ['4', 'a-model', '8.0', '—', '0.1', 'Failed · 503', '—'],
    ['5', 'a-model', '0.5', '0.3', '0.9', 'Stopped', '—']
  ])
  expect(screen.getByText('Earlier steps left out of this list: 2')).toBeTruthy()
})

it('says how long requests waited for a turn at the relay, in the summary and against the step', () => {
  render(<RunReportLines report={{ requests: 2, unit: 'model-requests', timing: { omitted: 0, steps: [
    { model: 'a-model', requestBytes: 1024, firstByteMs: 300, durationMs: 2000, status: 200, outcome: 'complete', queuedMs: 4200, queueDepth: 2 },
    { model: 'a-model', requestBytes: 1024, firstByteMs: 200, durationMs: 1000, status: 200, outcome: 'complete' }
  ] } }} />)
  expect(screen.getByText(/waited for a turn: 4\.2 s \(requests held: 1\)/)).toBeTruthy()
  fireEvent.click(screen.getByText('Steps'))
  expect(rowsOf()[0]![5]).toBe('Done · 200 · waited 4.2 s')
  expect(rowsOf()[1]![5]).toBe('Done · 200')
})

 it.each([
  [{mode:'auto',ran:false,reason:'no-proposal'}, 'Review skipped · auto · No pack proposal'],
  [{mode:'auto',ran:true,reason:'cases',trigger:'cases',reasoning:'high'}, 'Review ran · auto · Pack has tests · Reasoning effort: high'],
  [{mode:'auto',trigger:'model-asked'}, 'Review requested · auto · Assistant requested review'],
  [{mode:'on',ran:false,reason:'phase'}, 'Review skipped · on · Plan mode']
 ] as const)('shows the saved critic decision without inferring an outcome: %j', (critic,line)=>{
  render(<RunReportLines report={{requests:1,unit:'model-requests',critic}} />)
  expect(screen.getByText(line)).toBeTruthy()
 })

it.each(['model-requests', 'provider-managed'] as const)('keeps %s prompt details collapsed and shows exact selectable digests by ordinal', (unit) => {
  const sha256 = '0123456789abcdef'.repeat(4)
  render(<RunReportLines report={{requests: 2, unit, prompts: [{request: 41, sha256}, {request: 43, sha256: 'b'.repeat(64)}]}} />)
  const summary = screen.getByText('Prompt digests')
  const details = summary.closest('details')!
  expect(details.open).toBe(false)
  fireEvent.click(summary)
  expect(details.open).toBe(true)
  expect(rowsOf()).toEqual([['41', sha256], ['43', 'b'.repeat(64)]])
  expect(screen.getByText(sha256).tagName).toBe('CODE')
  expect(screen.getByRole('columnheader', {name: 'Request'})).toBeTruthy()
})

it('offers no prompt detail for legacy or empty rows', () => {
  const {rerender} = render(<RunReportLines report={{requests: 1, unit: 'model-requests'}} />)
  expect(screen.queryByText('Prompt digests')).toBeNull()
  rerender(<RunReportLines report={{requests: 1, unit: 'model-requests', prompts: []}} />)
  expect(screen.queryByText('Prompt digests')).toBeNull()
})


it.each<{name: string; tokens?: TokenFigures; expected: string}>([
  {name: 'absent', expected: '—'},
  {name: 'empty', tokens: {}, expected: '—'},
  {name: 'input only', tokens: {inputTokens: 17}, expected: 'in 17, out —'},
  {name: 'output only', tokens: {outputTokens: 23}, expected: 'in —, out 23'},
  {name: 'zero input only', tokens: {inputTokens: 0}, expected: 'in 0, out —'},
  {name: 'zero output only', tokens: {outputTokens: 0}, expected: 'in —, out 0'},
  {name: 'explicit zeros', tokens: {inputTokens: 0, outputTokens: 0}, expected: 'in 0, out 0'},
  {name: 'reasoning only', tokens: {reasoningTokens: 5}, expected: '—'}
])('preserves $name token missingness in main, delegated parent, child and batch figures', ({tokens, expected}) => {
  const delegation = structuredClone(DELEGATION)
  delegation.parent.tokens = tokens
  delegation.batches[0]!.children[0]!.tokens = tokens
  delegation.batches[0]!.total.tokens = tokens
  render(<RunReportLines report={{requests: 1, unit: 'model-requests', ...(tokens === undefined ? {} : {tokens}), delegation}} />)
  const summary = screen.getByRole('region', {name: 'Run report'}).querySelector('p')!
  expect(summary.textContent).toBe(`Model requests: 1${tokens === undefined ? '' : ` · tokens reported by the provider: ${expected}`}`)
  for (const index of [0, 1, 3]) expect(rowsOf()[index]![4]).toBe(expected)
})

it.each([1, 2])('presents the complete retained report v%s without turning provider-managed used zero into invocations', async version => {
 const {REPORT1, REPORT2} = await import('./reportFixtures')
 const {readRunReport} = await import('@judgment-pack/harness/runReport')
 const canonical = readRunReport(version === 1 ? REPORT1 : REPORT2)
 render(<RunReportLines report={{requests: 0, unit: 'provider-managed', canonical}} />)
 expect(screen.getByText('Agent runs: 1')).toBeTruthy()
 expect(screen.queryByText('Agent runs: 0')).toBeNull()
 fireEvent.click(screen.getByText('Complete run report'))
 const retained = document.querySelector('pre')!
 expect(JSON.parse(retained.textContent!)).toEqual(canonical)
 expect(retained.textContent).toContain('reported-model')
 expect(retained.textContent).toContain('"outputTokens": 0')
})

it('keeps native invocation count unknown when a canonical report has only used zero', async () => {
 const {REPORT1} = await import('./reportFixtures')
 const {readRunReport} = await import('@judgment-pack/harness/runReport')
 const {usage: _unknown, ...facts} = REPORT1
 render(<RunReportLines report={{requests: 0, unit: 'provider-managed', canonical: readRunReport(facts)}} />)
 expect(screen.getByText('Provider managed · agent runs unknown')).toBeTruthy()
 expect(screen.queryByText('Agent runs: 0')).toBeNull()
})
