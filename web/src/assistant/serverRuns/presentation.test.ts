import { expect, it } from 'vitest'
import { INITIAL_STATE } from '../../research/initialState'
import { decodeSnapshot } from './decode'
import { presentRun } from './presentation'
import pending from './fixtures/pending.json'
it('retains parent and child public previews as display-only data outside durable turns and replay cursor',()=>{
 const task={id:'child-one',parentRunId:'parent-one',title:'Child review',objective:'Review the supplied document.',profile:'review',state:'running',createdAt:'2026-10-23T00:00:00Z',modelRequests:1,toolCalls:0,attempts:1,steps:[]}
 const snapshot=decodeSnapshot({...pending,state:'stopped',events:[{id:'task-event',event:{type:'delegated_task',task}}],cursor:'1:1',displayPreview:[{text:'Parent written so far'},{taskId:task.id,text:'Child written so far'}]},{owner:{...pending.owner,kind:'chat'},requestId:pending.requestId})
 const view=presentRun(INITIAL_STATE,snapshot)
 expect(view.streaming).toBe('Parent written so far');expect(view.turns).toEqual([])
 expect(view.responses?.[0].work.items[0]).toMatchObject({id:task.id,status:'interrupted',progress:'Child written so far'})
 expect(snapshot.cursor).toBe('1:1');expect(snapshot.events).toHaveLength(1)
})
