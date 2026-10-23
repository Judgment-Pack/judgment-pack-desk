import { describe, expect, it } from 'vitest'
import { readExecution } from '@judgment-pack/harness/execution/state'
import { reportFromExecution, sumTokens, validRunReport } from './report'
import { checkpoint, DELEGATION } from './reportFixtures'

describe('the report an execution checkpoint gives', () => {
  it('counts the requests, sums the tokens the provider returned, and counts the requests it refused', () => {
    const report = reportFromExecution(checkpoint({
      completions: [
        { request: 1, phase: 'work', result: { finishReason: 'tool-calls', inputTokens: 1000, outputTokens: 50, cacheReadTokens: 800 }, attempt: 1, failure: { version: 1, code: 'provider-unavailable', source: 'model', outcome: 'not-started' } as never, waitMs: 1000 },
        { request: 2, phase: 'work', result: { finishReason: 'tool-calls', inputTokens: 1200, outputTokens: 70 }, attempt: 2 },
        { request: 3, phase: 'review', result: { finishReason: 'stop', inputTokens: 300, outputTokens: 30 } }
      ]
    }))
    expect(report).toEqual({
      requests: 11, unit: 'model-requests',
      retries: { count: 1, reasons: ['provider-unavailable'] },
      // The critic's request is the review's, not the parent's own.
      tokens: { inputTokens: 2200, outputTokens: 120, cacheReadTokens: 800 }
    })
  })

  it('states no token count the provider did not return, and no retry that did not happen', () => {
    const report = reportFromExecution(checkpoint({ used: 2, completions: [{ request: 1, phase: 'work', result: { finishReason: 'stop' } }] }))
    expect(report).toEqual({ requests: 2, unit: 'model-requests' })
    expect(sumTokens([{}, {}])).toBeUndefined()
    expect(sumTokens([{ inputTokens: 5 }, { inputTokens: 7, outputTokens: 1 }])).toEqual({ inputTokens: 12, outputTokens: 1 })
  })

  it('carries the checkpoint\'s delegation figures whole, each child beside the batch total and the parent\'s own', () => {
    const report = reportFromExecution(checkpoint({ delegation: DELEGATION }))!
    expect(report.delegation).toEqual(DELEGATION)
    expect(report.delegation).not.toBe(DELEGATION)
  })

  it('has no report for a checkpoint that made no request', () => {
    expect(reportFromExecution(undefined)).toBeUndefined()
    expect(reportFromExecution(checkpoint({ used: 0 }))).toBeUndefined()
  })

  it('survives the checkpoint\'s own save and read', () => {
    const saved = readExecution(JSON.parse(JSON.stringify(checkpoint({ delegation: DELEGATION }))))
    expect(reportFromExecution(saved)).toEqual(reportFromExecution(checkpoint({ delegation: DELEGATION })))
  })
})

describe('a saved report', () => {
  it('is read closed', () => {
    const good = reportFromExecution(checkpoint({ delegation: DELEGATION }))
    expect(validRunReport(good)).toBe(true)
    for (const bad of [{ ...good, extra: 1 }, { ...good, unit: 'tokens' }, { ...good, requests: -1 }, { ...good, retries: { count: 1, reasons: [3] } }, { ...good, tokens: { inputTokens: 'many' } }, { ...good, delegation: { ...DELEGATION, parent: { ...DELEGATION.parent, modelRequests: -2 } } }, null, []]) {
      expect(validRunReport(bad), JSON.stringify(bad)).toBe(false)
    }
  })
})

it('keeps every canonical critic decision through saved checkpoint and report readers, including why no critic ran',()=>{
 for(const critic of [{mode:'auto',ran:false,reason:'no-proposal'}, {mode:'on',ran:true,reason:'person',reasoning:'ultra'}, {mode:'auto',ran:true,reason:'cases',trigger:'cases',reasoning:'on'}] as const){
  const restored=readExecution(JSON.parse(JSON.stringify(checkpoint({critic}))))
  const report=reportFromExecution(restored)!
  expect(report.critic).toEqual(critic)
  expect(validRunReport(JSON.parse(JSON.stringify(report)))).toBe(true)
 }
 expect(validRunReport({requests:0,unit:'model-requests',critic:{mode:'sometimes'}})).toBe(false)
})

const prompts = [{ request: 41, sha256: 'a'.repeat(64) }, { request: 43, sha256: '0123456789abcdef'.repeat(4) }]

describe('retained prompt digests', () => {
  it.each(['complete', 'paused', 'stopped'] as const)('preserves %s rows independently of request and retry counters', (status) => {
    const execution = readExecution(JSON.parse(JSON.stringify(checkpoint({ status, unit: 'provider-managed', used: 1, limit: 1, prompts }))))!
    const report = reportFromExecution(execution)!
    expect(report.prompts).toEqual(prompts)
    expect(report.prompts).not.toBe(execution.prompts)
    expect(report.prompts![0]).not.toBe(execution.prompts![0])
    report.prompts![0]!.sha256 = 'b'.repeat(64)
    expect(execution.prompts).toEqual(prompts)
  })

  it('accepts legacy, empty and the full retained window without inventing rows', () => {
    expect(reportFromExecution(checkpoint())!.prompts).toBeUndefined()
    for (const rows of [[], prompts, Array.from({length: 300}, (_, at) => ({request: at + 400, sha256: 'a'.repeat(64)}))]) {
      expect(validRunReport({requests: 1, unit: 'model-requests', prompts: rows})).toBe(true)
      expect(readExecution(checkpoint({prompts: rows}))?.prompts).toEqual(rows)
    }
  })

  it('refuses the same malformed rows as the canonical checkpoint reader', () => {
    const row = prompts[0]!
    const invalid = [null, {}, 'private prompt text', [null], [[row]], [{}], [{request: 1}], [{sha256: row.sha256}],
      [{...row, text: 'private prompt text'}], [{...row, request: 0}], [{...row, request: -1}], [{...row, request: 1.5}],
      [{...row, request: Number.MAX_SAFE_INTEGER + 1}], [{...row, request: '41'}], [{...row, sha256: 'A'.repeat(64)}],
      [{...row, sha256: 'a'.repeat(63)}], [{...row, sha256: 'g'.repeat(64)}], [{...row, sha256: 1}],
      [row, row], [prompts[1], row], Array.from({length: 301}, (_, at) => ({...row, request: at + 1}))]
    for (const rows of invalid) {
      expect(validRunReport({requests: 2, unit: 'model-requests', prompts: rows})).toBe(false)
      expect(() => readExecution({...checkpoint(), prompts: rows})).toThrow()
    }
  })
})

// Keep this probe separate from the shared malformed corpus: a future pinned
// checkpoint reader change must not weaken the saved report's exact hash boundary.
it.each([['LF', '\n'], ['CR', '\r'], ['U+2028', '\u2028'], ['U+2029', '\u2029']])('refuses a trailing %s in saved report digests and probes the pinned reader', (_name, separator) => {
  const rows = [{request: 41, sha256: 'a'.repeat(64) + separator}]
  expect(validRunReport({requests: 2, unit: 'model-requests', prompts: rows})).toBe(false)
  expect(() => readExecution(checkpoint({prompts: rows}))).toThrow('Saved execution progress is invalid. History was left untouched.')
})
