import {cleanup,renderHook,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import type {ReactNode} from 'react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {useDialectPublicationAutoSave} from './useDialectPublicationAutoSave'
import {useActionApproval} from '../assistant/approvals/client'
import {savePublication} from '../dialects/savePublication'
import {usePublishDialectEditing} from '../dialects/recipeEditing'
import {publicationAction} from '../testing/publication'
import {publicationPath} from '../dialects/publicationProposal'
import {INITIAL_STATE} from '../research/run'
vi.mock('../assistant/approvals/client',async original=>({...await original<object>(),useActionApproval:vi.fn()}))
vi.mock('../dialects/savePublication',()=>({savePublication:vi.fn(async()=>{})}))
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(savePublication).mockResolvedValue({} as any)})
afterEach(()=>{cleanup();cache.clear()})
const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={cache}>{children}</QueryClientProvider>
function setup(){const a=publicationAction(),b={...publicationAction(),id:'publication-two',inputId:'prep_'+'c'.repeat(32)};const grant={id:'grant-one',action:'dialect-publish',target:a.id,path:publicationPath(a),revision:a.revision,state:'ready',expiresAt:new Date(Date.now()+60000).toISOString()};vi.mocked(useActionApproval).mockReturnValue({data:{mode:'auto',grant},isError:false} as any);return {a,b,grant,props:{chat:{id:'chat',interaction:'work',inputActions:[a,b]},store:{problem:vi.fn()},binding:{state:{...INITIAL_STATE,status:'complete'}},restored:true} as any}}
it('uses the exact grant target across multiple Dialect panes, not the newest proposal in the chat',async()=>{
 const {a,props}=setup();renderHook(()=>useDialectPublicationAutoSave(props),{wrapper})
 await waitFor(()=>expect(savePublication).toHaveBeenCalledTimes(1))
 expect(savePublication).toHaveBeenCalledWith(expect.objectContaining({action:a,grantId:'grant-one'}))
})
it.each(['plan','running','dirty','wrong target','research allowance','not restored'])('never dispatches for %s',async kind=>{
 const {a,grant,props}=setup()
 if(kind==='plan')props.chat.interaction='plan'
 if(kind==='running')props.binding.state.status='running'
 if(kind==='wrong target')grant.target='missing'
 if(kind==='research allowance')grant.action='research'
 if(kind==='not restored')props.restored=false
 renderHook(()=>{usePublishDialectEditing(a.inputId,kind==='dirty');useDialectPublicationAutoSave(props)},{wrapper})
 await new Promise(resolve=>setTimeout(resolve,0));expect(savePublication).not.toHaveBeenCalled()
})
it('does not repeat an uncertain automatic publication after query refresh',async()=>{
 const {props}=setup();vi.mocked(savePublication).mockRejectedValue(Error('Disconnected'))
 const h=renderHook(p=>useDialectPublicationAutoSave(p),{initialProps:props,wrapper})
 await waitFor(()=>expect(props.store.problem).toHaveBeenCalledTimes(1))
 h.rerender({...props});await new Promise(resolve=>setTimeout(resolve,0));expect(savePublication).toHaveBeenCalledTimes(1)
})

it('resumes a ready grant exactly once after a dirty editor clears',async()=>{
 const {a,props}=setup()
 const h=renderHook(({dirty})=>{usePublishDialectEditing(a.inputId,dirty);useDialectPublicationAutoSave(props)},{initialProps:{dirty:true},wrapper})
 await new Promise(resolve=>setTimeout(resolve,0));expect(savePublication).not.toHaveBeenCalled()
 h.rerender({dirty:false});await waitFor(()=>expect(savePublication).toHaveBeenCalledTimes(1))
 expect(props.store.problem).not.toHaveBeenCalled()
})
