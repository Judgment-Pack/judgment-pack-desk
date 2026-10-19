// The Desk's case of the harness's delegation tests: the response history is the Desk's.
// The rest of the file moved with the harness to @judgment-pack/harness (delegation.test.ts).
import { describe, expect, it } from 'vitest'
import type { DelegatedTask } from '@judgment-pack/harness/delegationContract'
import { workItems, readResponseHistory } from '../chat/responseHistory'

describe('retained task history',()=>{
 it('retains completed findings and restores in-flight tasks as interrupted',()=>{
  const done:DelegatedTask={id:crypto.randomUUID(),parentRunId:crypto.randomUUID(),title:'Review',objective:'Review it',profile:'review',state:'complete',createdAt:new Date().toISOString(),modelRequests:1,toolCalls:0,attempts:1,steps:[],result:'Saved findings'}
  const pending={...done,id:crypto.randomUUID(),state:'running' as const,result:undefined,steps:[{name:'list_dialects',state:'running' as const}]}
  const stopped=workItems([{type:'delegated_task',task:pending}],false)
  expect(stopped[0]).toMatchObject({status:'interrupted',task:{state:'interrupted',steps:[{name:'list_dialects',state:'interrupted'}]}})
  const history=[{id:'response',documents:[],websites:[],sourceIds:[],work:{items:[{id:done.id,name:'delegate_task',status:'complete',task:done},{id:pending.id,name:'delegate_task',status:'working',task:pending}],notices:[]}}]
  const restored=readResponseHistory(history)
  expect(restored[0]?.work.items[0]?.task?.result).toBe('Saved findings')
  expect(restored[0]?.work.items[1]).toMatchObject({status:'interrupted',task:{state:'interrupted'}})
  history[0]!.work.items[0]!.task={...done,modelRequests:100}
  expect(()=>readResponseHistory(history)).toThrow()
 })
})
