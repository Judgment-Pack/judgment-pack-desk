import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting} from '../../mcp/session'
import {decodeSnapshot} from './decode'
import {serverRunClient} from './client'
import pending from './fixtures/diagnostic.json'
import account from './fixtures/diagnostic-account.json'
import {providerRequest} from '../providers'
const binding={owner:{...pending.owner,kind:'connection-diagnostic' as const},requestId:pending.requestId}
beforeEach(()=>giveThisPageASessionForTesting('diagnostic-session'))
afterEach(()=>vi.unstubAllGlobals())
it('reads the closed diagnostic owner vector and rejects foreign/unknown owner identities',()=>{
 expect(decodeSnapshot(pending,binding).owner).toEqual(binding.owner)
 for(const patch of [{kind:'diagnostic'},{id:'chat-one'},{revision:'b'.repeat(64)}])expect(()=>decodeSnapshot({...pending,owner:{...pending.owner,...patch}},binding)).toThrow()
})
it('refuses arbitrary execution material and a changed connection before dispatch',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 const start={admissionId:pending.admissionId,purpose:'connection-diagnostic' as const,ownerId:pending.owner.id,revision:pending.owner.revision,requestId:pending.requestId}
 const target={connectionId:pending.owner.id,connectionRevision:'b'.repeat(64)}
 await expect(client.start(start)).rejects.toMatchObject({delivery:'not-sent'})
 await expect(client.start({...start,prompt:'override'} as never,undefined,target)).rejects.toMatchObject({delivery:'not-sent'})
 await expect(client.start({...start,admissionId:'replacement'},undefined,target)).rejects.toMatchObject({delivery:'not-sent'})
 await expect(client.start(start,undefined,{...target,connectionId:'ai-'+'2'.repeat(24)})).rejects.toMatchObject({delivery:'not-sent'})
 expect(fetch).not.toHaveBeenCalled()
})

it('reads the shared opaque account custody member and refuses malformed revisions',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify(account),{headers:{'Content-Type':'application/json'}})))
 expect(await providerRequest('status')).toEqual(account)
 for(const accountRevision of [null,{},'', 'PRIVATE-ACCOUNT','a'.repeat(65)]){
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({...account,accountRevision}),{headers:{'Content-Type':'application/json'}})))
  await expect(providerRequest('status')).rejects.toMatchObject({code:'provider-unavailable'})
 }
})
