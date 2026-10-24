/** Desk647: actual hook/consumers and real private-session publisher. Only the
 * server transport is synthetic; no production route or executor is activated. */
import {act,cleanup,fireEvent,render,renderHook,screen,waitFor} from '@testing-library/react'
import {useState} from 'react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {forgetSession} from '../mcp/session'
import {useAuthoringRun} from './useAuthoringRun'
import {AssistantPane} from './AssistantPane'
import {DescribeIt,useDescribeIt} from '../shell/DescribeIt'
import {GraphAuthor} from '../graphs/GraphAuthor'
import {checkpoint} from './reportFixtures'
import {RunRequestError} from './serverRuns/client'
import type {RunSnapshot,RunStart,RunState} from './serverRuns/types'
const f=vi.hoisted(()=>({capture:vi.fn(),start:vi.fn(),cancel:vi.fn(),events:vi.fn(),read:vi.fn(),getPrompt:vi.fn(),post:vi.fn(),state:'complete' as RunState}))
vi.mock('./serverRuns/client',async original=>({...await original<typeof import('./serverRuns/client')>(),serverRunClient:async()=>({captureAuthoring:f.capture,start:f.start,cancelAuthoring:f.cancel,events:f.events,read:f.read})}))
vi.mock('./useAssistantSlot',()=>({useAssistantSlot:()=>({endpoint:{kind:'openai-compatible',models:['chosen-model'],model:'chosen-model',tools:[]},state:'configured',keyPresent:true,keyStatus:'success',engine:'vercel',thinking:'off',connectionId:'selected-ai',connectionRevision:'e'.repeat(64)}),assistantReady:()=>true,CHECKING_KEY:'Checking key',UNREAD_KEY:'Unread key'}))
vi.mock('../mcp/prompts',()=>({AUTHOR_PACK_PROMPT:'author_pack',FIX_PACK_PROMPT:'fix_pack',TEST_PACK_PROMPT:'test_pack',AUTHOR_GRAPH_PROMPT:'author_graph',usePromptNames:()=>({data:['author_pack','author_graph'],isSuccess:true}),usePromptText:()=>({data:{text:'PRIVATE-GUIDANCE'},error:null,isFetching:false})}))
vi.mock('../mcp/McpProvider',()=>({useMcp:()=>({client:{getPrompt:f.getPrompt}})}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/project'}}),useFileContent:()=>({data:{content:'{"configVersion":"5","packs":{"one":{"path":"one.pack.json"}},"graphs":{}}'}})}))
vi.mock('@tanstack/react-query',async original=>({...await original(),useQueryClient:()=>({invalidateQueries:vi.fn()})}))
vi.mock('../files/client',async original=>({...await original(),readFile:async()=>({content:'{"title":"Saved pack"}',sha256:'a'.repeat(64)}),deskFetch:(...args:unknown[])=>f.post(...args)}))
const ref:RunStart={admissionId:'author-admit-'+'b'.repeat(32),purpose:'describe-pack',ownerId:'author_'+'a'.repeat(32),revision:'c'.repeat(64),requestId:'request-one'}
const document={title:'PRIVATE-PROPOSAL',graph:{nodes:{one:{pack:'one'}},edges:[]},id:'PRIVATE-ID',description:'PRIVATE-DESCRIPTION'}
const snapshot=(start=ref,state=f.state):RunSnapshot=>({admissionId:start.admissionId,id:'arun_'+'d'.repeat(32),owner:{kind:start.purpose,id:start.ownerId,revision:start.revision},requestId:start.requestId,revision:1,state,operations:[],checkpoint:checkpoint({used:1,limit:4}),events:[{id:'proposal',event:{type:'proposal',document,unknowns:['PRIVATE-UNKNOWN']}},{id:'end',event:{type:'end'}}],cursor:'1:2'})
const options={target:{connectionId:'selected-ai',connectionRevision:'e'.repeat(64)},model:'chosen-model',engine:'vercel' as const,thinking:'off' as const}
const input={purpose:'describe-pack' as const,prompt:'author_pack',arguments:{policy:'Private policy'}}
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return{promise,resolve}}
beforeEach(()=>{
 vi.clearAllMocks();f.state='complete'
 f.capture.mockImplementation(async request=>({start:{...ref,purpose:request.purpose,requestId:request.requestId}}))
 f.start.mockImplementation(async start=>snapshot(start));f.cancel.mockImplementation(async start=>({start,cancelled:true}))
 f.read.mockResolvedValue(snapshot());f.events.mockImplementation(async function*(){})
 f.getPrompt.mockResolvedValue({messages:[{content:{type:'text',text:'PRIVATE-GUIDANCE'}}]})
 f.post.mockResolvedValue(new Response(JSON.stringify({id:'PRIVATE-ID',path:'private.graph.json',content:JSON.stringify(document.graph),description:'PRIVATE-DESCRIPTION',before:'{}',configContent:'{}',configSha256:'a'.repeat(64),token:'b'.repeat(64),nonce:'nonce',hasLock:false,findings:'{"kind":"non-normative-runtime-convention","status":"valid","label":"PRIVATE-FINDINGS"}',plan:'{"kind":"non-normative-runtime-convention","status":"planned","label":"PRIVATE-PLAN"}'})))
})
afterEach(cleanup)
it('private session end clears events, checkpoint report and progress via the real publisher',async()=>{
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(h.result.current.report?.requests).toBe(1))
 act(()=>forgetSession('Signed out'))
 expect(h.result.current.events).toEqual([]);expect(h.result.current.report).toBeUndefined();expect(h.result.current.streaming).toBe('')
 const count=f.capture.mock.calls.length;await act(async()=>h.result.current.start(input));expect(f.capture).toHaveBeenCalledTimes(count)
})
it.each(['awaiting-input','paused-budget','awaiting-client'] as const)('paused %s retains its owner until exact explicit cancellation acknowledges once',async state=>{
 f.state=state
 const cancellation=deferred<{start:RunStart;cancelled:true}>();f.cancel.mockReturnValue(cancellation.promise)
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(h.result.current.status).toBe('finished'))
 expect(h.result.current.failure).toBeTruthy();expect(h.result.current.terminals.count).toBe(1)
 await act(async()=>h.result.current.start(input));expect(f.capture).toHaveBeenCalledOnce()
 act(()=>{h.result.current.stop();h.result.current.stop()});await waitFor(()=>expect(f.cancel).toHaveBeenCalledOnce())
 const start=f.start.mock.calls[0][0];expect(f.cancel).toHaveBeenCalledWith(start)
 await act(async()=>h.result.current.start(input));expect(f.capture).toHaveBeenCalledOnce()
 await act(async()=>cancellation.resolve({start,cancelled:true}));expect(h.result.current.terminals.count).toBe(1)
 f.state='complete';act(()=>h.result.current.start(input));await waitFor(()=>expect(f.capture).toHaveBeenCalledTimes(2))
 expect(f.cancel).toHaveBeenCalledOnce();expect(f.start).toHaveBeenCalledTimes(2)
})
it.each(['complete','failed','stopped','interrupted'] as const)('true terminal %s Stop never creates cancellation or admission',async state=>{
 f.state=state;const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(h.result.current.status).toBe('finished'))
 act(()=>{h.result.current.stop();h.result.current.stop()});expect(f.cancel).not.toHaveBeenCalled();expect(f.capture).toHaveBeenCalledOnce()
})
it('historical paused state is read-only and cannot issue cancellation',async()=>{
 f.start.mockImplementation(async start=>({...snapshot(start,'awaiting-input'),historical:true}))
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(h.result.current.status).toBe('finished'))
 act(()=>h.result.current.stop());expect(f.cancel).not.toHaveBeenCalled()
})
it('uncertain paused cancellation retains the exact owner without retry or new capture',async()=>{
 f.state='paused-budget';f.cancel.mockRejectedValue(new RunRequestError('uncertain'))
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(h.result.current.status).toBe('finished'))
 act(()=>h.result.current.stop());await waitFor(()=>expect(f.cancel).toHaveBeenCalledOnce())
 act(()=>{h.result.current.stop();h.result.current.start(input)})
 expect(f.cancel).toHaveBeenCalledOnce();expect(f.capture).toHaveBeenCalledOnce()
})
it('late admission after real session end cannot restore private output or report',async()=>{
 const admission=deferred<RunSnapshot>();f.start.mockReturnValue(admission.promise)
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(f.start).toHaveBeenCalledOnce())
 act(()=>forgetSession('Signed out'));await act(async()=>admission.resolve(snapshot()))
 expect(h.result.current.events).toEqual([]);expect(h.result.current.report).toBeUndefined();expect(h.result.current.streaming).toBe('')
})
function Pane(){const[bytes,setBytes]=useState('{ "title":"LOCAL-ARTIFACT", "number":1.20 }');return <><textarea aria-label="Local artifact" value={bytes} onChange={e=>setBytes(e.target.value)}/><AssistantPane draft={bytes}/></>}
it('actual pane withdraws private proposal on session end and preserves the outside artifact bytes',async()=>{
 render(<Pane/>);fireEvent.change(screen.getByLabelText('What should this pack decide?'),{target:{value:'Private policy'}});fireEvent.click(screen.getByRole('button',{name:'Run'}))
 await screen.findByRole('region',{name:'The proposal'});expect(documentText()).toContain('PRIVATE-PROPOSAL')
 act(()=>forgetSession('Signed out'))
 expect(screen.queryByRole('region',{name:'The proposal'})).toBeNull();expect(documentText()).not.toContain('PRIVATE-PROPOSAL')
 expect(screen.getByLabelText('Local artifact')).toHaveProperty('value','{ "title":"LOCAL-ARTIFACT", "number":1.20 }')
})
function Description(){const state=useDescribeIt();return <><DescribeIt state={state} expanded/><button onClick={()=>state.discard(true)}>Dismiss authoring</button></>}
it('actual DescribeIt clears private output and report on session end',async()=>{
 render(<Description/>);fireEvent.change(screen.getByLabelText(/What should this pack decide\? Who decides/),{target:{value:'Private policy'}});fireEvent.click(screen.getByRole('button',{name:'Propose'}))
 await screen.findByRole('region',{name:'The proposal'});act(()=>forgetSession('Signed out'))
 expect(screen.queryByRole('region',{name:'The proposal'})).toBeNull();expect(documentText()).not.toContain('PRIVATE-PROPOSAL')
})
it('actual DescribeIt dismissal cancels a paused owner without showing a running spinner',async()=>{
 f.state='awaiting-input';render(<Description/>);fireEvent.change(screen.getByLabelText(/What should this pack decide\? Who decides/),{target:{value:'Private policy'}});fireEvent.click(screen.getByRole('button',{name:'Propose'}))
 await waitFor(()=>expect(f.start).toHaveBeenCalledOnce());await waitFor(()=>expect(screen.getByRole('button',{name:'Stop'})).not.toHaveProperty('disabled',true))
 fireEvent.click(screen.getByRole('button',{name:'Dismiss authoring'}));await waitFor(()=>expect(f.cancel).toHaveBeenCalledOnce())
})
async function proposeGraph(){render(<GraphAuthor/>);fireEvent.click(screen.getByRole('button',{name:'Author graph'}));fireEvent.click(screen.getByLabelText('one'));fireEvent.change(screen.getByLabelText('Relationship'),{target:{value:'Private relationship'}});fireEvent.click(screen.getByRole('button',{name:'Propose graph'}));await screen.findByLabelText('Graph bytes to write')}
const documentText=()=>window.document.body.textContent??''
it('actual GraphAuthor clears copied graph, offer, metadata and Runtime preview on session end',async()=>{
 await proposeGraph();fireEvent.change(screen.getByLabelText('Graph path'),{target:{value:'private.graph.json'}});fireEvent.click(screen.getByRole('button',{name:'Review graph write'}));await screen.findByRole('button',{name:'Confirm graph write'})
 expect(documentText()).toContain('PRIVATE-GUIDANCE');expect(documentText()).toContain('PRIVATE-FINDINGS')
 act(()=>forgetSession('Signed out'))
 expect(screen.queryByLabelText('Graph bytes to write')).toBeNull();expect(screen.queryByRole('button',{name:'Confirm graph write'})).toBeNull()
 expect(documentText()).not.toMatch(/PRIVATE-(GUIDANCE|FINDINGS|PLAN|DESCRIPTION|UNKNOWN)/)
})
it('late GraphAuthor review cannot repopulate a cleared offer after session end',async()=>{
 await proposeGraph();const response=deferred<Response>();f.post.mockReturnValue(response.promise)
 fireEvent.change(screen.getByLabelText('Graph path'),{target:{value:'private.graph.json'}});fireEvent.click(screen.getByRole('button',{name:'Review graph write'}));await waitFor(()=>expect(f.post).toHaveBeenCalledOnce())
 act(()=>forgetSession('Signed out'));await act(async()=>response.resolve(new Response(JSON.stringify({id:'PRIVATE-ID',content:'PRIVATE-LATE-OFFER',path:'private.graph.json',findings:'{}',plan:'{}',configContent:'{}',before:'{}'}))))
 expect(screen.queryByLabelText('Graph bytes to write')).toBeNull();expect(documentText()).not.toContain('PRIVATE-LATE-OFFER')
})
it('late GraphAuthor guidance cannot start or restore preview after session end',async()=>{
 const prompt=deferred<{messages:{content:{type:'text';text:string}}[]}>();f.getPrompt.mockReturnValue(prompt.promise)
 render(<GraphAuthor/>);fireEvent.click(screen.getByRole('button',{name:'Author graph'}));fireEvent.click(screen.getByLabelText('one'));fireEvent.change(screen.getByLabelText('Relationship'),{target:{value:'Private relationship'}});fireEvent.click(screen.getByRole('button',{name:'Propose graph'}));await waitFor(()=>expect(f.getPrompt).toHaveBeenCalledOnce())
 act(()=>forgetSession('Signed out'));await act(async()=>prompt.resolve({messages:[{content:{type:'text',text:'PRIVATE-LATE-GUIDANCE'}}]}))
 expect(f.capture).not.toHaveBeenCalled();expect(documentText()).not.toContain('PRIVATE-LATE-GUIDANCE')
})
it('active progress is cleared and late old progress stays withdrawn after real session end',async()=>{
 const release=deferred<void>()
 f.start.mockImplementation(async start=>({...snapshot(start,'running'),checkpoint:checkpoint({used:1,status:'running'}),events:[],cursor:'1:0'}))
 f.events.mockImplementation(async function*(){yield{kind:'progress',progress:{text:'PRIVATE-PROGRESS'}};await release.promise;yield{kind:'progress',progress:{text:'PRIVATE-LATE-PROGRESS'}}})
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(h.result.current.streaming).toBe('PRIVATE-PROGRESS'))
 act(()=>forgetSession('Signed out'));await act(async()=>release.resolve())
 expect(h.result.current.streaming).toBe('');expect(h.result.current.events).toEqual([]);expect(h.result.current.report).toBeUndefined()
})
it('Stop before capture returns retains ownership when exact cancellation is refused',async()=>{
 const captured=deferred<{start:RunStart}>();f.capture.mockReturnValue(captured.promise);f.cancel.mockRejectedValue(new RunRequestError('refused',403,'permission'))
 const h=renderHook(()=>useAuthoringRun(options));act(()=>h.result.current.start(input));await waitFor(()=>expect(f.capture).toHaveBeenCalledOnce());act(()=>h.result.current.stop())
 await act(async()=>captured.resolve({start:ref}));await waitFor(()=>expect(f.cancel).toHaveBeenCalledExactlyOnceWith(ref))
 await act(async()=>h.result.current.start(input));expect(f.capture).toHaveBeenCalledOnce();expect(f.start).not.toHaveBeenCalled()
})
