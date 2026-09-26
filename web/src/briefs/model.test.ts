import { describe, expect, it } from 'vitest'
import { briefResults, briefText, caseSnapshot, evidenceFor, type Snapshot } from './model'
import { emptySuite, type TestCase } from '../packs/test-workspace/model'
const c: TestCase = { id:'a', name:'Example', revision:1, row:{id:'a',facts:{value:1},evidenceAvailability:{policy:'present'},expectedDisposition:{kind:'outcome',outcomeId:'accept'}},sources:[],sourceMappings:{},origin:'manual',rationale:'' }
const pack=JSON.stringify({evidenceRequirements:[{id:'policy',required:true},{id:'review',required:true}]})
describe('brief grounding',()=>{
 it('keeps required but absent evidence visible and does not upgrade declared present to verified',()=>{
  expect(evidenceFor(caseSnapshot(pack,c,emptySuite())).map(e=>[e.id,e.state])).toEqual([['policy','present'],['review','unknown']])
 })
 it('binds a result to the exact saved case and pack, excluding other case inputs',()=>{
  const suite=emptySuite();suite.runs=[{id:'run',at:'now',packDigest:'sha256:a',packText:pack,packVersion:'1',cases:[c],origin:'tests',report:{packs:[{id:'pack',rows:[{id:'a',status:'passed',expected:'{}',actual:'{"kind":"outcome","outcomeId":"accept"}'}]}]}} as never]
  const snapshot=caseSnapshot(pack,c,suite)
  expect(briefResults(snapshot).actual).toBe('accept')
  expect(snapshot.record.run).not.toHaveProperty('cases')
  expect(caseSnapshot(pack,{...c,revision:2},suite).record.run).toBeUndefined()
  expect(caseSnapshot('{}',c,suite).record.run).toBeUndefined()
 })
 it('does not substitute a job sample for a run with unknown evidence',()=>{
  const snapshot:Snapshot={kind:'run',title:'Run',pack,record:{sample:{evidence:{policy:'present'}},run:{input:{facts:{}}}}}
  expect(evidenceFor(snapshot).every(e=>e.state==='unknown')).toBe(true)
  expect(briefResults(snapshot).actual).toBe('—')
 })
 it('rejects incomplete or oversized prose without changing a stored revision',()=>{
  expect(()=>briefText({context:'made up'})).toThrow()
  expect(()=>briefText({context:'x'.repeat(3001),findings:'x',uncertainty:'x',nextAction:'x'})).toThrow()
  expect(briefText({context:'Context',findings:'Finding',uncertainty:'Unknown',nextAction:'Review',verdict:'approve'})).not.toHaveProperty('verdict')
 })
})
