import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { CREATION_NOTICES, DeskSwitcher } from './DeskSwitcher'
import { languageReady, setLanguage, systemMessage } from '../i18n'
import { deskFetch } from '../files/client'
import { openDesk } from './scope'
import { UnsavedChangesProvider, useRegisteredChanges } from '../shell/UnsavedChanges'
const {flush}=vi.hoisted(()=>({flush:vi.fn()}))
vi.mock('../files/client',()=>({deskFetch:vi.fn()}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/projects/startup'}})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{organization:{name:'Unveil'}}})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:{flush,running:false},dirty:true})}))
vi.mock('./scope',()=>({activeDeskId:'',openDesk:vi.fn()}))
const next={id:'a'.repeat(32),name:'Review',folder:'/desks/review',managed:true}
const directory={current:{id:'',name:'Operations',folder:'/projects/startup',managed:false},desks:[{id:'',name:'Operations',folder:'/projects/startup',managed:false},next],location:'/desks'}
beforeEach(()=>{flush.mockResolvedValue(true);vi.mocked(deskFetch).mockImplementation(async(_url,init)=>new Response(JSON.stringify(init?.method==='POST'?next:directory),{status:200}));document.title='Original'})
afterEach(async()=>{cleanup();vi.clearAllMocks();setLanguage('en');await languageReady();localStorage.clear()})
function Dirty(){useRegisteredChanges(true,'Discard this job?',{name:'Unsaved job'});return null}
function show(dirty=false){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemoryRouter><UnsavedChangesProvider>{dirty&&<Dirty/>}<DeskSwitcher/></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)}
async function menu(){await screen.findByText('Operations');fireEvent.keyDown(screen.getByRole('button',{name:'Operations · Switch desk'}),{key:'Enter'})}
it('shows the named desk, updates the title, and flushes chats before switching',async()=>{
 show();await menu();await waitFor(()=>expect(document.title).toBe('Operations'))
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Review'}));await waitFor(()=>expect(openDesk).toHaveBeenCalledWith(next.id));expect(flush).toHaveBeenCalled()
})
it('creates a named desk through the server before opening it',async()=>{
 show();await menu();fireEvent.click(await screen.findByRole('menuitem',{name:'Create desk…'}))
 fireEvent.change(await screen.findByLabelText('Desk name'),{target:{value:'Review'}})
 fireEvent.click(screen.getByRole('button',{name:'Create desk'}))
 await waitFor(()=>expect(openDesk).toHaveBeenCalledWith(next.id))
 expect(deskFetch).toHaveBeenCalledWith('/api/desks',expect.objectContaining({method:'POST',body:'{"name":"Review"}'}))
})
it('preserves the asterisk and requires the item name before abandoning edits',async()=>{
 show(true);await menu();expect(document.title).toBe('* Operations')
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Review'}));await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(openDesk).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Type Unsaved job to confirm.'),{target:{value:'Yes'}})
 expect((screen.getByRole('button',{name:'Discard changes'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}));expect(openDesk).not.toHaveBeenCalled()
})
it('stays in the current desk if saving the conversation fails',async()=>{
 flush.mockResolvedValue(false);show();await menu();fireEvent.click(await screen.findByRole('menuitemradio',{name:'Review'}))
 await screen.findByText('Save the conversation before switching desks.');expect(openDesk).not.toHaveBeenCalled()
})

it('uses the logo for home and the desk name for its menu',async()=>{
 show();const home=await screen.findByRole('link',{name:'Desk home'})
 expect(home.querySelector('img')).not.toBeNull()
 expect((await screen.findByRole('button',{name:'Operations · Switch desk'})).textContent).toBe('Operations')
 expect(home.getAttribute('href')).toBe('/')
 expect(home.querySelector('button')).toBeNull()
 expect(screen.queryByRole('link',{name:'Project files'})).toBeNull()
 await menu()
 expect(screen.getByRole('menuitemradio',{name:'Operations'}).getAttribute('aria-checked')).toBe('true')
 expect(screen.getAllByRole('menuitemradio')[0].textContent).toBe('Operations')
 expect(screen.queryByText('/projects/startup')).toBeNull()
 expect(screen.getByRole('menuitem',{name:'Project files'}).getAttribute('href')).toBe('/author')
})

it('the logo home link asks before leaving unsaved work', async () => {
 const router=createMemoryRouter([{path:'*',element:<UnsavedChangesProvider><Dirty/><DeskSwitcher/></UnsavedChangesProvider>}],{initialEntries:['/jobs/new']})
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><RouterProvider router={router}/></QueryClientProvider>)
 fireEvent.click(await screen.findByRole('link',{name:'Desk home'}))
 await screen.findByRole('dialog',{name:'Unsaved changes'})
 expect(router.state.location.pathname).toBe('/jobs/new')
 fireEvent.click(screen.getByRole('button',{name:'Keep editing'}))
 expect(router.state.location.pathname).toBe('/jobs/new')
})

// What the chassis says of a desk it made with less than a new desk has,
// spelled as it writes each paragraph (`internal/desk/desks.go`), with this
// test's values.
const OLDER_FOR_FACTS = 'The runtime this Desk runs (jpack 0.24.0) reads configuration versions 1, 2, 3, 4, not 5. This desk was created at configVersion 4, without requireComparableFacts, so a fact of a type no comparison can match is not refused. A runtime of 0.25.0 or later creates desks with it.'
const OLDER_FOR_KEYS = 'This desk is not signed: a desk names its signing key at configVersion 6, and the runtime this Desk runs (jpack 0.24.0) does not read it. A runtime of 0.26.0 or later creates desks signed.'
const NO_CUSTODY = 'This desk is not signed, because Desk could not keep a signing key for it: Desk\'s signing folder is a symbolic link; a key is not kept anywhere reached through one. It was created at configVersion 5, which names no signing key.'
async function createWith(notice:string){
 vi.mocked(deskFetch).mockImplementation(async(_url,init)=>new Response(JSON.stringify(init?.method==='POST'?{...next,configVersion:'4',signed:false,notice}:directory),{status:201}))
 show();await menu();fireEvent.click(await screen.findByRole('menuitem',{name:'Create desk…'}))
 fireEvent.change(await screen.findByLabelText('Desk name'),{target:{value:'Review'}})
 fireEvent.click(screen.getByRole('button',{name:'Create desk'}))
}
it('says what a desk was made without, paragraph by paragraph, and opens it only when asked',async()=>{
 await createWith(OLDER_FOR_FACTS+'\n\n'+OLDER_FOR_KEYS)
 const said=await screen.findByRole('dialog',{name:'Desk created'})
 expect([...said.querySelectorAll('p')].map(paragraph=>paragraph.textContent)).toEqual([OLDER_FOR_FACTS,OLDER_FOR_KEYS])
 expect(openDesk).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Open desk'}))
 await waitFor(()=>expect(openDesk).toHaveBeenCalledWith(next.id))
})
it('stays where it is when the notice is closed',async()=>{
 await createWith(NO_CUSTODY)
 expect((await screen.findByRole('dialog',{name:'Desk created'})).textContent).toContain(NO_CUSTODY)
 fireEvent.click(screen.getByRole('button',{name:'Close'}))
 await waitFor(()=>expect(screen.queryByRole('dialog',{name:'Desk created'})).toBeNull())
 expect(openDesk).not.toHaveBeenCalled()
})
it('shows each paragraph the chassis writes in the owner’s language, keeping a reason as Desk gave it',async()=>{
 expect(CREATION_NOTICES).toHaveLength(3)
 setLanguage('de');await languageReady()
 expect(systemMessage(OLDER_FOR_KEYS)).toBe('Dieser Desk ist nicht signiert: Ein Desk nennt seinen Signaturschlüssel ab configVersion 6, und die Laufzeit, die Desk ausführt (jpack 0.24.0), liest sie nicht. Eine Laufzeit ab 0.26.0 erstellt Desks signiert.')
 expect(systemMessage(NO_CUSTODY)).toBe('Dieser Desk ist nicht signiert, weil Desk keinen Signaturschlüssel für ihn verwahren konnte: Desk\'s signing folder is a symbolic link; a key is not kept anywhere reached through one. Er wurde mit configVersion 5 erstellt, die keinen Signaturschlüssel nennt.')
 expect(systemMessage(OLDER_FOR_FACTS)).not.toBe(OLDER_FOR_FACTS)
 setLanguage('en');await languageReady()
 await createWith(OLDER_FOR_FACTS+'\n\n'+OLDER_FOR_KEYS)
 await screen.findByRole('dialog',{name:'Desk created'})
 await act(async()=>{setLanguage('de');await languageReady()})
 const said=await screen.findByRole('dialog',{name:'Desk erstellt'})
 expect([...said.querySelectorAll('p')].map(paragraph=>paragraph.textContent)).toEqual([systemMessage(OLDER_FOR_FACTS),systemMessage(OLDER_FOR_KEYS)])
})
it('refuses a creation answer whose notice is not words',async()=>{
 vi.mocked(deskFetch).mockImplementation(async(_url,init)=>new Response(JSON.stringify(init?.method==='POST'?{...next,notice:5}:directory),{status:201}))
 show();await menu();fireEvent.click(await screen.findByRole('menuitem',{name:'Create desk…'}))
 fireEvent.change(await screen.findByLabelText('Desk name'),{target:{value:'Review'}})
 fireEvent.click(screen.getByRole('button',{name:'Create desk'}))
 expect(await screen.findByText('Desks could not be loaded or saved. Please try again.')).toBeTruthy()
 expect(openDesk).not.toHaveBeenCalled()
})
