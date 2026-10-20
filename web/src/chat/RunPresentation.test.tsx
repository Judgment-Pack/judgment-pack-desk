import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { INITIAL_STATE } from '../research/run'
import { workItems, ComposerTaskStatus, TaskStatus, WorkSummary, candidateSummary } from './RunPresentation'
import { MessageRenderer } from './MessageRenderer'
import { readResponseHistory, summarizeWork } from './responseHistory'
import { checkpoint, DELEGATION } from '../assistant/reportFixtures'
afterEach(cleanup)

it('pairs parallel invocations by identity and preserves a pending or failed call', () => {
  const rows = workItems([
    { type: 'tool_call', callId: 'a', name: 'read_source', args: {} },
    { type: 'tool_call', callId: 'b', name: 'read_source', args: {} },
    { type: 'tool_result', callId: 'b', name: 'read_source', text: 'refused', isError: true }
  ], false)
  expect(rows).toEqual([{ id: 'a', name: 'read_source', status: 'interrupted' }, { id: 'b', name: 'read_source', status: 'failed' }])
})
it('prints one progress status and no infrastructure event count for a greeting', () => {
  const state = { ...INITIAL_STATE, status: 'running' as const, detail: 'Working…', events: [{ type: 'guardrail' as const, tool: 'get_schema', action: 'narrowed' as const, detail: 'Provider schema compatibility' }] }
  const rendered = render(<><TaskStatus state={state} /><WorkSummary state={state} /></>)
  expect(screen.getAllByText('Working…')).toHaveLength(1)
  expect(screen.queryByText(/events|Work ·|Provider schema/)).toBeNull()
  rendered.rerender(<TaskStatus state={{ ...state, status: 'complete' }} />)
  expect(screen.queryByRole('status')).toBeNull()
})
it('does not label a structural check as behavioral tests passed', () => {
  expect(candidateSummary({ ...INITIAL_STATE, candidates: [{ digest: 'd', check: { valid: true, documentDigest: 'd', cases: [] } } as any] })).toBe('Structure checked · Tests not run')
})
it('renders useful Markdown while refusing active HTML, remote images and unsafe links', () => {
  const rendered = render(<MessageRenderer text={'**Result**\n\n- First\n- Second\n\n[Source](https://example.org/page) [unsafe](javascript:alert(1))\n\n```json\n{"ok":true}\n```\n\n<img src=x onerror=alert(1)>\n\n![external](https://tracker.invalid/pixel)'} />)
  expect(rendered.container.querySelectorAll('strong')).toHaveLength(1)
  expect(rendered.container.querySelectorAll('li')).toHaveLength(2)
  expect(screen.getByRole('link', { name: 'Source' }).getAttribute('rel')).toContain('noopener')
  expect(rendered.container.querySelectorAll('img,script')).toHaveLength(0)
  expect(screen.queryByRole('link', { name: 'unsafe' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Copy json' })).toBeTruthy()
})


it('marks only running status for motion, leaving actionable errors static', () => {
 const view=render(<TaskStatus state={{...INITIAL_STATE,status:'running',detail:'Working…'}}/>)
 expect(screen.getByRole('status').getAttribute('data-running')).toBe('true')
 view.rerender(<TaskStatus state={{...INITIAL_STATE,status:'failed',detail:'Try again'}}/>)
 expect(screen.getByRole('alert').hasAttribute('data-running')).toBe(false)
 view.rerender(<TaskStatus state={{...INITIAL_STATE,status:'ready'}}/>)
 expect(screen.queryByRole('status')).toBeNull()
})

it('ties each failure reason to its search invocation without duplicating the notice',()=>{
 const state={...INITIAL_STATE,events:[
 {type:'tool_call' as const,callId:'slow',name:'search_sources',args:{}},
 {type:'tool_call' as const,callId:'fast',name:'search_sources',args:{}},
 {type:'tool_result' as const,callId:'fast',name:'search_sources',text:'ok',isError:false},
 {type:'tool_result' as const,callId:'slow',name:'search_sources',text:'untrusted raw detail',isError:true,structured:{searchFailure:'search-timeout'}}
 ]}
 const rows=workItems(state.events,false)
 expect(rows[0]?.failure).toBe('search-timeout')
 expect(rows[1]?.failure).toBeUndefined()
 const view=render(<WorkSummary state={state}/>)
 expect(view.container.querySelector('[data-status="failed"]')?.textContent).toContain('took too long')
 expect(view.container.querySelector('[data-status="complete"]')?.textContent).not.toContain('took too long')
 expect(view.container.textContent).not.toContain('untrusted raw detail')
 expect(screen.getAllByText(/took too long/)).toHaveLength(1)
})

it('shows activity in the collapsed Work summary and active row, then stops motion on interruption',()=>{
 const state={...INITIAL_STATE,status:'running' as const,events:[{type:'tool_call' as const,callId:'a',name:'search_sources',args:{}}]}
 const view=render(<WorkSummary state={state}/>);const disclosure=view.container.querySelector('summary')!
 expect(disclosure.querySelector('[data-spinner]')).toBeTruthy()
 fireEvent.click(disclosure)
 expect(view.container.querySelector('[data-status="working"] [data-spinner]')).toBeTruthy()
 view.rerender(<WorkSummary state={{...state,status:'failed'}}/>)
 expect(view.container.querySelector('[data-spinner]')).toBeNull()
 expect(screen.getByText('Interrupted')).toBeTruthy()
})

it.each(['propose_input_recipe', 'propose_dialect_recipe'])('keeps recipe feedback for %s across saved history',async(name)=>{
 const {readResponseHistory,summarizeWork}=await import('./responseHistory')
 const work=summarizeWork([{type:'tool_call',callId:'bad',name,args:{}},{type:'tool_result',callId:'bad',name,isError:true,text:'PRIVATE_SENTINEL',structured:{inputRecipeFailure:'criteria'}}],false)
 const record={id:'response',documents:[],websites:[],sourceIds:[],work}
 const restored=readResponseHistory([record])[0]!
 const view=render(<WorkSummary work={restored.work}/>)
 expect(view.container.textContent).toContain('Omit qualificationTarget when unused')
 expect(view.container.textContent).not.toContain('PRIVATE_SENTINEL')
 for(const patch of [{inputRecipeFailure:'raw-secret'},{name:'read_dialect'},{status:'complete'}]) expect(()=>readResponseHistory([{...record,work:{...work,items:[{...work.items[0],...patch}]}}])).toThrow()
})

it('renders one inspectable work row per delegated task as its state changes',async()=>{
 const task={id:crypto.randomUUID(),parentRunId:crypto.randomUUID(),title:'Review assumptions',objective:'Review supplied assumptions.',profile:'review',state:'queued' as const,createdAt:new Date().toISOString(),modelRequests:0,toolCalls:0,attempts:0,steps:[]}
 const events=[{type:'delegated_task' as const,task},{type:'delegated_task' as const,task:{...task,state:'running' as const}}]
 const rows=workItems(events,true)
 expect(rows).toHaveLength(1);expect(rows[0]?.status).toBe('working')
 const {vi}=await import('vitest');const onRead=vi.fn()
 const view=render(<WorkSummary work={{items:rows,notices:[]}} onRead={onRead}/>)
 fireEvent.click(screen.getByText('Work · 1 step'))
 const working=screen.getByRole('button',{name:'Review assumptions · Delegated task · Working…'})
 expect(working.querySelector('[class*="workIndicator"] svg')).toBeNull()
 fireEvent.click(working);expect(onRead).toHaveBeenCalled()
 const completed=workItems([...events,{type:'delegated_task',task:{...task,state:'complete',result:'Retained review.'}}],false)
 view.rerender(<WorkSummary work={{items:completed,notices:[]}} onRead={onRead}/>)
 expect(screen.getByRole('button',{name:'Review assumptions · Delegated task · Done'})).toBeTruthy()
 expect(screen.getAllByText('Review assumptions')).toHaveLength(1)
})

it('retains a readable question refusal and marks its successful correction without a failed badge',async()=>{
 const {readResponseHistory,summarizeWork}=await import('./responseHistory')
 const {interviewQuestion}=await import('../testing/question')
 const failure={type:'tool_result' as const,callId:'bad',name:'ask_user',isError:true,text:'PRIVATE_SENTINEL',structured:{questionFailure:'title'}}
 const record={id:'response',documents:[],websites:[],sourceIds:[],work:summarizeWork([failure],false)}
 const restored=readResponseHistory([record])[0]!
 const view=render(<WorkSummary work={restored.work}/>)
 expect(view.container.textContent).toContain('Ask a question')
 expect(view.container.textContent).toContain('40 characters')
 expect(view.container.textContent).not.toContain('PRIVATE_SENTINEL')
 const corrected={...record,work:summarizeWork([failure,{type:'user_question',id:'question',question:interviewQuestion}],false)}
 view.rerender(<WorkSummary work={readResponseHistory([corrected])[0]!.work}/>)
 expect(view.container.textContent).toContain('Corrected');expect(view.container.textContent).not.toContain('failed')
 expect(()=>readResponseHistory([{...record,work:{...record.work,items:[{...record.work.items[0],questionFailure:'raw-secret'}]}}])).toThrow()
})

it.each(['gemini-3.8-flash','claude-sonnet','gpt-6-sol'])('keeps running status visible for %s with a restored saved artifact and no streamed prose',model=>{
 const state={...INITIAL_STATE,status:'running' as const,restored:true,detail:'Working…',streaming:'',currentAITarget:{model}}
 const view=render(<ComposerTaskStatus state={state} empty={false} savedCandidate/>)
 expect(screen.getByRole('status').textContent).toBe('Working…')
 expect(screen.getByRole('status').querySelector('[data-spinner]')).toBeTruthy()
 expect(screen.getByRole('status').getAttribute('data-running')).toBe('true')
 view.rerender(<ComposerTaskStatus state={{...state,status:'complete'}} empty={false} savedCandidate/>)
 expect(screen.queryByRole('status')).toBeNull()
 view.rerender(<ComposerTaskStatus state={state} empty savedCandidate={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Working…')
})

it('renders readable labels for restored context work after an interrupted response',()=>{
 const items=Array.from({length:20},(_,i)=>({id:String(i),name:'read_context',status:'complete' as const}))
 const view=render(<WorkSummary work={{items,notices:[]}}/>)
 fireEvent.click(screen.getByText('Work · 20 steps'))
 expect(screen.getAllByText('Read retained context')).toHaveLength(20)
 expect([...view.container.querySelectorAll('li')].every(row=>row.textContent?.includes('Done'))).toBe(true)
})

it('keeps the critic\'s account of the declared cases with the response, and shows it',()=>{
 const critique={type:'critique' as const,refuted:true,checks:[{tool:'validate',status:'valid'}],text:'validate valid; incomplete, 1 declared case(s) not compared',incomplete:['c2'],cases:[{case:'c1',source:'test' as const,tool:'experimental_test_cases' as const,agrees:false,expected:{disposition:{kind:'approve'}},actual:{disposition:{kind:'decline'}}}],figures:{model:'cheap-critic',unit:'model-requests' as const,requests:2,inputTokens:500,outputTokens:60}}
 const work=summarizeWork([critique],false,{runModel:'main-model'})
 expect(work.critic).toEqual({compared:1,disagreed:1,incomplete:['c2'],figures:critique.figures,runModel:'main-model'})
 // It survives a save and a reload, and a record that is not what the Desk writes is refused.
 const row={id:'response-1',documents:[],websites:[],sourceIds:[],work:JSON.parse(JSON.stringify(work))}
 expect(readResponseHistory([row])[0]!.work.critic).toEqual(work.critic)
 expect(()=>readResponseHistory([{...row,work:{...row.work,critic:{...work.critic,disagreed:9}}}])).toThrow('invalid')
 render(<WorkSummary work={work}/>)
 fireEvent.click(screen.getByText('Assistant notice'))
 expect(screen.getByText(/Some declared cases were not compared, so this review cannot say the proposal was not refuted\. Not compared: c2/)).toBeTruthy()
 expect(screen.getByText('Declared cases: 1 compared, 1 differ from the expected outcome.')).toBeTruthy()
 expect(screen.getByText(/The review ran on cheap-critic, the model this connection names for reviews, and the answer on main-model\./)).toBeTruthy()
})

it('shows each child\'s figures beside the parent\'s, from the checkpoint, and again after a reload',()=>{
 const [first,second]=DELEGATION.batches[0]!.children
 const task=(id:string,title:string)=>({type:'delegated_task' as const,task:{id,parentRunId:crypto.randomUUID(),title,objective:title,profile:'review',state:'complete' as const,createdAt:new Date().toISOString(),modelRequests:1,toolCalls:0,attempts:1,steps:[],result:'Findings.'}})
 const events=[task(first!.task,'Review the pack'),task(second!.task,'Research the threshold')]
 const work=summarizeWork(events,false,{execution:checkpoint({delegation:DELEGATION})})
 expect(work.report).toMatchObject({requests:11,delegation:DELEGATION})
 const reloaded=readResponseHistory([{id:'response-1',documents:[],websites:[],sourceIds:[],work:JSON.parse(JSON.stringify(work))}])[0]!.work
 expect(reloaded.report).toEqual(work.report)
 render(<WorkSummary work={reloaded}/>)
 fireEvent.click(screen.getByText('Work · 2 steps'))
 const table=screen.getByRole('table')
 expect([...table.querySelectorAll('tbody tr')].map(row=>row.querySelector('th')!.textContent)).toEqual(['Assistant (its own)','Review the pack','Research the threshold (partial)','Batch 1 total'])
 expect(()=>readResponseHistory([{id:'response-1',documents:[],websites:[],sourceIds:[],work:{...JSON.parse(JSON.stringify(work)),report:{...work.report,requests:-3}}}])).toThrow('invalid')
})

it('leaves a reply that did no work and delegated nothing without a report',()=>{
 const work=summarizeWork([],false,{execution:checkpoint({used:1,completions:[{request:1,phase:'work',result:{finishReason:'stop',inputTokens:10,outputTokens:3}}]})})
 expect(work.report).toMatchObject({requests:1,tokens:{inputTokens:10,outputTokens:3}})
 expect(render(<WorkSummary work={work}/>).container.textContent).toBe('')
})
