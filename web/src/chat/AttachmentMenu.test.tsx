import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AttachmentMenu } from './AttachmentMenu'
import { genericConnection } from '../testing/genericConnection'
import { CONNECTION_PREFERENCES_KEY } from '../connections/preferences'
beforeEach(() => localStorage.removeItem(CONNECTION_PREFERENCES_KEY))
afterEach(cleanup)
it('caps shortcuts at five, prioritizes pins and recents, and preserves access to the full directory', async () => {
 const select=vi.fn(), more=vi.fn()
 const connections=Array.from({length:8},(_,i)=>({ provider:`files-${i}`, selection:'source-search' as const, descriptor:{...genericConnection,id:`files-${i}`,presentation:{...genericConnection.presentation!,name:`Files ${i}`}},onSelect:select }))
 localStorage.setItem(CONNECTION_PREFERENCES_KEY,JSON.stringify({pinned:['removed','files-6'],recent:['files-7','files-2']}))
 render(<AttachmentMenu disabled={false} onUpload={vi.fn()} connections={connections} onMore={more}/>)
 fireEvent.keyDown(screen.getByRole('button',{name:'Attach files'}),{key:'Enter'})
 const items=await screen.findAllByRole('menuitem',{name:/Files \d/})
 expect(items.map(item=>item.textContent?.match(/Files \d/)?.[0])).toEqual(['Files 6','Files 7','Files 2','Files 0','Files 1'])
 fireEvent.click(items[4]!); expect(select).toHaveBeenCalledOnce()
 expect(JSON.parse(localStorage.getItem(CONNECTION_PREFERENCES_KEY)!).recent[0]).toBe('files-1')
 fireEvent.keyDown(screen.getByRole('button',{name:'Attach files'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'More connections'})); expect(more).toHaveBeenCalledOnce()
})
it('keeps shortcut positions stable while open and disables a removed connection', async () => {
 const select = vi.fn()
 const obsidian = { provider: 'obsidian' as const, selection: 'source-search' as const, onSelect: select }
 const gmail = { provider: 'gmail' as const, selection: 'mail-search' as const, onSelect: vi.fn() }
 const ui = render(<AttachmentMenu disabled={false} onUpload={vi.fn()} connections={[obsidian]} onMore={vi.fn()} />)
 fireEvent.keyDown(screen.getByRole('button', { name: 'Attach files' }), { key: 'Enter' })
 expect(await screen.findByRole('menuitem', { name: /Obsidian/ })).toBeTruthy()
 ui.rerender(<AttachmentMenu disabled={false} onUpload={vi.fn()} connections={[gmail]} onMore={vi.fn()} />)
 const old = screen.getByRole('menuitem', { name: /Obsidian/ })
 expect(old.getAttribute('aria-disabled')).toBe('true')
 expect(screen.queryByRole('menuitem', { name: /Gmail/ })).toBeNull()
 fireEvent.click(old); expect(select).not.toHaveBeenCalled()
 fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
 fireEvent.keyDown(screen.getByRole('button', { name: 'Attach files' }), { key: 'Enter' })
 expect(await screen.findByRole('menuitem', { name: /Gmail/ })).toBeTruthy()
 expect(screen.queryByRole('menuitem', { name: /Obsidian/ })).toBeNull()
})

it('offers Add link only when the caller advertises the shipped URL reader', async () => {
 const link=vi.fn();const ui=render(<AttachmentMenu disabled={false} onUpload={vi.fn()} onLink={link} />)
 fireEvent.keyDown(screen.getByRole('button',{name:'Attach files'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:/Add link/}));expect(link).toHaveBeenCalledOnce()
 ui.rerender(<AttachmentMenu disabled={false} onUpload={vi.fn()} />)
 fireEvent.keyDown(screen.getByRole('button',{name:'Attach files'}),{key:'Enter'})
 await screen.findByRole('menuitem',{name:'Upload files'});expect(screen.queryByRole('menuitem',{name:/Add link/})).toBeNull()
})

it('keeps an open menu stable when URL retrieval becomes unavailable', async () => {
 const ui=render(<AttachmentMenu disabled={false} onUpload={vi.fn()} onLink={vi.fn()} />)
 fireEvent.keyDown(screen.getByRole('button',{name:'Attach files'}),{key:'Enter'})
 await screen.findByRole('menuitem',{name:/Add link/})
 ui.rerender(<AttachmentMenu disabled={false} onUpload={vi.fn()} />)
 expect(screen.getByRole('menuitem',{name:/Add link/}).getAttribute('aria-disabled')).toBe('true')
})
