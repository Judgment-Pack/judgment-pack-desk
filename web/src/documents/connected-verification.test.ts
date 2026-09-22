import { expect, it } from 'vitest'
import { verifyDocument } from './client'
import { signedConnected } from './__fixtures__/signedConnected'
it.each(['notion','obsidian'] as const)('verifies a signed %s snapshot and selection',async provider=>{
 const {object,pin}=await signedConnected(provider)
 const verified=await verifyDocument(object,pin)
 expect(verified.record.provenance.source.provider).toBe(provider)
})
it('refuses a validly signed snapshot of a different selected resource',async()=>{
 const {object,pin}=await signedConnected('obsidian',{resourceId:'Other.md'})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it('refuses a validly signed Notion record attributed to the Obsidian command source',async()=>{
 const {object,pin}=await signedConnected('notion',{source:'obsidian',shape:'command'})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it('refuses a validly signed record whose retained original differs from the held original',async()=>{
 const {object,pin}=await signedConnected('obsidian',{record:r=>{r.original.bytes=btoa('substituted text')}})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it('refuses a different document kind carrying connected-source extension fields',async()=>{
 const {object,pin}=await signedConnected('obsidian',{record:r=>{Object.assign(r.provenance.source,{kind:'google-drive',fileId:'file-A',mediaType:'text/plain'})}})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it('refuses Notion acquired through a command instead of MCP',async()=>{
 const {object,pin}=await signedConnected('notion',{shape:'command'})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it.each(['grant','source','extra-proof','missing-proof','signature','seal'] as const)('refuses connected %s substitution',async kind=>{
 const {object,pin}=await signedConnected('obsidian')
 if(kind==='grant') object.proof!.connected!.grant='bb'.repeat(32)
 if(kind==='source') object.proof!.source='documents'
 if(kind==='extra-proof') object.proof!.gmail={messageId:'abc',grant:'aa'.repeat(32)}
 if(kind==='missing-proof') delete object.proof!.connected
 if(kind==='signature') object.proof!.response=object.proof!.response.replace('Review budgets','Approve budgets')
 if(kind==='seal') object.proof!.registry=''
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
it('refuses a validly signed source name outside the connected provider set',async()=>{
 const {object,pin}=await signedConnected('obsidian',{source:'documents'})
 await expect(verifyDocument(object,pin)).rejects.toThrow()
})
