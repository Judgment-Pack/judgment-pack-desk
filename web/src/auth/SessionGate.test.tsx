import { StrictMode, useEffect, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SessionGate } from './SessionGate'
import { REFUSAL_HEADER, bootstrap, forgetSession, resetSessionForTesting, sessionEnded, sessionStorageKey, signOut } from '../mcp/session'

function legacyFetch(handler: (...args:any[])=>any, status={enabled:false,unavailable:false,sessionActive:false,localAccess:false}) {
 vi.stubGlobal('fetch',(url:unknown,init?:RequestInit)=>String(url)==='/api/auth/status'?Promise.resolve(Response.json(status)):handler(url,init))
}
const token = 'a'.repeat(48)
const noHandoff = () => new Response(null, { status: 401, headers: { [REFUSAL_HEADER]: 'no-handoff' } })
beforeEach(() => { resetSessionForTesting(); sessionStorage.clear() })
afterEach(() => { cleanup(); vi.unstubAllGlobals(); resetSessionForTesting() })
function mount(child = <div>Private workspace</div>) {
 const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<StrictMode><QueryClientProvider client={client}><SessionGate>{child}</SessionGate></QueryClientProvider></StrictMode>)
 return client
}
function expectBootstrapLoading() {
 const [status] = screen.getAllByRole('status')
 expect(screen.getAllByRole('status')).toHaveLength(1)
 expect(status.textContent).toBe('Opening Desk…')
 expect(status.getAttribute('data-running')).toBe('true')
 expect(screen.getByRole('main').contains(status)).toBe(true)
 expect(screen.queryByText('Unveil')).toBeNull()
 expect(screen.queryByRole('banner')).toBeNull()
 expect(screen.queryByRole('contentinfo')).toBeNull()
 expect(screen.queryByRole('combobox')).toBeNull()
 expect(document.querySelector('form')).toBeNull()
 expect(screen.queryByRole('heading', {name:'Sign in to Unveil'})).toBeNull()
}
it('keeps the workspace unmounted and offers provider sign-in without a launch link', async () => {
 const mounted=vi.fn()
 function Private(){useEffect(mounted,[]);return <div>Private workspace</div>}
 const fetcher=vi.fn((url) => Promise.resolve(String(url).endsWith('/auth/status') ? Response.json({enabled:true,label:'Example',unavailable:false,sessionActive:false}) : noHandoff()))
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
 legacyFetch(vi.fn((_url,init)=>init?.method==='POST'?Promise.resolve(Response.json({id:token})):read))
 const client=mount()
 client.setQueryData(['private'],{text:'sensitive'})
 expectBootstrapLoading()
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
 legacyFetch(vi.fn((_url,init)=>Promise.resolve(init?.method==='POST'?noHandoff():new Response(null,{status:401}))))
 mount()
 expectBootstrapLoading()
 await screen.findByRole('heading',{name:'Sign in to Unveil'})
 expect(screen.queryByText('Private workspace')).toBeNull()
 expect(sessionStorage.getItem(sessionStorageKey())).toBeNull()
})
it('distinguishes a stopped server from missing authorization',async()=>{
 legacyFetch(vi.fn().mockRejectedValue(new TypeError('offline')))
 mount()
 await screen.findByRole('heading',{name:'Desk is not responding'})
 expect(screen.getByRole('button',{name:'Try again'})).toBeTruthy()
})
it('does not accept a successful response without a session record',async()=>{
 legacyFetch(vi.fn((_url,init)=>Promise.resolve(Response.json(init?.method==='POST'?{id:token}:{}))))
 mount()
 await screen.findByRole('heading',{name:'Desk is not responding'})
 expect(screen.queryByText('Private workspace')).toBeNull()
})
it('ends access only after backend revocation succeeds; failures leave the session usable',async()=>{
 const fetcher=vi.fn().mockResolvedValueOnce(Response.json({id:token})).mockResolvedValueOnce(new Response(null,{status:503})).mockResolvedValueOnce(new Response(null,{status:204}))
 legacyFetch(fetcher)
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
 legacyFetch(fetcher)
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
 legacyFetch(fetcher,{enabled:false,unavailable:false,sessionActive:false,localAccess:true})
 mount()
 await screen.findByText('Private workspace')
 act(() => forgetSession())
 await screen.findByRole('heading', {name:'Open your local Desk'})
 expect(screen.getByRole('button', {name:'Continue'})).toBeTruthy()
 expect(screen.queryByRole('button', {name:'Set up sign-in'})).toBeNull()
 expect(screen.queryByLabelText('Owner setup code')).toBeNull()
 expect(screen.queryByText('Private workspace')).toBeNull()
})

it('does not send a failed exchange or private read for a stale password session',async()=>{
 sessionStorage.setItem(sessionStorageKey(),token)
 const calls:string[]=[]
 vi.stubGlobal('fetch',async(url:unknown)=>{
  calls.push(String(url))
  expect(String(url)).toBe('/api/auth/status')
  return Response.json({enabled:true,unavailable:false,sessionActive:false,mode:'password'})
 })
 mount()
 await screen.findByLabelText('Desk password',{exact:true})
 expect(screen.queryByText('Private workspace')).toBeNull()
 expect(sessionStorage.getItem(sessionStorageKey())).toBeNull()
 expect(calls.length).toBeGreaterThan(0)
})

it('keeps the selected desk across refresh and waits for verified access before fetching its workspace', async () => {
 const selected = 'b'.repeat(32)
 sessionStorage.setItem('jpack.active-desk', selected)
 sessionStorage.setItem(sessionStorageKey(), token)
 for (let documentNumber = 0; documentNumber < 2; documentNumber += 1) {
  // A reload re-evaluates the document's frozen desk scope while retaining
  // this tab's selection and session, just as switching desks does.
  vi.resetModules()
  const {SessionGate: ReloadedGate} = await import('./SessionGate')
  const {deskFetch} = await import('../files/client')
  const scope = await import('../desks/scope')
  let finish!: (response: Response) => void
  const session = new Promise<Response>(resolve => {finish = resolve})
  const privateRequests: Headers[] = []
  vi.stubGlobal('fetch', (url: unknown, init?: RequestInit) => {
   if (String(url) === '/api/auth/status') return Promise.resolve(Response.json({enabled:true, unavailable:false, sessionActive:true, mode:'password'}))
   if (String(url) === '/api/session') return session
   expect(String(url)).toBe('/api/files')
   privateRequests.push(new Headers(init?.headers))
   return Promise.resolve(Response.json({root:'/selected-workspace', files:[]}))
  })
  function SelectedWorkspace() {
   const [root, setRoot] = useState('')
   useEffect(() => {
    let active = true
    void deskFetch('/api/files').then(response => response.json()).then(listing => {if (active) setRoot(listing.root)})
    return () => {active = false}
   }, [])
   return <p>{root}</p>
  }
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  const view = render(<StrictMode><QueryClientProvider client={client}><ReloadedGate><SelectedWorkspace /></ReloadedGate></QueryClientProvider></StrictMode>)
  expectBootstrapLoading()
  expect(privateRequests).toHaveLength(0)
  expect(sessionStorage.getItem('jpack.active-desk')).toBe(selected)
  await act(async () => finish(Response.json({subject:'local user', issuer:null})))
  await screen.findByText('/selected-workspace')
  expect(screen.queryByRole('status')).toBeNull()
  expect(privateRequests.length).toBeGreaterThan(0)
  expect(privateRequests.every(headers => headers.get('X-Jpack-Desk') === selected && headers.get('Authorization') === `Bearer ${token}`)).toBe(true)
  expect(scope.activeDeskId).toBe(selected)
  expect(scope.deskSocketURL('ws://localhost/ws')).toBe(`ws://localhost/ws?desk=${selected}`)
  view.unmount()
  client.clear()
 }
})
