/** DescribeIt consumes the canonical host projection. Creation and validation
 * remain separately covered by CreatePackDialog's actual writer tests. */
import {act,cleanup,renderHook,waitFor} from '@testing-library/react'
import {useCallback,useState} from 'react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import type {AssistantEvent} from '@judgment-pack/harness/engine'
import {useDescribeIt,STILL_RUNNING} from './DescribeIt'
const fixture=vi.hoisted(()=>({start:vi.fn(),stop:vi.fn(),publish:undefined as undefined|((events:AssistantEvent[],failure?:string)=>void),ready:true,promptReady:true,endSession:()=>{}}))
vi.mock('../assistant/useAssistantSlot',()=>({useAssistantSlot:()=>({endpoint:{kind:'openai-compatible',models:['chosen'],model:'chosen',tools:[]},state:'configured',keyPresent:fixture.ready,keyStatus:'success',engine:'vercel',thinking:'off',connectionId:'selected',connectionRevision:'a'.repeat(64)}),assistantReady:()=>fixture.ready,CHECKING_KEY:'Checking key',UNREAD_KEY:'Unread key'}))
vi.mock('../mcp/prompts',()=>({AUTHOR_PACK_PROMPT:'author_pack',TEST_PACK_PROMPT:'test_pack',usePromptNames:()=>({data:['author_pack','test_pack'],isSuccess:true}),usePromptText:()=>({data:fixture.promptReady?{text:'Guidance preview'}:undefined,error:null})}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/project'}})}))
vi.mock('../mcp/session',async original=>({...await original<typeof import('../mcp/session')>(),whenSessionEnds:(fn:()=>void)=>{fixture.endSession=fn;return()=>{}}}))
vi.mock('../assistant/useAuthoringRun',()=>({useAuthoringRun:()=>{
 const[events,setEvents]=useState<AssistantEvent[]>([]);const[status,setStatus]=useState<'idle'|'running'|'finished'>('idle');const[failure,setFailure]=useState<string>()
 fixture.publish=(events,failure)=>{setEvents(events);setFailure(failure);setStatus('finished')}
 const start=useCallback((input:unknown)=>{fixture.start(input);setStatus('running');setEvents([]);setFailure(undefined)},[])
 const stop=useCallback(()=>{fixture.stop();setStatus('finished')},[])
 return{events,status,failure,start,stop,streaming:'',report:undefined,engineId:'vercel'}
}}))
const proposal:AssistantEvent={type:'proposal',document:{title:'New proposal',value:1.2},unknowns:['Policy decision needed']}
beforeEach(()=>{vi.clearAllMocks();fixture.ready=true;fixture.promptReady=true;sessionStorage.clear()})
afterEach(cleanup)
it('captures only the description as closed request data and offers no document before completion',async()=>{
 const {result}=renderHook(()=>useDescribeIt());act(()=>result.current.setTyped('Exact policy\n<&> 한글'));act(()=>result.current.propose());await waitFor(()=>expect(fixture.start).toHaveBeenCalledOnce())
 expect(fixture.start.mock.calls[0][0]).toEqual({purpose:'describe-pack',prompt:'author_pack',arguments:{policy:'Exact policy\n<&> 한글'}})
 expect(result.current.offered).toBe(false);expect(result.current.blocking).toBe(STILL_RUNNING)
 act(()=>fixture.publish!([proposal,{type:'end'}]));expect(result.current.offered).toBe(true);expect(result.current.proposal).toEqual(proposal);expect(result.current.blocking).toBe('')
})
it('a fresh submission immediately withdraws the previous offer while waiting for its own projection',async()=>{
 const {result}=renderHook(()=>useDescribeIt());act(()=>result.current.setTyped('policy'));act(()=>result.current.propose());await waitFor(()=>expect(fixture.start).toHaveBeenCalledOnce());act(()=>fixture.publish!([proposal,{type:'end'}]));expect(result.current.offered).toBe(true)
 act(()=>result.current.propose());expect(result.current.offered).toBe(false);await waitFor(()=>expect(fixture.start).toHaveBeenCalledTimes(2));expect(result.current.blocking).toBe(STILL_RUNNING)
})
it('Stop before the prompt preview settles cannot start a delayed model request',()=>{
 fixture.promptReady=false;const {result,rerender}=renderHook(()=>useDescribeIt());act(()=>result.current.setTyped('policy'));act(()=>result.current.propose());act(()=>result.current.stop());fixture.promptReady=true;rerender();expect(fixture.start).not.toHaveBeenCalled();expect(result.current.offered).toBe(false)
})
it('a host failure after proposal prevents Create from receiving that document',async()=>{
 const {result}=renderHook(()=>useDescribeIt());act(()=>result.current.setTyped('policy'));act(()=>result.current.propose());await waitFor(()=>expect(fixture.start).toHaveBeenCalledOnce());act(()=>fixture.publish!([proposal,{type:'end'}],'Interrupted host run'));expect(result.current.offered).toBe(false);expect(result.current.blocking).toContain('Interrupted host run')
})
it('discard, session termination and loss of the connection withdraw an otherwise finished offer',async()=>{
 const {result,rerender}=renderHook(()=>useDescribeIt());act(()=>result.current.setTyped('policy'));act(()=>result.current.propose());await waitFor(()=>expect(fixture.start).toHaveBeenCalledOnce());act(()=>fixture.publish!([proposal,{type:'end'}]));act(()=>fixture.endSession());expect(result.current.offered).toBe(false)
 act(()=>result.current.setTyped('new'));act(()=>result.current.propose());await waitFor(()=>expect(fixture.start).toHaveBeenCalledTimes(2));act(()=>fixture.publish!([proposal,{type:'end'}]));fixture.ready=false;rerender();expect(result.current.offered).toBe(false);expect(result.current.blocking).toBeTruthy()
 act(()=>result.current.discard());expect(result.current.typed).toBe('');expect(result.current.offered).toBe(false)
})
it('route dismissal drops local review state without sending Stop to the retained host run',async()=>{
 const {result}=renderHook(()=>useDescribeIt());act(()=>result.current.setTyped('policy'));act(()=>result.current.propose());await waitFor(()=>expect(fixture.start).toHaveBeenCalledOnce());act(()=>result.current.discard(false));expect(fixture.stop).not.toHaveBeenCalled();expect(result.current.offered).toBe(false);expect(result.current.typed).toBe('')
})
