import { describe, expect, it, vi } from 'vitest'
import { checkCandidate } from './checkCandidate'
import { EVALUATOR_SPEC, EXPECTATION_TOOL, findingSummary, validateExpectations, type InvalidFinding } from './expectations'
import { fixtureExpectations } from './__fixtures__/expectationRuntime'
import recordedFindings from './__fixtures__/expectations.json'
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

  it('keeps an expectation no pack can produce apart from a Core defect and a limit, and says the expectation must change', async () => {
    // The runtime's ADR-0037: a legal §8.3 shape that §8's step order or §5's
    // identifier grammar puts beyond every conforming pack. It is not a shape
    // defect and not an input the runtime declined to read, so it is a case of
    // its own -- and what a person reads is that the expectation, not the draft,
    // has to change, in a sentence that ends with the runtime's own rule.
    const unreachable = [
      ['unresolved retaining not-applicable', '§8 step 1'],
      ['no match beside another reason', '§8 step 10'],
      ['outcome id outside the local-id grammar', '§5']
    ] as const
    const recorded = (name: string) => recordedFindings.find(row => row.name === name)!
    const results = await validateExpectations([...unreachable.map(([name]) => JSON.parse(recorded(name).text)), reasonless], callTool, signal)
    unreachable.forEach(([name, rule], index) => {
      const { code, message } = recorded(name).result as { code: string; message: string }
      expect(code).toBe('JPS-EXPECTATION-UNREACHABLE')
      expect(results[index]).toEqual({ status: 'invalid', message, unreachable: true })
      const summary = findingSummary(results[index] as InvalidFinding)
      expect(summary).toBe(`This expectation names a disposition no pack can produce, so the expectation must change, not the draft: ${message}`)
      expect(summary).toContain(rule)
      expect(summary).not.toContain('did not admit')
    })
    // A §8.3 defect in the same batch is still read as one, in the runtime's words alone.
    expect(results[3]).toMatchObject({ status: 'invalid', admitted: true })
    expect(results[3]).not.toHaveProperty('unreachable')
    expect(findingSummary(results[3] as InvalidFinding)).toBe((recorded('reasonless unresolved').result as { message: string }).message)
  })

  it('reports a prose refusal in the runtime\'s own words, not as a JSON error', async () => {
    // A live run against a project whose jpack.json the runtime would not load
    // showed every case as "Unexpected token 'T' … is not valid JSON": the
    // refusal was parsed before it was read as one, and the sentence saying what
    // to fix never reached the Tests tab.
    const refusal = 'The project configuration jpack.json does not satisfy the jpack.json schema: at \'/packs\': minProperties: got 0, want 1'
    const tools = vi.fn(async (name) => {
      if (name === 'validate') return { structuredContent: { status: 'valid' } }
      return { isError: true, content: [{ type: 'text', text: refusal }] }
    })
    const row = { id: 'test', facts: {}, expectationSource: 'src-1#e1', rationale: 'fixture', expectedDisposition: unknown }
    const checked = await checkCandidate(JSON.stringify({ specVersion: EVALUATOR_SPEC }), [row], tools, signal)
    expect(checked.cases[0]).toMatchObject({ passed: false, refused: `the runtime refused: ${refusal}` })
    expect(checked.cases[0]!.refused).not.toContain('JSON')
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
