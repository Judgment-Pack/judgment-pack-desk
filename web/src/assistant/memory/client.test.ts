import {expect,it,vi,afterEach} from 'vitest'
import vectors from '../../../../internal/desk/testdata/assistant-memory-review-v1.json'
import {readMemoryReply,memoryRequest} from './client'
import {memoryRequest as transport} from '@judgment-pack/harness/memory/client'
vi.mock('@judgment-pack/harness/memory/client',async original=>({...await original<typeof import('@judgment-pack/harness/memory/client')>(),memoryRequest:vi.fn()}))
afterEach(()=>vi.resetAllMocks())
const reply=(content:unknown)=>({sha256:'a'.repeat(64),content})
it('reads the same private receipt and refusal vectors as Go and backup validation',async()=>{
 await expect(readMemoryReply(reply(vectors.document))).resolves.toBeTruthy()
 for(const field of vectors.requiredReceiptMembers){const doc=structuredClone(vectors.document);delete (doc.reviews[0] as Record<string,unknown>)[field];await expect(readMemoryReply(reply(doc))).rejects.toBeTruthy()}
 for(const bad of vectors.badReceiptMembers){const doc=structuredClone(vectors.document);(doc.reviews[0] as Record<string,unknown>)[bad.member]=bad.value;await expect(readMemoryReply(reply(doc))).rejects.toBeTruthy()}
 for(const bad of vectors.badResultLinks){const doc=structuredClone(vectors.document);Object.assign(doc.reviews[0],bad);await expect(readMemoryReply(reply(doc))).rejects.toBeTruthy()}
 for(const decision of ['accept','edit']){const doc=structuredClone(vectors.document),r=doc.reviews[0] as Record<string,unknown>;Object.assign(r,{decision,entryId:'private-one',memoryId:decision==='accept'?'proposal-one':'person-memory-one',memoryDigest:'sha256:'+'6'.repeat(64)});await expect(readMemoryReply(reply(doc))).resolves.toBeTruthy()}
 for(const shape of [null,{},[null]]){const doc=structuredClone(vectors.document) as Record<string,unknown>;doc.reviews=shape;await expect(readMemoryReply(reply(doc))).rejects.toBeTruthy()}
 for(const name of ['Reviews','REVIEWS']){const doc=structuredClone(vectors.document) as Record<string,unknown>;doc[name]=doc.reviews;delete doc.reviews;await expect(readMemoryReply(reply(doc))).rejects.toBeTruthy()}
})
it('refuses an acknowledgement for another reviewed proposal without replaying',async()=>{
 vi.mocked(transport).mockResolvedValue(reply(vectors.document))
 await expect(memoryRequest({action:'review',review:{id:'review-one',runId:'arun_'+'a'.repeat(32),runRevision:7,proposalId:'another',proposalDigest:'sha256:'+'6'.repeat(64),decision:'decline'}},'a'.repeat(64))).rejects.toBeTruthy()
 expect(transport).toHaveBeenCalledTimes(1)
})
