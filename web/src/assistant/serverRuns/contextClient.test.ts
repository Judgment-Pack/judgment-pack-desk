import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting} from '../../mcp/session'
import {serverRunClient} from './client'
import pending from './fixtures/pending.json'
const document='a'.repeat(64),pane='b'.repeat(64)
const json=(value:unknown)=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
beforeEach(()=>giveThisPageASessionForTesting('context-protocol-session'))
afterEach(()=>vi.unstubAllGlobals())
it('uses authenticated closed model-free lifecycle requests and exact returned selectors',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(json({document})).mockResolvedValueOnce(json({document,pane})).mockResolvedValueOnce(json({released:true})).mockResolvedValueOnce(json({released:true}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 expect(await client.contextDocument('doc-attempt')).toEqual({document})
 expect(await client.contextPane(document,'pane-attempt','author-pack')).toEqual({document,pane})
 await client.releaseContext({document,pane});await client.releaseContext({document})
 expect(fetch.mock.calls.map(([path])=>path)).toEqual(['/api/assistant-contexts/documents','/api/assistant-contexts/panes','/api/assistant-contexts/panes/release','/api/assistant-contexts/documents/release'])
 expect(fetch.mock.calls.map(([,init])=>JSON.parse(init.body))).toEqual([{registrationId:'doc-attempt'},{document,registrationId:'pane-attempt',purpose:'author-pack'},{document,pane},{document}])
 for(const [,init] of fetch.mock.calls)expect(new Headers(init.headers).get('Authorization')).toBe('Bearer context-protocol-session')
})
it('carries Context in Start while source/capture references remain separate',async()=>{
 const fetch=vi.fn().mockResolvedValue(json(pending));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 const start={admissionId:pending.admissionId,purpose:'chat' as const,ownerId:pending.owner.id,revision:pending.owner.revision,requestId:pending.requestId,context:{document}}
 await client.start(start)
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(start)
 await expect(client.start({...start,context:{document,pane}})).rejects.toBeDefined()
 expect(fetch).toHaveBeenCalledOnce()
})
it.each([{document,pane,history:[]},{document:'forged'},null])('rejects malformed/extra registration responses without replacement',async value=>{
 const fetch=vi.fn().mockResolvedValue(json(value));vi.stubGlobal('fetch',fetch)
 await expect((await serverRunClient()).contextDocument('original-attempt')).rejects.toBeDefined()
 expect(fetch).toHaveBeenCalledOnce()
})
