import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { RunReportLines } from './RunReportLines'
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
