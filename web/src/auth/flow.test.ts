import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { finishSignIn } from './flow'

const id='a'.repeat(48),proof='b'.repeat(48),session='c'.repeat(48)
const key='jpack-desk.sign-in-attempt.v1'
beforeEach(()=>{sessionStorage.clear();history.replaceState(null,'','/auth/return#attempt='+id)})
afterEach(()=>{sessionStorage.clear();history.replaceState(null,'','/');vi.unstubAllGlobals()})
function pending(kind='login',at=Date.now()) {sessionStorage.setItem(key,JSON.stringify({id,proof,kind,at}))}

it('requires a tab-held proof, not just the callback URL',async()=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher)
 expect(await finishSignIn(null)).toEqual({handled:true,id:null})
 expect(fetcher).not.toHaveBeenCalled()
 expect(location.hash).toBe('')
})
it('finishes login once and sends no existing session to the login completion',async()=>{
 pending();const fetcher=vi.fn().mockResolvedValue(Response.json({kind:'login',id:session,returnPath:'/packs?filter=draft'}));vi.stubGlobal('fetch',fetcher)
 expect(await finishSignIn('old')).toEqual({handled:true,id:session})
 expect(fetcher).toHaveBeenCalledWith('/api/auth/complete',expect.objectContaining({body:JSON.stringify({attemptId:id,proof}),credentials:'omit',redirect:'error'}))
 expect(fetcher.mock.calls[0]![1].headers.Authorization).toBeUndefined()
 expect(location.pathname).toBe('/packs');expect(sessionStorage.getItem(key)).toBeNull()
 history.replaceState(null,'','/auth/return#attempt='+id)
 expect((await finishSignIn(null)).id).toBeNull();expect(fetcher).toHaveBeenCalledTimes(1)
})
it('returns a verified provider test to its original administrator session',async()=>{
 pending('test');const fetcher=vi.fn().mockResolvedValue(Response.json({kind:'test',returnPath:'/admin#identity-provider'}));vi.stubGlobal('fetch',fetcher)
 expect(await finishSignIn(session)).toEqual({handled:true,id:session})
 expect(fetcher.mock.calls[0]![1].headers.Authorization).toBe('Bearer '+session)
 expect(location.pathname+location.hash).toBe('/admin#identity-provider')
})
it('preserves the administrator on a cancelled test but creates no login session on failure',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(Response.json({error:'cancelled'},{status:403})))
 pending('test');expect((await finishSignIn(session)).id).toBe(session)
 history.replaceState(null,'','/auth/return#attempt='+id);pending('login')
 expect((await finishSignIn(session)).id).toBeNull()
})
it('refuses expired, malformed, and mismatched pending attempts without requests',async()=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher)
 for(const attempt of [{id,proof,kind:'login',at:Date.now()-360000},{id,proof,kind:'unknown',at:Date.now()},{id,proof,kind:'login'}, {id:'wrong',proof,kind:'login',at:Date.now()}]){
  history.replaceState(null,'','/auth/return#attempt='+id);sessionStorage.setItem(key,JSON.stringify(attempt))
  expect((await finishSignIn(null)).id).toBeNull()
 }
 expect(fetcher).not.toHaveBeenCalled()
})
it('rejects an external return location and the wrong completion kind',async()=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher)
 for(const reply of [{kind:'login',id:session,returnPath:'//untrusted.example'}, {kind:'test',returnPath:'/'}]){
  history.replaceState(null,'','/auth/return#attempt='+id);pending()
  fetcher.mockResolvedValue(Response.json(reply));expect((await finishSignIn(null)).id).toBeNull()
  expect(location.pathname).toBe('/')
 }
})
