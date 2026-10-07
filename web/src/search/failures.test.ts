import { describe, expect, it } from 'vitest'
import { GatewayError } from '../research/gatewayClient'
import { searchFailure, SearchFailure } from './failures'
import { readResponseHistory, summarizeWork, workItems } from '../chat/responseHistory'

describe('safe search failure diagnostics', () => {
 it.each([
  ['search-not-grounded', /no usable grounded sources/],
  ['provider-unavailable', /could not complete this request/],
  ['credentials-required', /authentication or access/],
  ['search-budget-exhausted', /daily search limit/],
  ['rate-limited', /limiting requests/],
  ['source-changed', /connection changed/],
 ] as const)('identifies %s without blaming unrelated settings', (code, expected) => {
  const error = searchFailure(new GatewayError(400, `source failed: ${code}`))
  expect(error.code).toBe(code)
  expect(error.message).toMatch(expected)
 })
 it('does not leak arbitrary upstream messages or infer authentication from HTTP 400', () => {
  for (const cause of [new GatewayError(400,'private query and secret-token'),new GatewayError(400,'source failed: credentials-required secret-token'),new Error('secret-token')]) {
   const result=searchFailure(cause)
   expect(result.message).not.toMatch(/secret-token|private query|Check the connection credentials/)
   expect(result.code).not.toBe('credentials-required')
  }
  expect(searchFailure(new SearchFailure('search-verification-failed')).message).toContain('could not be verified')
  // A token-shaped word the adapter did not author is not a cause Desk names.
  for (const word of ['secret-token','search-canceled','search-failed','toString']) expect(searchFailure(new GatewayError(400, `source failed: ${word}`)).code).toBe('gateway-refused')
  expect(searchFailure(new GatewayError(502, 'source failed: secret-token')).code).toBe('gateway-unavailable')
 })
 it('keeps an authored cause in saved Work details without raw tool text', () => {
  const work=summarizeWork([{type:'tool_result',name:'search_sources',isError:true,text:'secret-token',structured:{searchFailure:'search-not-grounded'}}],false)
  expect(work.notices).toEqual([searchFailure(new GatewayError(400,'source failed: search-not-grounded')).message])
  const saved=readResponseHistory([{id:'response',documents:[],websites:[],sourceIds:[],work}])
  expect(saved[0]!.work).toEqual(work)
  expect(JSON.stringify(saved)).not.toContain('secret-token')
  expect(summarizeWork([{type:'tool_result',name:'search_sources',isError:true,text:'secret-token',structured:{searchFailure:'secret-token'}}],false).notices).toEqual([])
  expect(workItems([{type:'tool_call',callId:'a',name:'search_sources',args:{query:'q'}},{type:'tool_result',callId:'a',name:'search_sources',isError:true,text:'secret-token',structured:{searchFailure:'secret-token'}}],false)[0]).not.toHaveProperty('failure')
 })
 it('refuses saved history whose step names a cause Desk did not author, or names one on the wrong step', () => {
  const row=(item:object)=>[{id:'response',documents:[],websites:[],sourceIds:[],work:{items:[item],notices:[]}}]
  expect(readResponseHistory(row({id:'a',name:'search_sources',status:'failed',failure:'search-timeout'}))[0]!.work.items[0]!.failure).toBe('search-timeout')
  for (const item of [{id:'a',name:'search_sources',status:'failed',failure:'secret-token'},{id:'a',name:'search_sources',status:'complete',failure:'search-timeout'},{id:'a',name:'read_link',status:'failed',failure:'search-timeout'}])
   expect(()=>readResponseHistory(row(item))).toThrow('History was left untouched')
 })
})
