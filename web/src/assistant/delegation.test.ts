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

 it('shows the delegate_tasks call as a row of its own, before the tasks it started',()=>{
  const task:DelegatedTask={id:crypto.randomUUID(),parentRunId:crypto.randomUUID(),title:'Review',objective:'Review it',profile:'review',state:'running',createdAt:new Date().toISOString(),modelRequests:0,toolCalls:0,attempts:1,steps:[]}
  const events=[
   {type:'tool_call' as const,callId:'call-1',name:'delegate_tasks',args:{tasks:[{title:'Review',objective:'Review it',profile:'review'}]}},
   {type:'delegated_task' as const,task},
   {type:'delegated_task' as const,task:{...task,state:'complete' as const,result:'Findings.'}},
   {type:'tool_result' as const,callId:'call-1',name:'delegate_tasks',isError:false,text:'{"tasks":[]}'}
  ]
  const rows=workItems(events,false)
  expect(rows.map(row=>[row.name,row.status])).toEqual([['delegate_tasks','complete'],['delegate_task','complete']])
  expect(workItems(events.slice(0,2),true).map(row=>[row.name,row.status])).toEqual([['delegate_tasks','working'],['delegate_task','working']])
 })
 it('puts a running task\'s streamed prose under it until it ends, and keeps none of it saved',()=>{
  const task:DelegatedTask={id:crypto.randomUUID(),parentRunId:crypto.randomUUID(),title:'Review',objective:'Review it',profile:'review',state:'running',createdAt:new Date().toISOString(),modelRequests:1,toolCalls:0,attempts:1,steps:[]}
  const progress={[task.id]:'Reading the receipt rule'}
  expect(workItems([{type:'delegated_task',task}],true,progress)[0]?.progress).toBe('Reading the receipt rule')
  const ended=workItems([{type:'delegated_task',task},{type:'delegated_task',task:{...task,state:'complete',result:'Findings.'}}],true,progress)
  expect(ended[0]?.progress).toBeUndefined()
  // What a checkpoint holds mid-run is read back without it.
  const saved=[{id:'response',documents:[],websites:[],sourceIds:[],work:{items:[{id:task.id,name:'delegate_task',status:'working',task,progress:'Reading the receipt rule'}],notices:[]}}]
  expect(readResponseHistory(saved)[0]?.work.items[0]).not.toHaveProperty('progress')
 })
})
