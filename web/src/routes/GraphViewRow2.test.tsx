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

const SERVED = JSON.stringify({
  formatVersion: '1',
  id: 'onboarding',
  version: '0.1.0',
  nodes: { screening: { pack: 'sanctions-screening' }, decision: { pack: 'vendor-onboarding' } },
  edges: [{ from: 'screening', to: 'decision', fact: '/vendor/sanctionsScreening/status' }],
  result: 'decision'
})
const DIGEST = 'c'.repeat(64)

function render(suite: GraphSuite, served = false) {
  const { client } = stubClient({
    experimental_test_graphs: () => ({ text: JSON.stringify(suite) }),
    experimental_list_graphs: () => ({ text: JSON.stringify({ status: 'valid', graphs: [] }) }),
    experimental_get_graph: () => ({
      text: SERVED,
      structured: { status: 'valid', id: 'onboarding', graphId: 'onboarding', graphVersion: '0.1.0', formatVersion: '1', path: 'onboarding.graph.json', bytes: SERVED.length, sha256: DIGEST }
    })
  })
  const rendered = renderConnected(
    <Routes><Route path="/graphs" element={<GraphView />} /></Routes>,
    connected({ client, graphInventorySupported: true, graphDocumentSupported: served }),
    { path: '/graphs' }
  )
  fireEvent.click(screen.getByRole('button', { name: /Run all graph tests/ }))
  return rendered
}

const PASSED: GraphSuite = {
  ...SUITE,
  status: 'passed',
  summary: { total: 1, passed: 1, mismatched: 0 },
  graphs: [{
    ...SUITE.graphs![0]!,
    status: 'passed',
    summary: { total: 1, passed: 1, mismatched: 0 },
    rows: [{ ...SUITE.graphs![0]!.rows![0]!, status: 'passed', actual: SUITE.graphs![0]!.rows![0]!.expected }]
  }]
}
const BOUND: GraphSuite = { ...PASSED, graphs: [{ ...PASSED.graphs![0]!, graphSha256: DIGEST }] }

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
    expect(summaries.filter(text => text === 'total 3, passed 2, mismatched 1')).toHaveLength(2)
    expect(container.textContent).not.toMatch(/of \d+ cases passed/)
    expect(container.textContent).not.toMatch(/\d+\/\d+ rows/)
  })

  it('graph row 2: the summary keeps the runtime’s members and their order', async () => {
    const { container } = render({ ...SUITE, summary: { total: 3, passed: 2, skipped: 1 } as unknown as GraphSuite['summary'] })
    await screen.findByText(LABEL)
    expect(container.textContent).toContain('summary total 3, passed 2, skipped 1')
    expect(container.textContent).not.toMatch(/mismatched (undefined|NaN)/)
  })

  it.each([
    ['a suite with a mismatch', SUITE, false],
    ['a suite where every row passed', PASSED, false],
    ['a suite whose digest is the served document’s', BOUND, true]
  ])('graph row 2: no sentence of Desk’s claims anything, in %s', async (_name, suite, served) => {
    const { container } = render(suite, served)
    await screen.findByText(LABEL)
    if (served) await screen.findByText(/One revision/)
    const own = container.cloneNode(true) as HTMLElement
    // The runtime’s sentences, ids and members are the runtime’s own.
    own.querySelectorAll('[lang="en"], code').forEach(node => node.remove())
    expect(own.textContent).not.toMatch(/verif|\bprov(e|es|ed|ing)\b|proof|evidenc|trust|verdict|healthy|passed (its|their) checks/i)
  })

  it('graph row 2: the Tests view carries one Experimental label, and the all-graphs locator names the graphs’ packs', async () => {
    const { container } = render(SUITE)
    await screen.findByText(LABEL)
    expect([...container.querySelectorAll('.pill, [class*="pill" i]')].filter(node => node.textContent === 'Experimental')).toHaveLength(1)
    expect(container.textContent).toContain("not about these graphs' packs")
    expect(container.textContent).not.toContain("not about this graph's packs")
  })

  it('graph row 2: the closing note says what a run shows and what it does not', async () => {
    render(SUITE)
    await screen.findByText(LABEL)
    expect(screen.getByText(/does not show that the rows are right, that coverage is complete, or any authorization/)).toBeTruthy()
  })
})

describe('graph row 2: no path reaches the page', () => {
  it('graph row 2: a quoted path from a root goes whole, as the real payload prints it (relative configPath)', async () => {
    // The runtime prints configPath relative and a control character in a path as "?".
    const root = '/home/someone/Owner Files?Q3?desk'
    const suite: GraphSuite = {
      ...SUITE,
      configPath: 'jpack.json',
      graphs: [{
        ...SUITE.graphs![0]!,
        rows: [{ ...SUITE.graphs![0]!.rows![0]!, detail: `The run was refused: Node "screening" (pack "missing-pack"): The path "${root}/missing-pack-0.1.0.pack.json" resolves outside the configuration's own directory, which no configured path may.` }]
      }]
    }
    const { container } = render(suite)
    await screen.findByText(/resolves outside the configuration/)
    expect(container.textContent).not.toContain('someone')
    expect(container.textContent).not.toContain('Owner')
    expect(container.textContent).not.toContain('Q3')
    expect(container.textContent).toContain('The path "…" resolves outside')
  })

  it('graph row 2: the same, with the folder in the payload’s own path members', async () => {
    const root = '/home/someone/my project\twith\u2028tabs'
    const suite: GraphSuite = {
      ...SUITE,
      configPath: `${root}/jpack.json`,
      graphs: [{
        ...SUITE.graphs![0]!,
        path: `${root}/onboarding.graph.json`,
        rowsPath: `${root}/onboarding.rows.json`,
        detail: `The file ${root}/onboarding.rows.json could not be read.`,
        rows: [{ ...SUITE.graphs![0]!.rows![0]!, detail: `Could not read ${root}/pack.json` }]
      }]
    }
    const { container } = render(suite)
    await screen.findByText(LABEL)
    expect(container.textContent).not.toContain('someone')
    expect(container.textContent).not.toContain('my project')
    expect(container.textContent).not.toContain('tabs')
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
