import { cleanup, fireEvent, screen } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { connected, renderConnected, stubClient } from '../testing/harness'
import { forgetDivergentPairs } from '../mcp/refetchLedger'
import type { GraphSuite } from '../mcp/types'
import { GraphView } from './GraphView'

// ADR-0011, row 2: the Tests view is held to sections 2 and 9. Each test below
// pins one label or one sentence, with a payload that discriminates it.

afterEach(cleanup)
afterEach(forgetDivergentPairs)

const CLAIM = 'the runtime’s CONFORMANCE.md'
const LABEL = 'Experimental, non-normative: this reports the supplied cases, not a conformance result.'

const SUITE: GraphSuite = {
  status: 'mismatch',
  kind: 'graph-test-matrix',
  experimental: true,
  label: LABEL,
  conformanceClaimReference: 'CONFORMANCE.md',
  configPath: 'jpack.json',
  summary: { total: 3, passed: 2, mismatched: 1 },
  graphs: [
    {
      id: 'onboarding',
      status: 'mismatch',
      path: 'onboarding.graph.json',
      rowsPath: 'onboarding.rows.json',
      summary: { total: 3, passed: 2, mismatched: 1 },
      rows: [
        {
          id: 'clear-approves',
          status: 'mismatch',
          expected: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
          actual: '{"kind":"outcome","outcomeId":"decline","reasons":[]}'
        }
      ]
    }
  ]
}

function render(suite: GraphSuite) {
  const { client } = stubClient({
    experimental_test_graphs: () => ({ text: JSON.stringify(suite) }),
    experimental_list_graphs: () => ({ text: JSON.stringify({ status: 'valid', graphs: [] }) })
  })
  const rendered = renderConnected(
    <Routes><Route path="/graphs" element={<GraphView />} /></Routes>,
    connected({ client, graphInventorySupported: true }),
    { path: '/graphs' }
  )
  fireEvent.click(screen.getByRole('button', { name: /Run all graph tests/ }))
  return rendered
}

const without = <K extends keyof GraphSuite>(key: K): GraphSuite => {
  const copy = { ...SUITE }
  delete copy[key]
  return copy
}

describe('graph row 2: the labels beside the runtime’s label', () => {
  it('graph row 2: kind, experimental and the claim reference are shown, each as what it is', async () => {
    const { container } = render(SUITE)
    await screen.findByText(LABEL)
    const labels = container.querySelector('.graph-labels')!
    expect(labels.textContent).toContain('kind graph-test-matrix')
    expect(labels.textContent).toContain('experimental true')
    expect(labels.textContent).toContain('Conformance claim')
    expect(labels.textContent).toContain('stated in CONFORMANCE.md')
    expect(labels.textContent).toContain('a locator for the repository file that makes the claim. This payload makes none')
    expect(labels.querySelector('code[lang="en"]')).not.toBeNull()
    expect(screen.getByText(LABEL).getAttribute('lang')).toBe('en')
    // The reference is a locator, so it is not worded as a claim of Desk's.
    expect(labels.textContent).not.toContain(CLAIM)
    expect(labels.textContent).not.toMatch(/conforms|conformant|certified/i)
  })

  it('graph row 2: a payload with no claim reference shows no claim', async () => {
    const { container } = render(without('conformanceClaimReference'))
    await screen.findByText(LABEL)
    expect(container.textContent).not.toContain('Conformance claim')
    expect(container.textContent).not.toContain('a locator for the repository file')
  })

  it('graph row 2: experimental is shown only as the runtime printed it', async () => {
    const absent = render(without('experimental'))
    await screen.findByText(LABEL)
    expect(absent.container.querySelector('.graph-labels')!.textContent).not.toMatch(/experimental (true|false)/)
    cleanup()
    const printedFalse = render({ ...SUITE, experimental: false })
    await screen.findByText(LABEL)
    const text = printedFalse.container.querySelector('.graph-labels')!.textContent!
    expect(text).toContain('experimental false')
    expect(text).not.toContain('experimental true')
  })

  it('graph row 2: a payload carrying none of the members shows no label block', async () => {
    const bare: GraphSuite = { status: 'passed', summary: { total: 0, passed: 0, mismatched: 0 }, graphs: [] }
    const { container } = render(bare)
    await screen.findByText('No graph test results were reported.')
    expect(container.querySelector('.graph-labels')).toBeNull()
  })
})

describe('graph row 2: Desk’s own sentences', () => {
  it('graph row 2: the summary is the runtime’s members, not a count of cases passed', async () => {
    const { container } = render(SUITE)
    await screen.findByText(LABEL)
    const summaries = [...container.querySelectorAll('code[lang="en"]')].map(node => node.textContent)
    expect(summaries.filter(text => text === 'passed 2, mismatched 1, total 3')).toHaveLength(2)
    expect(container.textContent).not.toMatch(/of \d+ cases passed/)
    expect(container.textContent).not.toMatch(/\d+\/\d+ rows/)
  })

  it('graph row 2: a member the runtime did not print is not counted in', async () => {
    const { container } = render({ ...SUITE, summary: { total: 3, passed: 2 } as GraphSuite['summary'] })
    await screen.findByText(LABEL)
    expect(container.textContent).toContain('summary passed 2, total 3')
    expect(container.textContent).not.toMatch(/mismatched (undefined|NaN)/)
  })

  it('graph row 2: no sentence of Desk’s calls anything verified, proof, evidence, trusted or a verdict', async () => {
    const { container } = render(SUITE)
    await screen.findByText(LABEL)
    const own = container.cloneNode(true) as HTMLElement
    // The runtime’s sentences, ids and members are the runtime’s own.
    own.querySelectorAll('[lang="en"], code').forEach(node => node.remove())
    expect(own.textContent).not.toMatch(/verified|proof|evidence|trusted|verdict/i)
  })

  it('graph row 2: the closing note says what a run shows and what it does not', async () => {
    render(SUITE)
    await screen.findByText(LABEL)
    expect(screen.getByText(/does not show that the rows are right or that coverage is complete/)).toBeTruthy()
  })
})

describe('graph row 2: no path reaches the page', () => {
  it('graph row 2: paths and sentences naming a path from a root are redacted', async () => {
    const root = '/home/someone/my project\twith tabs'
    const suite: GraphSuite = {
      ...SUITE,
      configPath: `${root}/jpack.json`,
      graphs: [{
        ...SUITE.graphs![0]!,
        path: `${root}/onboarding.graph.json`,
        rowsPath: `${root}/onboarding.rows.json`,
        detail: `The file "${root}/onboarding.rows.json" could not be read.`,
        rows: [{ ...SUITE.graphs![0]!.rows![0]!, detail: `Could not read ${root}/pack.json` }]
      }]
    }
    const { container } = render(suite)
    await screen.findByText(LABEL)
    expect(container.textContent).not.toContain('someone')
    expect(container.textContent).not.toContain('my project')
    expect(container.textContent).not.toContain('tabs')
    expect(container.textContent).toContain('could not be read')
  })

  it('graph row 2: the entry’s and the row’s sentences are marked as the runtime’s English', async () => {
    const suite: GraphSuite = {
      ...SUITE,
      graphs: [{
        ...SUITE.graphs![0]!,
        detail: 'The rows file could not be read.',
        rows: [{ ...SUITE.graphs![0]!.rows![0]!, detail: 'The expected disposition differs.' }]
      }]
    }
    render(suite)
    expect((await screen.findByText('The rows file could not be read.')).getAttribute('lang')).toBe('en')
    expect(screen.getByText('The expected disposition differs.').getAttribute('lang')).toBe('en')
  })

  it('graph row 2: a relative path is shown as given', async () => {
    render(SUITE)
    expect(await screen.findByText('onboarding.graph.json')).toBeTruthy()
    expect(screen.getByText('onboarding.rows.json')).toBeTruthy()
  })
})
