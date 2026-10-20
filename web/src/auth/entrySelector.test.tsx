import { StrictMode } from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { SessionPage } from './SessionPage'
import { entryDesks } from './entryDesks'
import { selectDesk } from '../desks/scope'

const REVIEW = 'b'.repeat(32)
const directory = {startup: REVIEW, desks:[{id:'',name:'Operations',mark:null},{id:REVIEW,name:'Review',mark:null}]}
afterEach(()=>{cleanup();vi.unstubAllGlobals();sessionStorage.clear();localStorage.clear();vi.restoreAllMocks()})
function install(options: {unavailable?: boolean; pendingLogin?: Promise<Response>} = {}) {
 const fetcher = vi.fn((url: unknown, _init?: RequestInit) => {
  if(String(url)==='/api/auth/desks') return Promise.resolve(Response.json(directory))
  if(String(url)==='/api/auth/status') return Promise.resolve(Response.json({enabled:true, unavailable:false,mode:'password'}))
  if(String(url)==='/api/auth/password/login') return options.pendingLogin ?? Promise.resolve(Response.json({error:'password'},{status:401}))
  throw Error(`Unexpected request ${url}`)
 })
 vi.stubGlobal('fetch',fetcher)
 return fetcher
}
async function open() {fireEvent.keyDown(screen.getByRole('button',{name:/Switch desk/}),{key:'Enter'});await screen.findByRole('menu')}
it('defaults fresh entry to configured startup and clears the password on a destination switch',async()=>{
 const fetcher=install()
 render(<StrictMode><SessionPage state="recovery"/></StrictMode>)
 await screen.findByRole('heading',{name:'Sign in to Review'})
 expect(sessionStorage.getItem('jpack.active-desk')).toBe(REVIEW)
 fireEvent.change(await screen.findByLabelText('Desk password'),{target:{value:'entered secret'}})
 await open()
 expect(screen.getByRole('menuitemradio',{name:'ReviewStartup'}).getAttribute('aria-checked')).toBe('true')
 expect(screen.queryByRole('menuitem',{name:'Create desk…'})).toBeNull()
 expect(screen.queryByRole('menuitem',{name:'Project files'})).toBeNull()
 fireEvent.click(screen.getByRole('menuitemradio',{name:'Operations'}))
 await screen.findByRole('heading',{name:'Sign in to Operations'})
 expect((await screen.findByLabelText('Desk password') as HTMLInputElement).value).toBe('')
 expect(sessionStorage.getItem('jpack.active-desk')).toBe('')
 expect(directory.startup).toBe(REVIEW)
 expect(fetcher.mock.calls.every(([url,init])=>String(url).startsWith('/api/auth/')&&!new Headers(init?.headers).has('X-Jpack-Desk'))).toBe(true)
})
it('preserves explicit owner alias across refresh despite a different startup and searches a long list',async()=>{
 selectDesk('')
 const rows = {...directory,desks:[...directory.desks,...Array.from({length:60},(_,i)=>({id:(i+10).toString(16).padStart(32,'0'),name:`Desk ${i} with a long descriptive name`,mark:null}))]}
 const fetcher=install()
 fetcher.mockImplementation((url:unknown)=>Promise.resolve(Response.json(String(url)==='/api/auth/desks'?rows:{enabled:true,unavailable:false,mode:'password'})))
 for (let refresh=0;refresh<2;refresh++) {
  const view=render(<SessionPage state="recovery"/>)
  await screen.findByRole('heading',{name:'Sign in to Operations'})
  expect(sessionStorage.getItem('jpack.active-desk')).toBe('')
  await open()
  fireEvent.change(screen.getByRole('textbox',{name:'Search desks'}),{target:{value:'Desk 59'}})
  expect(screen.getAllByRole('menuitemradio')).toHaveLength(1)
  expect(screen.getByRole('menuitemradio').textContent).toContain('Desk 59')
  view.unmount()
 }
})
it('shows an actionable error for an unknown selected desk and never silently chooses startup',async()=>{
 const missing='f'.repeat(32);selectDesk(missing);install()
 render(<SessionPage state="recovery"/>)
 await screen.findByText('The selected desk is unavailable. Choose another desk.')
 expect(screen.queryByLabelText('Desk password')).toBeNull()
 expect(screen.queryByRole('button',{name:'Sign in'})).toBeNull()
 expect(sessionStorage.getItem('jpack.active-desk')).toBe(missing)
 await open();fireEvent.click(screen.getByRole('menuitemradio',{name:'ReviewStartup'}))
 await screen.findByRole('heading',{name:'Sign in to Review'})
 expect(sessionStorage.getItem('jpack.active-desk')).toBe(REVIEW)
})
it('disables switching while password sign-in is in flight and retains the selected destination',async()=>{
 let finish!:(response:Response)=>void
 const pendingLogin=new Promise<Response>(resolve=>{finish=resolve})
 const fetcher=install({pendingLogin})
 render(<SessionPage state="recovery"/>)
 fireEvent.change(await screen.findByLabelText('Desk password'),{target:{value:'secret passphrase'}})
 fireEvent.click(screen.getByRole('button',{name:'Sign in'}))
 await waitFor(()=>expect((screen.getByRole('button',{name:/Switch desk/}) as HTMLButtonElement).disabled).toBe(true))
 expect(sessionStorage.getItem('jpack.active-desk')).toBe(REVIEW)
 expect(fetcher.mock.calls.find(([url])=>String(url).endsWith('/password/login'))?.[1]?.body).toBe('{"password":"secret passphrase"}')
 await act(async()=>finish(Response.json({error:'password'},{status:401})))
 expect((screen.getByRole('button',{name:/Switch desk/}) as HTMLButtonElement).disabled).toBe(false)
})
it('refuses malformed or unbounded public identity payloads',async()=>{
 for (const value of [{startup:REVIEW,desks:[{id:REVIEW,name:'Review',mark:'https://external/image'}]}, {startup:REVIEW,desks:Array(258).fill(directory.desks[1])}]) {
  vi.stubGlobal('fetch',()=>Promise.resolve(Response.json(value)))
  await expect(entryDesks()).rejects.toThrow('Invalid entry directory')
 }
})
it('keeps checking neutral without fetching public or protected desk data',()=>{
 const fetcher=install()
 render(<SessionPage state="checking"/>)
 expect(screen.getByRole('status').textContent).toBe('Opening Desk…')
 expect(screen.queryByRole('button')).toBeNull();expect(screen.queryByRole('banner')).toBeNull()
 expect(fetcher).not.toHaveBeenCalled()
})
