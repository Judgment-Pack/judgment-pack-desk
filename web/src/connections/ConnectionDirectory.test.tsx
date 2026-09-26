import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ConnectionDirectory, type DirectoryEntry } from './ConnectionDirectory'
import { genericConnection } from '../testing/genericConnection'

const key = 'jpack-desk.connection-directory.v1:picker'
function entry(id: string, name: string, state: 'connected' | 'not-connected' | 'setup-required' | 'unavailable' = 'connected'): DirectoryEntry {
 return { descriptor: { ...genericConnection, id, presentation: { ...genericConnection.presentation!, name, description: { en: 'Réservé team documents' } } }, status: { data: { state } } }
}
const entries = [entry('drive-two','Another drive'), entry('aws-s3','Amazon S3','setup-required'), entry('fixture-files','Fixture files','not-connected')]
const select = vi.fn(), retry = vi.fn()
function view(props: Partial<Parameters<typeof ConnectionDirectory>[0]> = {}) {
 return <ConnectionDirectory entries={entries} available loading={false} isError={false} retry={retry} mode="picker" onSelect={select} {...props} />
}
beforeEach(() => { sessionStorage.clear(); vi.clearAllMocks() })
afterEach(() => { cleanup(); vi.restoreAllMocks() })

it('searches unknown providers by name, description and exact or spaced provider ID', () => {
 render(view())
 const search = screen.getByRole('textbox', { name: 'Search connections…' })
 for (const query of ['s3','aws-s3','aws s3','amazon reserve']) {
  fireEvent.change(search, { target: { value: query } })
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  expect(screen.getByRole('button',{ name:'Set up: Amazon S3' })).toBeTruthy()
 }
 expect(select).not.toHaveBeenCalled()
})

it('filters connected providers, clears an empty search, and keeps browse state when returning', () => {
 const ui=render(view())
 fireEvent.click(screen.getByRole('radio',{name:'Connected · 1'}))
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'another'}})
 const opener=screen.getByRole('button',{name:'Open: Another drive'})
 fireEvent.click(opener)
 expect(select).toHaveBeenCalledWith(expect.objectContaining(entries[0]!),opener)
 ui.unmount(); render(view())
 expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('another')
 expect(screen.getByRole('radio',{name:'Connected · 1'}).getAttribute('aria-checked')).toBe('true')
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'Open: Another drive'}))
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'missing'}})
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Clear filters'}))
 expect(screen.getAllByRole('listitem')).toHaveLength(3)
 expect(document.activeElement).toBe(screen.getByRole('textbox'))
})

it('filters setup-required and unavailable including incompatible providers without inventing actions', () => {
 sessionStorage.setItem(key,JSON.stringify({view:'all',filter:'setup-required',query:'',scroll:0}))
 const unavailable=entry('broken','Broken'); unavailable.status={isError:true}
 const unsupported={...genericConnection,id:'future-files',presentation:{...genericConnection.presentation!,name:'Future files'}}
 const ui=render(view({entries:[...entries,unavailable],unsupported:[unsupported]}))
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 expect(screen.getByRole('button',{name:'Set up: Amazon S3'})).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('combobox',{name:'Connection status'}),{key:'ArrowDown'})
 fireEvent.click(screen.getByRole('option',{name:'Unavailable'}))
 expect(screen.getAllByRole('listitem')).toHaveLength(2)
 expect(screen.getByRole('button',{name:'Manage: Broken'})).toBeTruthy()
 expect(within(screen.getByRole('heading',{name:'Future files'}).closest('li')!).queryByRole('button')).toBeNull()
 ui.rerender(view({isError:true}))
 expect(screen.queryAllByRole('listitem')).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Retry'})); expect(retry).toHaveBeenCalledOnce()
})

it('restores scrolling after asynchronous rows resolve and retains scroll across renders', () => {
 sessionStorage.setItem(key,JSON.stringify({view:'all',filter:'all',query:'',scroll:240,selected:'aws-s3'}))
 const ui=render(<div style={{overflowY:'auto'}}>{view({entries:[],loading:true})}</div>)
 const scroller=ui.container.firstElementChild as HTMLElement
 expect(scroller.scrollTop).toBe(0)
 ui.rerender(<div style={{overflowY:'auto'}}>{view()}</div>)
 expect(scroller.scrollTop).toBe(240)
 scroller.scrollTop=300; fireEvent.scroll(scroller)
 ui.rerender(<div style={{overflowY:'auto'}}>{view({entries:[...entries]})}</div>)
 fireEvent.click(screen.getByRole('button',{name:'Set up: Amazon S3'}))
 expect(JSON.parse(sessionStorage.getItem(key)!).scroll).toBe(300)
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'fixture'}})
 expect(scroller.scrollTop).toBe(0)
})

it('ignores malformed view memory and works when optional browser storage is unavailable', () => {
 sessionStorage.setItem(key,'null')
 vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw new Error('blocked')})
 render(view())
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'fixture'}})
 expect(screen.getAllByRole('listitem')).toHaveLength(1)
 fireEvent.click(screen.getByRole('button',{name:'Connect: Fixture files'}))
 expect(select).toHaveBeenCalledOnce()
})

it('waits for statuses before declaring a remembered filtered view empty', () => {
 sessionStorage.setItem(key,JSON.stringify({view:'connected',filter:'all',query:'',scroll:0}))
 const ui=render(view({entries:entries.map(item=>({...item,status:{}}))}))
 expect(screen.getByRole('status').textContent).toBe('Loading…')
 expect(screen.queryByText('No connections found.')).toBeNull()
 ui.rerender(view())
 expect(screen.getByRole('button',{name:'Open: Another drive'})).toBeTruthy()
 expect(screen.queryByRole('status')).toBeNull()
})

it('does not steal focus or scroll when a late status settles after user navigation', () => {
 sessionStorage.setItem(key,JSON.stringify({view:'all',filter:'all',query:'',scroll:240,selected:'aws-s3'}))
 const pending=entries.map((item,i)=>i===1?{...item,status:{}}:item)
 const ui=render(<div style={{overflowY:'auto'}}>{view({entries:pending})}</div>)
 const scroller=ui.container.firstElementChild as HTMLElement
 const filter=screen.getByRole('combobox')
 filter.focus()
 fireEvent.wheel(scroller)
 scroller.scrollTop=70; fireEvent.scroll(scroller)
 ui.rerender(<div style={{overflowY:'auto'}}>{view()}</div>)
 expect(document.activeElement).toBe(filter)
 expect(scroller.scrollTop).toBe(70)
 expect(JSON.parse(sessionStorage.getItem(key)!).scroll).toBe(70)
})
