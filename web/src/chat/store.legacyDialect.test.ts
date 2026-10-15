import {expect, it, vi} from 'vitest'
import {ChatStore} from './store'
import {INITIAL_STATE} from '../research/run'
import {memoryDraftPersistence} from '../testing/draftPersistence'

it('loads the whole history when a pre-Dialects chat has saved recipe validation feedback', async () => {
 const oldStep = {id:'old-call',name:'propose_input_recipe',status:'failed',inputRecipeFailure:'criteria'}
 const response = {id:'response',documents:[],websites:[],sourceIds:[],work:{items:[oldStep],notices:[]}}
 const chat = {id:'legacy-chat',title:'Existing conversation',pinned:false,archived:false,updatedAt:'2026-10-13T12:00:00Z',composer:'Keep this draft',model:'',mode:'draft',view:'chat',checkpoint:{state:{...INITIAL_STATE,responses:[response]},sources:[]}}
 const content = {version:1,chats:[chat,{...chat,id:'other-chat',checkpoint:undefined}]}
 const before = JSON.stringify(content), write=vi.fn()
 const store = new ChatStore('/fixture',{read:async()=>({project:'/fixture',sha256:'saved',content}),write},memoryDraftPersistence('/fixture'))
 try {
  await store.load()
  expect(store.getSnapshot()).toMatchObject({ready:true,error:'',dirty:false})
  expect(store.getSnapshot().chats).toHaveLength(2)
  expect(store.getSnapshot().chats[0]?.checkpoint?.state.responses?.[0]?.work.items[0]).toEqual(oldStep)
  expect(store.getSnapshot().chats[0]?.composer).toBe('Keep this draft')
  await store.flush()
  expect(write).not.toHaveBeenCalled()
  expect(JSON.stringify(content)).toBe(before)
 } finally {store.dispose()}
})
