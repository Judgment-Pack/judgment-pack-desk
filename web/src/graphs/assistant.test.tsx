import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import {GraphAssistant} from './GraphAssistant'
import type {Chat} from '../chat/store'
const fixture=vi.hoisted(()=>({chat:{id:'chat',graphDrafts:[]} as unknown as Chat, onChat:vi.fn(), apply:vi.fn(), update:vi.fn(), openDraftId:undefined as string|undefined, context:undefined as undefined|{kind:string;text:string;beforeSend:()=>void}}))
vi.mock('../mcp/queries', () => ({useGraphMatrix: () => ({data: undefined})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:{activate:vi.fn(),update:fixture.update},ready:true,chats:[fixture.chat],drafts:[],bindings:new Map([['chat',{state:{status:'ready',restored:false}}]])})}))
vi.mock('../chat/ChatPanel',()=>({ChatPanel:({context,proposalActions,openGraphDraftId}:{context:typeof fixture.context;proposalActions:import('react').ReactNode;openGraphDraftId?:string})=>{fixture.context=context;fixture.openDraftId=openGraphDraftId;return <><input aria-label="Message"/><button onClick={()=>context?.beforeSend()}>Send fixture</button>{proposalActions}</>}}))
const initial={id:'flow',path:'flow.graph.json',baseSha256:'a'.repeat(64),content:'{"nodes":{"a":{"pack":"a"}}}'}
afterEach(()=>{cleanup();fixture.chat={id:'chat',graphDrafts:[]} as unknown as Chat;vi.clearAllMocks()})
const element=(content=initial.content,selection='a')=><GraphAssistant proposal={{...initial,content}} workspace="flow" openDraftId="current-draft" chatId="chat" onChat={fixture.onChat} onApply={fixture.apply} busy={false} selection={selection}/>
it('binds proposals to the send-time graph bytes and preserves unsent text when selection changes',()=>{
 const view=render(element());fireEvent.change(screen.getByLabelText('Message'),{target:{value:'Unsent follow-up'}});fireEvent.click(screen.getByText('Send fixture'))
 fixture.chat={...fixture.chat,graphDrafts:[{...initial,content:'{"nodes":{"a":{"pack":"b"}}}',draftId:'revision-1',createdAt:new Date().toISOString()}]};view.rerender(element(undefined,'b'))
 expect(fixture.openDraftId).toBe('current-draft');expect(fixture.context?.kind).toBe('graph');expect(screen.getByLabelText('Message')).toHaveProperty('value','Unsent follow-up')
 fireEvent.click(screen.getByText('Review proposed changes'));expect(screen.getByRole('button',{name:'Apply to draft'})).toHaveProperty('disabled',false)
 view.rerender(element('{"nodes":{}}'));expect(screen.getByRole('button',{name:'Apply to draft'})).toHaveProperty('disabled',true)
 expect(fixture.apply).not.toHaveBeenCalled()
})
it('does not accept a saved graph proposal aimed at a different path',()=>{
 const view=render(element());fireEvent.click(screen.getByText('Send fixture'))
 fixture.chat={...fixture.chat,graphDrafts:[{...initial,path:'different.graph.json',draftId:'revision-1',createdAt:new Date().toISOString()}]};view.rerender(element())
 fireEvent.click(screen.getByText('Review proposed changes'));expect(screen.getByRole('button',{name:'Apply to draft'})).toHaveProperty('disabled',true)
})

it('binds an unscoped draft conversation to its graph before another turn',()=>{
 render(element())
 expect(fixture.update).toHaveBeenCalledWith('chat',{graph:{id:'flow',path:'flow.graph.json',workspace:'flow'}})
})

it('keeps read-mode graph proposals as draft artifacts without offering direct application',()=>{
 const view=render(<GraphAssistant proposal={initial} workspace="flow" chatId="chat" onChat={fixture.onChat} busy={false} selection=""/>)
 fireEvent.click(screen.getByText('Send fixture'))
 fixture.chat={...fixture.chat,graphDrafts:[{...initial,content:'{"nodes":{"a":{"pack":"b"}}}',draftId:'revision-1',createdAt:new Date().toISOString()}]}
 view.rerender(<GraphAssistant proposal={initial} workspace="flow" chatId="chat" onChat={fixture.onChat} busy={false} selection=""/>)
 expect(fixture.context?.kind).toBe('graph')
 expect(screen.queryByText('Review proposed changes')).toBeNull()
 expect(screen.queryByRole('button',{name:'Apply to draft'})).toBeNull()
 expect(fixture.apply).not.toHaveBeenCalled()
})
