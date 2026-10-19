import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DialectPublicationReview} from './DialectPublicationReview'
import {publicationAction,publishedAnswer} from '../testing/publication'
import {dialectAPI} from './client'
import {actionApprovalAPI} from '../assistant/approvals/client'
import {INITIAL_STATE} from '../research/run'
import type {ChatStore} from '../chat/store'
const mocked=vi.hoisted(()=>({store:null as unknown as ChatStore}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:mocked.store})}))
vi.mock('./client',async original=>({...await original<object>(),dialectAPI:vi.fn()}))
vi.mock('../assistant/approvals/client',async original=>({...await original<object>(),actionApprovalAPI:vi.fn()}))
let cache:QueryClient,chat:any,action:ReturnType<typeof publicationAction>,result:ReturnType<typeof publishedAnswer>|null,onClose:ReturnType<typeof vi.fn<()=>void>>
beforeEach(()=>{
 vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});action=publicationAction();result=null;onClose=vi.fn();chat={id:'chat',interaction:'work',inputActions:[action],checkpoint:{state:{...INITIAL_STATE,status:'complete'}}}
 mocked.store={getSnapshot:()=>({chats:[chat],bindings:new Map()}),checkpoint:vi.fn(async()=>{}),update:vi.fn((_id,patch)=>Object.assign(chat,patch))} as unknown as ChatStore
 vi.mocked(dialectAPI).mockImplementation(async()=>({result}) as any)
 vi.mocked(actionApprovalAPI).mockImplementation(async()=>{result=publishedAnswer(action);return result as any})
})
afterEach(()=>{cleanup();cache.clear()})
function show(disabled=false){return render(<QueryClientProvider client={cache}><Tooltip.Provider><DialectPublicationReview chatId="chat" action={action} opener={{current:null}} onClose={onClose} disabled={disabled}/></Tooltip.Provider></QueryClientProvider>)}
it('Ask me publishes only after the explicit compact shared action, then verifies receipt',async()=>{
 show();const publish=await screen.findByRole('button',{name:'Publish version'}) as HTMLButtonElement
 await waitFor(()=>expect(publish.disabled).toBe(false));expect(actionApprovalAPI).not.toHaveBeenCalled();fireEvent.click(publish)
 await waitFor(()=>expect(onClose).toHaveBeenCalledTimes(1));expect(actionApprovalAPI).toHaveBeenCalledWith('chat','apply',expect.objectContaining({target:action.id,reviewed:true}))
 expect(chat.inputActions[0].applied).toBeTruthy()
})
it('Plan blocks publishing while preserving read-only receipt recovery',async()=>{
 chat.interaction='plan';show(true)
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalled());expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText('Finish the response and review open questions before publishing.')).toBeTruthy();expect(actionApprovalAPI).not.toHaveBeenCalled()
})
it('restores a completed publication without reading a missing recipe or replaying the write',async()=>{
 chat.interaction='plan';result=publishedAnswer(action);show(true)
 fireEvent.click(await screen.findByRole('button',{name:'Confirm saved result'}))
 await waitFor(()=>expect(onClose).toHaveBeenCalledTimes(1));expect(actionApprovalAPI).not.toHaveBeenCalled()
 expect(vi.mocked(dialectAPI).mock.calls.every(([path,method])=>path?.includes('?request=')&&method==='GET')).toBe(true)
})
it('retains the failed proposal and offers a read-only check after interrupted publication',async()=>{
 vi.mocked(actionApprovalAPI).mockRejectedValue(Error('Connection lost'));show();const button=screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement
 await waitFor(()=>expect(button.disabled).toBe(false));fireEvent.click(button)
 await screen.findByText('Connection lost');expect(chat.inputActions[0].applied).toBeUndefined()
 fireEvent.click(screen.getByRole('button',{name:'Check saved result'}));await waitFor(()=>expect(dialectAPI).toHaveBeenCalled());expect(actionApprovalAPI).toHaveBeenCalledTimes(1)
})
