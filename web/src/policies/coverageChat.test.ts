import {expect,it,vi} from 'vitest'
import type {Chat,ChatStore} from '../chat/store'
import {prepareCoverageChat} from './coverageChat'
import {coverageCandidateId,coveragePolicyId,coverageRecordId} from '../testing/policyCoverage'

it('appends an unsent exact coverage request to the same Policy chat, preserving Plan, attachments and turns',()=>{
 const chat={id:'existing-chat',policy:{id:coveragePolicyId},composer:'Existing question',updatedAt:'2026-10-18T12:00:00Z',interaction:'plan',attachments:[{name:'reference.pdf'}],checkpoint:{state:{turns:[{id:'turn-one',text:'Prior message'}]}}} as unknown as Chat
 const before=structuredClone(chat),update=vi.fn((_id:string,patch:Partial<Chat>)=>Object.assign(chat,patch)),startChat=vi.fn(),checkpoint=vi.fn()
 const store={getSnapshot:()=>({ready:true,chats:[chat],drafts:[]}),canCreate:true,update,startChat,checkpoint,activate:vi.fn()} as unknown as ChatStore
 expect(prepareCoverageChat(store,coveragePolicyId,{coverageId:coverageRecordId,candidateId:coverageCandidateId},chat.id).id).toBe(chat.id)
 expect(chat.composer).toContain('Existing question\n\n');expect(chat.composer).toContain('read_policy_coverage');expect(chat.composer).toContain(coverageCandidateId)
 expect(chat.interaction).toBe('plan');expect(chat.attachments).toEqual(before.attachments);expect(chat.checkpoint).toEqual(before.checkpoint)
 expect(startChat).not.toHaveBeenCalled();expect(checkpoint).not.toHaveBeenCalled()
 expect(()=>prepareCoverageChat(store,coveragePolicyId,{coverageId:coverageRecordId,candidateId:'latest'})).toThrow()
})
