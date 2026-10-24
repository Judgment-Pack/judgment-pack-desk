import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting} from '../../mcp/session'
import {serverRunClient} from './client'
import type {AuthoringRequest} from '../useAuthoringRun'
import pending from './fixtures/pending.json'
const start={admissionId:'author-admit-'+'a'.repeat(32),purpose:'author-pack' as const,ownerId:'author_'+'b'.repeat(32),revision:'c'.repeat(64),requestId:'chosen-request'}
const request:AuthoringRequest={requestId:start.requestId,purpose:'author-pack',prompt:'fix_pack',arguments:{diagnostics:'[{ "value":1.20,"literal":"50","flag":false,"zero":0,"null":null }]'},draft:'{\n "exact": "한글 <&>"\n}',path:'one.pack.json',model:'selected-model',thinking:'off',phase:'work'}
const snapshot={...pending,admissionId:start.admissionId,owner:{kind:start.purpose,id:start.ownerId,revision:start.revision},requestId:start.requestId}
const json=(value:unknown)=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
beforeEach(()=>giveThisPageASessionForTesting('author-session'));afterEach(()=>vi.unstubAllGlobals())
it('captures only closed person data with selected connection metadata and reads the host-issued owner/start',async()=>{
 const fetch=vi.fn().mockResolvedValue(json({start}));vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 const ref=await client.captureAuthoring(request,{connectionId:'selected-ai',connectionRevision:'d'.repeat(64)})
 expect(ref.start).toEqual(start);expect(fetch).toHaveBeenCalledOnce();expect(fetch.mock.calls[0][0]).toBe('/api/assistant-authoring/captures');expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(request);expect(fetch.mock.calls[0][1].headers).toMatchObject({Authorization:'Bearer author-session','X-Assistant-Connection':'selected-ai','X-Assistant-Revision':'d'.repeat(64)})
})
it('reference reconciliation is GET-only and cancellation sends the exact host reference',async()=>{
 const fetch=vi.fn().mockImplementation(async()=>json({start,run:snapshot}));vi.stubGlobal('fetch',fetch);const client=await serverRunClient()
 expect((await client.authoringReference(start.requestId,start.purpose)).run?.id).toBe(snapshot.id)
 expect(fetch.mock.calls[0][0]).toBe('/api/assistant-authoring/captures/chosen-request');expect(fetch.mock.calls[0][1].method).toBe('GET')
 await client.cancelAuthoring(start);expect(fetch.mock.calls[1][0]).toBe('/api/assistant-authoring/captures/chosen-request/cancel');expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({ownerId:start.ownerId,revision:start.revision})
})
it.each([{purpose:'chat'},{ownerId:'client-chosen-owner'},{admissionId:'chosen-request'},{requestId:'other'},{revision:'not-a-digest'}])('refuses an inexact server authoring reference (%j)',async patch=>{
 const fetch=vi.fn().mockResolvedValue(json({start:{...start,...patch}}));vi.stubGlobal('fetch',fetch);const client=await serverRunClient();await expect(client.authoringReference(start.requestId,start.purpose)).rejects.toMatchObject({failure:{code:'invalid-response'}});expect(fetch).toHaveBeenCalledOnce()
})
it('refuses a response from another owner even when the provider model happens to match',async()=>{
 const fetch=vi.fn().mockResolvedValue(json({start,run:{...snapshot,owner:{...snapshot.owner,id:'author_'+'e'.repeat(32)}}}));vi.stubGlobal('fetch',fetch);const client=await serverRunClient();await expect(client.authoringReference(start.requestId,start.purpose)).rejects.toMatchObject({failure:{code:'invalid-response'}})
})
