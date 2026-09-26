import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { QuickSwitcher } from './QuickSwitcher'
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'/fixture'}})}))
vi.mock('../mcp/queries',()=>({usePacks:()=>({data:{packs:[{id:'travel',description:'Expense policy'}]}}),useGraphInventory:()=>({data:{graphs:[{id:'onboarding'}]}})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({ready:true,chats:[{id:'chat-one',title:'Policy conversation',archived:false}],packDrafts:[{id:'draft-one',title:'Policy draft'}]})}))
vi.mock('../packs/folders/client',()=>({FOLDERS_KEY:['pack-folders'],loadFolders:async()=>({document:{version:1,folders:[{id:'finance',name:'Finance',parentId:null}],assignments:{}}})}))
afterEach(()=>{cleanup();localStorage.clear()})
function Harness(){const location=useLocation();return <><QuickSwitcher/><input aria-label="Editor"/><div data-testid="location">{location.pathname+location.search}</div></>}
function mount(){return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><MemoryRouter><Harness/></MemoryRouter></QueryClientProvider>)}
it('searches all destinations, navigates by keyboard, and records only the visited destination',async()=>{
 mount();fireEvent.click(screen.getByRole('button',{name:'Search and switch'}))
 const input=screen.getByRole('combobox')
 await screen.findByRole('option',{name:/Finance/})
 fireEvent.change(input,{target:{value:'finance'}})
 expect(screen.getAllByRole('option')).toHaveLength(1)
 fireEvent.keyDown(input,{key:'Enter'})
 await waitFor(()=>expect(screen.getByTestId('location').textContent).toBe('/packs?folder=finance'))
 expect(localStorage.getItem('jpack.quick-switch.v1:/fixture')).toContain('finance')
 fireEvent.keyDown(document.body,{key:'k',ctrlKey:true})
 fireEvent.change(screen.getByRole('combobox'),{target:{value:'policy draft'}})
 fireEvent.keyDown(screen.getByRole('combobox'),{key:'ArrowDown'})
 fireEvent.keyDown(screen.getByRole('combobox'),{key:'Enter'})
 await waitFor(()=>expect(screen.getByTestId('location').textContent).toBe('/packs/drafts/draft-one'))
})
it('leaves text-field shortcuts alone and restores focus when closing the dialog',async()=>{
 mount();const field=screen.getByRole('textbox',{name:'Editor'});field.focus();fireEvent.keyDown(field,{key:'k',ctrlKey:true})
 expect(screen.queryByRole('dialog')).toBeNull()
 const button=screen.getByRole('button',{name:'Search and switch'});button.focus();fireEvent.click(button)
 fireEvent.click(screen.getByRole('button',{name:'Close'}))
 await waitFor(()=>expect(document.activeElement).toBe(button))
})
