/** Standalone producer-reader UI custody. The host projection is a fixture;
 * real capture/admission/provider paths are exercised by assistant_authoring_test.go. */
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {useCallback,useMemo,useState} from 'react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import type {AssistantEvent} from '@judgment-pack/harness/engine'
import {AssistantPane} from './AssistantPane'
import {EditingContext,type EditingSession} from '../packs/edit/editingContext'
import {useDocumentBuffer,type DocumentBuffer} from '../packs/edit/useDocumentBuffer'
import {buffered,bytesAt} from '../packs/edit/writes'
const fixture=vi.hoisted(()=>({starts:vi.fn(),stop:vi.fn(),publish:undefined as undefined|((events:AssistantEvent[],failure?:string)=>void),ready:true,hold:false,promptReady:true,prompts:['author_pack','fix_pack','test_pack']}))
vi.mock('./useAssistantSlot',()=>({useAssistantSlot:()=>({endpoint:{kind:'openai-compatible',models:['one','two'],model:'one',tools:[]},state:'configured',keyPresent:fixture.ready,keyStatus:'success',engine:'vercel',thinking:'off',connectionId:'selected',connectionRevision:'a'.repeat(64)}),assistantReady:()=>fixture.ready,CHECKING_KEY:'Checking key',UNREAD_KEY:'Unread key'}))
vi.mock('../mcp/prompts',()=>({AUTHOR_PACK_PROMPT:'author_pack',FIX_PACK_PROMPT:'fix_pack',TEST_PACK_PROMPT:'test_pack',usePromptNames:()=>({data:fixture.prompts,isSuccess:true}),usePromptText:()=>({data:fixture.promptReady?{text:'Preview guidance'}:undefined,error:null,isFetching:!fixture.promptReady})}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/project'}})}))
vi.mock('./useAuthoringRun',()=>({useAuthoringRun:()=>{
 const[events,setEvents]=useState<AssistantEvent[]>([]);const[status,setStatus]=useState<'idle'|'running'|'finished'>('idle');const[failure,setFailure]=useState<string>()
 fixture.publish=(events,failure)=>{setEvents(events);setFailure(failure);setStatus('finished')}
 const start=useCallback((input:unknown)=>{fixture.starts(input);setStatus('running');setEvents([]);setFailure(undefined)},[])
 const stop=useCallback(()=>{fixture.stop();setStatus('finished')},[])
 return{events,status,failure,start,stop,streaming:'',report:undefined,engineId:'vercel',terminals:{count:0}}
}}))
const draft='{\n    "title":"Original",\n    "number": 1.20,\n    "flag":false,\n    "decision": {"literal":"50"}\n}\n'
const proposed={title:'Proposed',number:1.2,flag:false,decision:{literal:'50'}}
let buffer:DocumentBuffer;let writes=0;let busy=''
function Harness({editing=true,path='one.pack.json',diagnostics}:{editing?:boolean;path?:string;diagnostics?:{count:number;bytes:string}}){
 buffer=useDocumentBuffer({path,bytes:draft.length,content:draft,sha256:'b'.repeat(64)})
 const read=useMemo(()=>buffered(buffer.text??''),[buffer.text])
 const session:EditingSession={editing,buffer:read,write:()=>{throw Error('unguarded write')},writeIfCurrent:(expected,baseline,edit)=>{if(!editing||busy)return{applied:false};const result=buffer.commitIfCurrent(expected,baseline,text=>edit(buffered(text)).text);if(result.applied)writes++;return result},diagnosticsAt:()=>[],ids:{outcomes:[],evidence:[],sources:[],rules:[],factPaths:[]},pending:new Map(),hold:()=>{}}
 return <EditingContext.Provider value={session}><AssistantPane draft={buffer.text} identity={buffer.identity} editing={editing} busy={()=>busy} diagnostics={diagnostics}/></EditingContext.Provider>
}
async function run(){fireEvent.change(screen.getByLabelText('What should this pack decide?'),{target:{value:'Exact policy\n<&> 한글'}});fireEvent.click(screen.getByRole('button',{name:'Run'}));await waitFor(()=>expect(fixture.starts).toHaveBeenCalledOnce())}
async function propose(){await run();act(()=>fixture.publish!([{type:'proposal',document:proposed,unknowns:['Policy question']},{type:'end'}]));await screen.findByRole('region',{name:'The proposal'})}
const accept=()=>fireEvent.click(screen.getByRole('button',{name:'Accept into draft'}))
beforeEach(()=>{vi.clearAllMocks();fixture.ready=true;fixture.promptReady=true;fixture.prompts=['author_pack','fix_pack','test_pack'];writes=0;busy='';sessionStorage.clear()})
afterEach(cleanup)
it('captures exact current buffer/path and closed prompt args at the same baseline used for Apply',async()=>{render(<Harness/>);await propose();expect(fixture.starts.mock.calls[0][0]).toEqual({purpose:'author-pack',prompt:'author_pack',arguments:{policy:'Exact policy\n<&> 한글'},draft,path:'one.pack.json'});expect(writes).toBe(0)})
it('Apply is one guarded write preserving unchanged bytes; Undo restores clean original and refuses the old revision',async()=>{
 render(<Harness/>);await propose();accept();await waitFor(()=>expect(buffer.dirty).toBe(true));expect(writes).toBe(1);expect(bytesAt(buffered(buffer.text!),'/number')).toBe('1.20');expect(bytesAt(buffered(buffer.text!),'/decision')).toBe('{"literal":"50"}');expect(JSON.parse(buffer.text!)).toEqual(proposed)
 act(()=>buffer.undo());expect(buffer.text).toBe(draft);expect(buffer.dirty).toBe(false);accept();expect(writes).toBe(1)
})
it('a same-tick edit refuses an already rendered Apply without losing the new bytes',async()=>{
 render(<Harness/>);await propose();act(()=>{buffer.commit(draft+' ');accept()});expect(writes).toBe(0);expect(buffer.text).toBe(draft+' ');expect(screen.getByText('The proposal or editor changed. Review it again before saving.')).toBeTruthy()
})
it('same bytes in a new buffer generation cannot accept the prior proposal',async()=>{const view=render(<Harness/>);await propose();view.rerender(<Harness path="other.pack.json"/>);accept();expect(writes).toBe(0)})
it('a Save claimed between render and click vetoes Apply',async()=>{render(<Harness/>);await propose();busy='Saving';accept();expect(writes).toBe(0);expect(buffer.text).toBe(draft)})
it('reading mode offers no Apply and a failed retained run cannot write',async()=>{const view=render(<Harness editing={false}/>);await propose();expect(screen.queryByRole('button',{name:'Accept into draft'})).toBeNull();view.rerender(<Harness/>);act(()=>fixture.publish!([{type:'proposal',document:proposed,unknowns:[]},{type:'end'}],'Interrupted host execution'));accept();expect(writes).toBe(0)})
it('Fix retains the exact diagnostic string as request data and does not send a browser prompt',async()=>{const diagnostics={count:1,bytes:'[ {"code":"bad","missing":null,"value":1.20} ]'};render(<Harness diagnostics={diagnostics}/>);fireEvent.click(screen.getByRole('button',{name:'Fix'}));await waitFor(()=>expect(fixture.starts).toHaveBeenCalledOnce());expect(fixture.starts.mock.calls[0][0]).toMatchObject({prompt:'fix_pack',arguments:{diagnostics:diagnostics.bytes},draft})})
it('Stop while the preview is loading prevents the later continuation from starting',async()=>{fixture.promptReady=false;const view=render(<Harness/>);fireEvent.change(screen.getByLabelText('What should this pack decide?'),{target:{value:'policy'}});fireEvent.click(screen.getByRole('button',{name:'Run'}));fireEvent.click(screen.getByRole('button',{name:'Stop'}));fixture.promptReady=true;view.rerender(<Harness/>);expect(fixture.starts).not.toHaveBeenCalled()})
it('two explicit submissions of unchanged text create two requests and clear old Apply data',async()=>{render(<Harness/>);await propose();fireEvent.click(screen.getByRole('button',{name:'Run'}));await waitFor(()=>expect(fixture.starts).toHaveBeenCalledTimes(2));expect(screen.queryByRole('button',{name:'Accept into draft'})).toBeNull()})
it('unavailable connection and missing advertised prompt disable submission',()=>{fixture.ready=false;const view=render(<Harness/>);expect(screen.queryByRole('button',{name:'Run'})).toBeNull();fixture.ready=true;fixture.prompts=[];view.rerender(<Harness/>);expect(screen.getByRole('button',{name:'Run'}).hasAttribute('disabled')).toBe(true)})
