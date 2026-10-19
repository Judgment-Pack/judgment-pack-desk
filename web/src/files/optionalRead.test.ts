import {afterEach,expect,it,vi} from 'vitest'
import {FileRequestError,readFile,readOptionalFile} from './client'

vi.mock('../mcp/session',async original=>({...await original<typeof import('../mcp/session')>(),sessionBearer:async()=> 'test-session'}))
afterEach(()=>vi.unstubAllGlobals())

it('returns absence without parsing a body and sends the session and abort signal',async()=>{
 const fetch=vi.fn(async()=>new Response(null,{status:204}))
 vi.stubGlobal('fetch',fetch)
 const signal=new AbortController().signal
 expect(await readOptionalFile('jpack-folders.json',signal)).toBeNull()
 expect(fetch).toHaveBeenCalledOnce()
 const [url,init]=fetch.mock.calls[0] as unknown as [string,RequestInit]
 expect(url).toBe('/api/file?path=jpack-folders.json&optional=true')
 expect(init.signal).toBe(signal)
 expect(new Headers(init.headers).get('Authorization')).toBe('Bearer test-session')
 expect(init.credentials).toBe('omit')
})

it('returns existing content and its digest unchanged',async()=>{
 const file={path:'jpack-folders.json',content:'{}',bytes:2,sha256:'observed'}
 vi.stubGlobal('fetch',async()=>Response.json(file))
 expect(await readOptionalFile(file.path)).toEqual(file)
})

it.each([[403,'forbidden'],[404,'parent-is-a-file'],[413,'too-large'],[415,'not-utf8'],[500,'internal']])('preserves read failure %s rather than treating it as absence',async(status,code)=>{
 vi.stubGlobal('fetch',async()=>Response.json({error:'Cannot read this file',code},{status}))
 await expect(readOptionalFile('jpack-folders.json')).rejects.toMatchObject({name:'FileRequestError',status,code})
})

it('keeps network failures and malformed responses visible',async()=>{
 const offline=new Error('offline')
 vi.stubGlobal('fetch',async()=>{throw offline})
 await expect(readOptionalFile('jpack-folders.json')).rejects.toBe(offline)
 vi.stubGlobal('fetch',async()=>new Response('not json'))
 await expect(readOptionalFile('jpack-folders.json')).rejects.toBeInstanceOf(FileRequestError)
})

it('keeps ordinary file reads strict',async()=>{
 const fetch=vi.fn(async()=>Response.json({error:'Missing',code:'not-found'},{status:404}))
 vi.stubGlobal('fetch',fetch)
 await expect(readFile('missing.json')).rejects.toMatchObject({status:404,code:'not-found'})
 expect((fetch.mock.calls[0] as unknown[])[0]).toBe('/api/file?path=missing.json')
})
