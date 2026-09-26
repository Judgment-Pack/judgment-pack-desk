import { StrictMode, useEffect } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SessionGate } from './SessionGate'
import { REFUSAL_HEADER, bootstrap, forgetSession, resetSessionForTesting, sessionEnded, sessionStorageKey, signOut } from '../mcp/session'

const token = 'a'.repeat(48)
const noHandoff = () => new Response(null, { status: 401, headers: { [REFUSAL_HEADER]: 'no-handoff' } })
beforeEach(() => { resetSessionForTesting(); sessionStorage.clear() })
afterEach(() => { cleanup(); vi.unstubAllGlobals(); resetSessionForTesting() })
function mount(child = <div>Private workspace</div>) {
 const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<StrictMode><QueryClientProvider client={client}><SessionGate>{child}</SessionGate></QueryClientProvider></StrictMode>)
 return client
}
it('keeps the workspace unmounted and offers provider sign-in without a launch link', async () => {
 const mounted=vi.fn()
 function Private(){useEffect(mounted,[]);return <div>Private workspace</div>}
 const fetcher=vi.fn((url) => Promise.resolve(String(url).endsWith('/auth/status') ? Response.json({enabled:true,label:'Example',unavailable:false}) : noHandoff()))
 vi.stubGlobal('fetch',fetcher)
 mount(<Private />)
 await screen.findByRole('button',{name:'Continue with Example'})
 expect(mounted).not.toHaveBeenCalled()
 expect(fetcher.mock.calls.every(([url])=>['/api/session','/api/auth/status'].includes(String(url)))).toBe(true)
 expect(screen.queryByText(/launch link/)).toBeNull()
})
it('verifies the session with the backend before mounting and removes private cached data on revocation',async()=>{
 let resolveRead!:(response:Response)=>void
 const read=new Promise<Response>(resolve=>{resolveRead=resolve})
 vi.stubGlobal('fetch',vi.fn((_url,init)=>init?.method==='POST'?Promise.resolve(Response.json({id:token})):read))
 const client=mount()
 client.setQueryData(['private'],{text:'sensitive'})
 expect(screen.queryByText('Private workspace')).toBeNull()
 await act(async()=>{resolveRead(Response.json({subject:'local user',issuer:null}))})
 await screen.findByText('Private workspace')
 act(()=>forgetSession())
 await screen.findByRole('heading',{name:'Sign in to Unveil'})
 expect(screen.queryByText('Private workspace')).toBeNull()
 expect(client.getQueryData(['private'])).toBeUndefined()
 expect(sessionStorage.getItem(sessionStorageKey())).toBeNull()
})
it('does not flash the workspace for a stale stored session',async()=>{
 sessionStorage.setItem(sessionStorageKey(),token)
 vi.stubGlobal('fetch',vi.fn((_url,init)=>Promise.resolve(init?.method==='POST'?noHandoff():new Response(null,{status:401}))))
 mount()
 await screen.findByRole('heading',{name:'Sign in to Unveil'})
 expect(screen.queryByText('Private workspace')).toBeNull()
 expect(sessionStorage.getItem(sessionStorageKey())).toBeNull()
})
it('distinguishes a stopped server from missing authorization',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new TypeError('offline')))
 mount()
 await screen.findByRole('heading',{name:'Desk is not responding'})
 expect(screen.getByRole('button',{name:'Try again'})).toBeTruthy()
})
it('does not accept a successful response without a session record',async()=>{
 vi.stubGlobal('fetch',vi.fn((_url,init)=>Promise.resolve(Response.json(init?.method==='POST'?{id:token}:{}))))
 mount()
 await screen.findByRole('heading',{name:'Desk is not responding'})
 expect(screen.queryByText('Private workspace')).toBeNull()
})
it('ends access only after backend revocation succeeds; failures leave the session usable',async()=>{
 const fetcher=vi.fn().mockResolvedValueOnce(Response.json({id:token})).mockResolvedValueOnce(new Response(null,{status:503})).mockResolvedValueOnce(new Response(null,{status:204}))
 vi.stubGlobal('fetch',fetcher)
 await bootstrap()
 await expect(signOut()).rejects.toThrow()
 expect(sessionEnded()).toBeNull()
 expect(sessionStorage.getItem(sessionStorageKey())).toBe(token)
 await signOut()
 expect(sessionEnded()).not.toBeNull()
 expect(sessionStorage.getItem(sessionStorageKey())).toBeNull()
 const request=fetcher.mock.calls[1]![1]
 expect(request).toMatchObject({method:'DELETE',credentials:'omit',redirect:'error',headers:{Authorization:`Bearer ${token}`}})
})
it('never lets a late successful read reopen an ended session',async()=>{
 let finish!:(response:Response)=>void
 const pending=new Promise<Response>(resolve=>{finish=resolve})
 const fetcher=vi.fn((_url,init)=>init?.method==='POST'?Promise.resolve(Response.json({id:token})):pending)
 vi.stubGlobal('fetch',fetcher)
 mount()
 await waitFor(()=>expect(fetcher.mock.calls.length).toBeGreaterThan(1))
 act(()=>forgetSession())
 await act(async()=>finish(Response.json({subject:'local user',issuer:null})))
 expect(screen.queryByText('Private workspace')).toBeNull()
})

it('offers local reopening after sign-out without asking for owner setup', async () => {
 const fetcher = vi.fn((url, init) => Promise.resolve(
  String(url).endsWith('/auth/status') ? Response.json({enabled:false, unavailable:false, localAccess:true}) :
  init?.method === 'POST' ? Response.json({id:token}) :
  Response.json({subject:'local user', issuer:null, localAccess:true})
 ))
 vi.stubGlobal('fetch', fetcher)
 mount()
 await screen.findByText('Private workspace')
 act(() => forgetSession())
 await screen.findByRole('heading', {name:'Open your local Desk'})
 expect(screen.getByRole('button', {name:'Continue'})).toBeTruthy()
 expect(screen.queryByRole('button', {name:'Set up sign-in'})).toBeNull()
 expect(screen.queryByLabelText('Owner setup code')).toBeNull()
 expect(screen.queryByText('Private workspace')).toBeNull()
})
