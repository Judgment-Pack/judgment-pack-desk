import { describe, expect, it, vi } from 'vitest'
import { checkCandidate } from './checkCandidate'
import { EVALUATOR_SPEC, EXPECTATION_TOOL, findingSummary, validateExpectations } from './expectations'
import { fixtureExpectations } from './__fixtures__/expectationRuntime'
import type { McpToolResult } from '../assistant/engine'

const signal = new AbortController().signal
const unknown = { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } }
const reasonless = { ...unknown, reasons: [] }
const callTool = vi.fn(async (_name, args) => fixtureExpectations(args))

describe('runtime expectation admission', () => {
  it('retains invalid findings in position and canonicalizes valid reason sets', async () => {
    const unordered = { ...unknown, reasons: ['unknown', 'conflict'] }
    const results = await validateExpectations([unknown, reasonless, unordered], callTool, signal)
    expect(results).toMatchObject([{ status: 'valid' }, { status: 'invalid', message: expect.stringContaining('§8.3') }, { status: 'valid' }])
    expect(JSON.parse((results[2] as { canonical: string }).canonical).reasons).toEqual(['conflict', 'unknown'])
    // The evaluator's version, never the draft's: an expectation describes what
    // an evaluator produces, and only one evaluator version exists.
    expect(callTool).toHaveBeenLastCalledWith(EXPECTATION_TOOL, { spec_version: EVALUATOR_SPEC, expectations: [unknown, reasonless, unordered].map(row => JSON.stringify(row)) })
  })

  it.each([
    { isError: true, content: [{ type: 'text', text: 'Unknown tool' }] },
    { isError: true, structuredContent: { specVersion: EVALUATOR_SPEC, status: 'valid', results: [{ index: 0, status: 'valid', canonical: '{}' }] } },
    { structuredContent: {} },
    { structuredContent: { specVersion: EVALUATOR_SPEC, status: 'valid', results: [] } },
    { structuredContent: { specVersion: '0.1.0-draft', status: 'valid', results: [{ index: 0, status: 'valid', canonical: '{}' }] } },
    { structuredContent: { specVersion: EVALUATOR_SPEC, status: 'valid', results: [{ index: 1, status: 'valid', canonical: '{}' }] } },
    { structuredContent: { specVersion: EVALUATOR_SPEC, status: 'valid', results: [{ index: 0, status: 'invalid', message: 'invalid' }] } },
    { structuredContent: { specVersion: EVALUATOR_SPEC, status: 'valid', results: [{ index: 0, status: 'valid', canonical: 'null' }] } },
    { structuredContent: { specVersion: EVALUATOR_SPEC, status: 'valid', results: [{ index: 0, status: 'valid' }] } },
    { structuredContent: { specVersion: EVALUATOR_SPEC, status: 'invalid', results: [{ index: 0, status: 'invalid', message: '' }] } }
  ] as McpToolResult[])('fails closed for an unavailable or incomplete validator: %#', async response => {
    await expect(validateExpectations([unknown], async () => response, signal)).rejects.toThrow()
  })

  it('carries a suite larger than one call, and cancels a pending call without accepting late results', async () => {
    // The tool is stateless and indexed per call, so its 256-expectation bound
    // is a bound on a call and never on a run: a larger suite is chunked, and
    // every case is still accounted for, in order.
    const batches: number[] = []
    const chunked = vi.fn(async (_name, args) => {
      batches.push((args as { expectations: string[] }).expectations.length)
      return fixtureExpectations(args)
    })
    const results = await validateExpectations(Array(257).fill(unknown), chunked, signal)
    expect(batches).toEqual([256, 1])
    expect(results).toHaveLength(257)
    expect(results.every(row => row.status === 'valid')).toBe(true)

    const controller = new AbortController()
    const pending = validateExpectations([unknown], () => new Promise(() => {}), controller.signal)
    controller.abort()
    await expect(pending).rejects.toMatchObject({ name: 'RunCancelled' })
  })

  it('keeps a runtime limit apart from a Core defect', async () => {
    const limited = await validateExpectations([unknown], async () => ({
      structuredContent: { specVersion: EVALUATOR_SPEC, status: 'invalid', results: [{ index: 0, status: 'invalid', code: 'JPS-EXPECTATION-LIMIT', message: 'Input contains a string exceeding the configured limit.' }] }
    }), signal)
    expect(limited[0]).toMatchObject({ status: 'invalid', admitted: false })
    // A limit finding says the input was not admitted, not that Core prohibits
    // its meaning, so it is never presented as a §8.3 defect to correct.
    expect(findingSummary(limited[0] as { message: string; admitted: boolean })).toContain('did not admit')
    const refused = await validateExpectations([reasonless], callTool, signal)
    expect(findingSummary(refused[0] as { message: string; admitted: boolean })).toContain('§8.3')
  })

  it('rehearses the stored assertion, and asks the contract nothing a second time', async () => {
    const tools = vi.fn(async (name, args) => {
      if (name === EXPECTATION_TOOL) return fixtureExpectations(args)
      if (name === 'validate') return { structuredContent: { status: 'valid' } }
      return { structuredContent: { status: 'evaluated', rehearsal: true, disposition: { ...unknown, reasons: ['conflict', 'unknown'] } } }
    })
    const row = { id: 'test', facts: {}, expectationSource: 'src-1#e1', rationale: 'fixture', expectedDisposition: { ...unknown, reasons: ['conflict', 'unknown'] } }
    const document = JSON.stringify({ specVersion: EVALUATOR_SPEC })
    const checked = await checkCandidate(document, [row], tools, signal)
    expect(checked.cases[0]?.passed).toBe(true)
    // Admission is where the contract is asked, once, and the case carries the
    // canonical answer; a check that asked again would be asking about bytes it
    // already holds.
    expect(tools.mock.calls.map(call => call[0])).toEqual(['validate', 'experimental_evaluate'])
  })
})
