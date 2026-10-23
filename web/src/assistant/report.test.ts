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
