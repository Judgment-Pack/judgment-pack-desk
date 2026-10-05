import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TestsContent } from './TestsWorkspace'
import { emptySuite, importMatrix, type TestSuite } from './model'
import type { PackDocument } from '../../mcp/types'
import type { McpToolResult } from '../../assistant/engine'
import { DetailsSlotContext } from '../../shell/DetailsSlot'
import { languageReady, msg, setLanguage, systemMessage } from '../../i18n'
import { findingSummary, type InvalidFinding } from '../../research/expectations'
import { fixtureExpectations } from '../../research/__fixtures__/expectationRuntime'
import recorded from '../../research/__fixtures__/expectations.json'

const fixture = vi.hoisted(() => ({ call: vi.fn(), tools: vi.fn(), update: vi.fn(), suite: undefined as unknown as TestSuite }))
vi.mock('../../mcp/McpProvider', () => ({ useMcp: () => ({ status: 'ready', client }) }))
const client = { callTool: fixture.call, listTools: fixture.tools }
vi.mock('../../research/checkCandidate', async original => ({ ...await original<typeof import('../../research/checkCandidate')>(), digestOf: async () => 'digest' }))
vi.mock('../../files/queries', () => ({ useFileListing: () => ({ data: { root: 'project', files: [] } }) }))
vi.mock('./store', () => ({ useTestStorage: () => ({ suite: fixture.suite, query: { isSuccess: true }, update: fixture.update }), updateTests: vi.fn() }))
afterEach(async () => { cleanup(); vi.clearAllMocks(); setLanguage('en'); await languageReady(); localStorage.clear() })

// The runtime's own answers (`experimental_validate_expectations`, recorded),
// and the limit answer the research tests use, since no recorded row has one.
const LIMIT = { status: 'invalid', code: 'JPS-EXPECTATION-LIMIT', message: 'Input contains a string exceeding the configured limit.' }
const LIMITED = { kind: 'outcome', outcomeId: 'a'.repeat(4097), reasons: [], handoff: { state: 'none' } }
const row = (name: string) => recorded.find(r => r.name === name)!
const unreachable = recorded.filter(r => r.result.status === 'invalid' && 'code' in r.result && r.result.code === 'JPS-EXPECTATION-UNREACHABLE')

function runtime({ name, arguments: args }: { name: string; arguments: Record<string, unknown> }): McpToolResult {
  if (name !== 'experimental_validate_expectations') throw new Error(`unexpected tool ${name}`)
  const [text] = args.expectations as string[]
  if (text === JSON.stringify(LIMITED)) return { structuredContent: { status: 'invalid', specVersion: args.spec_version, results: [{ index: 0, ...LIMIT }] } }
  return fixtureExpectations(args)
}

const details = { target: null as HTMLElement | null, open: true, claim: () => () => {}, reveal: () => {} }

/** Saves one case carrying `expected` and returns the sentence the case editor shows. */
async function saveShown(expected: unknown): Promise<string> {
  fixture.call.mockClear()
  fixture.update.mockClear()
  fixture.suite = { ...emptySuite(), cases: importMatrix({ cases: [{ id: 'held', focus: 'Held case', facts: {}, expectedDisposition: expected }] }, 'manual') }
  fixture.tools.mockResolvedValue({ tools: [{ name: 'experimental_test_cases' }] })
  fixture.call.mockImplementation(async (request: Parameters<typeof runtime>[0]) => runtime(request))
  details.target = document.body.appendChild(document.createElement('div'))
  render(<QueryClientProvider client={new QueryClient()}><Tooltip.Provider><MemoryRouter><DetailsSlotContext.Provider value={details}>
    <TestsContent owner="pack" document={{ version: '1', outcomes: [] } as unknown as PackDocument} text="{}" title="Example" />
  </DetailsSlotContext.Provider></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Held case' }))
  const save = await screen.findByRole('button', { name: msg('Save case') }) as HTMLButtonElement
  await waitFor(() => expect(save.disabled).toBe(false))
  fireEvent.click(save)
  // The case editor, open beside the list, shows the save error under the case.
  const alert = await waitFor(() => {
    const shown = within(details.target!).queryAllByRole('alert').map(node => node.textContent ?? '').filter(Boolean)
    expect(shown).toHaveLength(1)
    return shown[0]!
  })
  const checked = fixture.call.mock.calls.map(([request]) => request).filter(request => request.name === 'experimental_validate_expectations')
  expect(checked).toHaveLength(1)
  expect(JSON.parse(checked[0].arguments.expectations[0])).toEqual(expected)
  // Nothing is saved while the runtime holds the expectation invalid.
  expect(fixture.update).not.toHaveBeenCalled()
  details.target.remove()
  return alert
}

describe('an invalid expectation saved in the Tests workspace', () => {
  it.each(unreachable.map(r => [r.name, r] as const))('says an expectation no pack can produce must change, not the draft (%s)', async (_, r) => {
    const message = (r.result as { message: string }).message
    const shown = await saveShown(JSON.parse(r.text))
    expect(shown).toBe(`This expectation names a disposition no pack can produce, so the expectation must change, not the draft: ${message}`)
    // The research feature's sentence for the same finding, byte for byte.
    expect(shown).toBe(findingSummary({ message, unreachable: true }))
  })

  it('still shows a §8.3 defect as the runtime words it, and a limit as input the runtime did not admit', async () => {
    const defect = row('reasonless unresolved').result as { message: string }
    const shownDefect = await saveShown(JSON.parse(row('reasonless unresolved').text))
    expect(shownDefect).toBe(defect.message)
    expect(shownDefect).toBe(findingSummary({ message: defect.message, admitted: true }))
    cleanup()
    const shownLimit = await saveShown(LIMITED)
    expect(shownLimit).toBe(`The runtime did not admit this expectation: ${LIMIT.message}`)
    expect(shownLimit).toBe(findingSummary({ message: LIMIT.message, admitted: false }))
  })

  it('reads in another language as the research feature shows the same finding, with the runtime’s message unchanged', async () => {
    await act(async () => { setLanguage('fr'); await languageReady() })
    const r = unreachable[0]!
    const findings: [unknown, InvalidFinding][] = [
      [JSON.parse(r.text), { message: (r.result as { message: string }).message, unreachable: true }],
      [LIMITED, { message: LIMIT.message, admitted: false }],
    ]
    for (const [expected, finding] of findings) {
      const shown = await saveShown(expected)
      // The research feature shows `findingSummary` through `systemMessage`.
      expect(shown).toBe(systemMessage(findingSummary(finding)))
      expect(shown).not.toBe(findingSummary(finding))
      expect(shown.endsWith(finding.message)).toBe(true)
      cleanup()
    }
  })
})
