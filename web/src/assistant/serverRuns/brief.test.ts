import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting} from '../../mcp/session'
import {serverRunClient} from './client'
import {briefSubject,decodeSnapshot} from './decode'
import pending from './fixtures/pending.json'

const token='1'.repeat(32),revision='a'.repeat(64)
const binding={owner:{kind:'brief' as const,id:'pack:one',revision},requestId:token}
const snapshot={...pending,...binding,admissionId:`brief-${token}`}
const start={admissionId:`brief-${token}`,purpose:'brief' as const,ownerId:'pack:one',revision,requestId:token}
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
beforeEach(()=>giveThisPageASessionForTesting('brief-session'))
afterEach(()=>vi.unstubAllGlobals())

it('reads a real brief owner and keeps ordinary chat discovery scoped to chat',async()=>{
 expect(decodeSnapshot(snapshot,binding).owner.kind).toBe('brief')
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 await expect(client.discover({owner:{kind:'brief',id:'pack:one'},requestId:token} as never)).rejects.toMatchObject({delivery:'not-sent'})
 expect(fetch).not.toHaveBeenCalled()
})

it.each(['pack:one','pack-draft:one','graph:flow','graph-draft:chat_one','case:one:case-one','case:source:owner:case-one',`job:job_${'a'.repeat(32)}`,`run:run_${'b'.repeat(32)}`])('accepts the closed brief subject %s',subject=>expect(briefSubject(subject)).toBe(true))
it.each(['chat:one','case:one','case::','job:../private','/private/briefs.json','graph:','pack:one\nother',`run:run_${'b'.repeat(33)}`])('refuses an undeclared brief subject %s',subject=>expect(briefSubject(subject)).toBe(false))

it('keeps the existing Brief UTF8 subject bound, with escaped Unicode and other owners unchanged',()=>{
 for(const length of [200,201,1024,1025])expect(briefSubject('pack:'+'x'.repeat(length-5))).toBe(length<=1024)
 const subject='pack:'+'한'.repeat(339)+'aa'
 expect(new TextEncoder().encode(subject)).toHaveLength(1024)
 expect(briefSubject(JSON.parse(JSON.stringify(subject)))).toBe(true);expect(briefSubject(subject+'a')).toBe(false)
 expect(()=>decodeSnapshot({...snapshot,owner:{...snapshot.owner,id:subject}}, {...binding,owner:{...binding.owner,id:subject}})).not.toThrow()
 expect(()=>decodeSnapshot({...snapshot,owner:{kind:'chat',id:subject,revision}}, {...binding,owner:{kind:'chat',id:subject,revision}})).toThrow()
})

it('issues a model-free reference, starts exactly that reservation with explicit connection, and sends no executable material',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(json({start})).mockResolvedValueOnce(json(snapshot));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 const ref=await client.briefReference('pack:one',token)
 expect(fetch).toHaveBeenCalledOnce();expect(fetch.mock.calls[0][0]).toBe('/api/assistant-briefs/reference')
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({subject:'pack:one',token})
 await client.start(ref.start,undefined,{connectionId:'ai-'+'1'.repeat(24),connectionRevision:'selected-revision'})
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual(start)
 expect(fetch.mock.calls[1][1].headers).toMatchObject({'X-Assistant-Connection':'ai-'+'1'.repeat(24),'X-Assistant-Revision':'selected-revision'})
})

it('refuses arbitrary second admission, browser checkpoint and missing target before sending',async()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);const client=await serverRunClient(),target={connectionId:'ai-'+'1'.repeat(24),connectionRevision:'selected-revision'}
 await expect(client.start({...start,admissionId:'second-generation'},undefined,target)).rejects.toMatchObject({delivery:'not-sent'})
 await expect(client.start({...start,checkpoint:{}} as never,undefined,target)).rejects.toMatchObject({delivery:'not-sent'})
 await expect(client.start(start)).rejects.toMatchObject({delivery:'not-sent'})
 expect(fetch).not.toHaveBeenCalled()
})

it('discovers retained output without starting and commits only the exact run/token reference',async()=>{
 const complete={...snapshot,state:'complete'},fetch=vi.fn().mockResolvedValueOnce(json({start,run:complete})).mockResolvedValueOnce(json({content:{version:1,subjects:{}}}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient(),ref=await client.briefReference('pack:one',token)
 expect(ref.run?.state).toBe('complete');expect(fetch).toHaveBeenCalledOnce()
 await client.commitBrief(ref.run!)
 expect(fetch.mock.calls[1][0]).toBe(`/api/assistant-runs/${snapshot.id}/brief`)
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({token})
})

it.each([{owner:{...snapshot.owner,kind:'chat'}},{owner:{...snapshot.owner,id:'pack:other'}},{requestId:'another-token'},{owner:{...snapshot.owner,revision:'b'.repeat(64)}}])('refuses a foreign brief reference (%j)',patch=>expect(()=>decodeSnapshot({...snapshot,...patch},binding)).toThrow())

it('refuses a successful start acknowledgement from another admission',async()=>{
 const fetch=vi.fn().mockResolvedValue(json({...snapshot,admissionId:'another-admission'}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 await expect(client.start(start,undefined,{connectionId:'ai-'+'1'.repeat(24),connectionRevision:'selected-revision'})).rejects.toMatchObject({delivery:'uncertain',failure:{code:'invalid-response'}})
 expect(fetch).toHaveBeenCalledOnce()
})

it('refuses a retained reference from another admission without starting again',async()=>{
 const fetch=vi.fn().mockResolvedValue(json({start,run:{...snapshot,admissionId:'another-admission'}}));vi.stubGlobal('fetch',fetch)
 const client=await serverRunClient()
 await expect(client.briefReference('pack:one',token)).rejects.toMatchObject({failure:{code:'invalid-response'}})
 expect(fetch).toHaveBeenCalledOnce()
})
