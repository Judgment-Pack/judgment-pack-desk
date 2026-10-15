import { expect, it } from 'vitest'
import { testConversationTurns } from './conversation'
import { emptySuite } from './model'
import type { TestProposal } from './proposals'
it('uses the same user message identity at submission, completion and legacy recovery', async () => {
 const record={id:'proposal',at:'2026-10-13T12:00:00Z',request:'Design cases'} as TestProposal
 const suite={...emptySuite(),proposals:[record]}
 const user={role:'user' as const,at:record.at,text:record.request}
 const assistant={role:'assistant' as const,at:'2026-10-13T12:01:00Z',text:'Cases proposed',proposalId:record.id}
 const initial=await testConversationTurns('draft',[{...user,proposalId:record.id}],suite)
 const final=await testConversationTurns('pack',[user,assistant],suite)
 expect(initial[0]?.id).toBe(final[0]?.id)
 expect(final[0]?.testDesign?.proposalId).toBe(record.id)
 const legacy=await testConversationTurns('pack',[user,{...assistant,proposalId:undefined}],suite)
 expect(legacy.map(t=>t.id)).toEqual(final.map(t=>t.id))
})
