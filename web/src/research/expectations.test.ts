import { describe, expect, it, vi } from 'vitest'
import { checkCandidate } from './checkCandidate'
import { EXPECTATION_TOOL, expectationSpec, validateExpectations } from './expectations'
import { fixtureExpectations } from './__fixtures__/expectationRuntime'
import type { McpToolResult } from '../assistant/engine'

const signal = new AbortController().signal
const unknown = { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'none' } }
const reasonless = { ...unknown, reasons: [] }
const callTool = vi.fn(async (_name, args) => fixtureExpectations(args))

describe('runtime expectation admission', () => {
  it('retains invalid findings in position and canonicalizes valid reason sets', async () => {
    const unordered = { ...unknown, reasons: ['unknown', 'conflict'] }
    const results = await validateExpectations('0.2.0-draft', [unknown, reasonless, unordered], callTool, signal)
    expect(results).toMatchObject([{ status: 'valid' }, { status: 'invalid', message: expect.stringContaining('§8.3') }, { status: 'valid' }])
    expect(JSON.parse((results[2] as { canonical: string }).canonical).reasons).toEqual(['conflict', 'unknown'])
    expect(callTool).toHaveBeenLastCalledWith(EXPECTATION_TOOL, { spec_version: '0.2.0-draft', expectations: [unknown, reasonless, unordered].map(row => JSON.stringify(row)) })
  })

  it.each([
    { isError: true, content: [{ type: 'text', text: 'Unknown tool' }] },
    { structuredContent: {} },
    { structuredContent: { specVersion: '0.2.0-draft', status: 'valid', results: [] } },
    { structuredContent: { specVersion: '0.1.0-draft', status: 'valid', results: [{ index: 0, status: 'valid', canonical: '{}' }] } },
    { structuredContent: { specVersion: '0.2.0-draft', status: 'valid', results: [{ index: 1, status: 'valid', canonical: '{}' }] } },
    { structuredContent: { specVersion: '0.2.0-draft', status: 'valid', results: [{ index: 0, status: 'invalid', message: 'invalid' }] } },
    { structuredContent: { specVersion: '0.2.0-draft', status: 'valid', results: [{ index: 0, status: 'valid', canonical: 'null' }] } },
    { structuredContent: { specVersion: '0.2.0-draft', status: 'invalid', results: [{ index: 0, status: 'invalid', message: '' }] } }
  ] as McpToolResult[])('fails closed for an unavailable or incomplete validator: %#', async response => {
    await expect(validateExpectations('0.2.0-draft', [unknown], async () => response, signal)).rejects.toThrow()
  })

  it('bounds the batch and cancels a pending call without accepting late results', async () => {
    const unused = vi.fn()
    await expect(validateExpectations('0.2.0-draft', Array(257).fill(unknown), unused, signal)).rejects.toThrow('256')
    expect(unused).not.toHaveBeenCalled()
    const controller = new AbortController()
    const pending = validateExpectations('0.2.0-draft', [unknown], () => new Promise(() => {}), controller.signal)
    controller.abort()
    await expect(pending).rejects.toMatchObject({ name: 'RunCancelled' })
    expect(() => expectationSpec({})).toThrow('specVersion')
  })

  it('checks the contract before any rehearsal, and preserves exact comparison after set normalization', async () => {
    const tools = vi.fn(async (name, args) => {
      if (name === EXPECTATION_TOOL) return fixtureExpectations(args)
      if (name === 'validate') return { structuredContent: { status: 'valid' } }
      return { structuredContent: { status: 'evaluated', rehearsal: true, disposition: { ...unknown, reasons: ['conflict', 'unknown'] } } }
    })
    const row = { id: 'test', facts: {}, expectationSource: 'src-1#e1', rationale: 'fixture', expectedDisposition: reasonless }
    const document = JSON.stringify({ specVersion: '0.2.0-draft' })
    await expect(checkCandidate(document, [row], tools, signal)).rejects.toThrow('Invalid expectation for test')
    expect(tools.mock.calls.map(call => call[0])).toEqual([EXPECTATION_TOOL])
    const checked = await checkCandidate(document, [{ ...row, expectedDisposition: { ...unknown, reasons: ['unknown', 'conflict'] } }], tools, signal)
    expect(checked.cases[0]?.passed).toBe(true)
  })
})
